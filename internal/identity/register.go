package identity

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/kms"
)

// EventUserRegistered is the CloudEvents type for a completed registration.
const EventUserRegistered = "user.registered"

const maxDisplayNameRunes = 100

// BoundaryResolver resolves a ward to its constituency, county and national units.
type BoundaryResolver interface {
	ResolveWard(wardID int) (boundary.Scope, error)
}

// RegisterRequest is the body of POST /v1/auth/register.
type RegisterRequest struct {
	NationalID     string `json:"national_id"`
	DisplayName    string `json:"display_name"`
	PreferredLang  string `json:"preferred_lang"`
	WardID         int    `json:"ward_id"`
	ConsentVersion string `json:"consent_version"`
	ConsentGranted bool   `json:"consent_granted"`
}

// Group is one of the four memberships a new user joins.
type Group struct {
	Level int    `json:"level"` // 1=ward, 2=constituency, 3=county, 4=national
	ID    int    `json:"id"`
	Name  string `json:"name"`
}

// RegisterResponse is the 201 body of POST /v1/auth/register.
type RegisterResponse struct {
	UserID        string  `json:"user_id"`
	PublicID      string  `json:"public_id"`
	DisplayName   string  `json:"display_name"`
	PreferredLang string  `json:"preferred_lang"`
	Groups        []Group `json:"groups"`
	AccessToken   string  `json:"access_token"`
	RefreshToken  string  `json:"refresh_token"`
	ExpiresIn     int     `json:"expires_in"`
}

// UserRegisteredData is the payload of the user.registered event.
// It carries no national ID or hash.
type UserRegisteredData struct {
	UserID         int64  `json:"user_id"`
	PublicID       string `json:"public_id"`
	WardID         int    `json:"ward_id"`
	ConstituencyID int    `json:"constituency_id"`
	CountyID       int    `json:"county_id"`
	ConsentVersion string `json:"consent_version"`
	KeyVersion     string `json:"key_version"`
}

// RegisterHandler serves POST /v1/auth/register (T-1.1.1.2).
type RegisterHandler struct {
	Store    Store
	Keyring  kms.Keyring
	Boundary BoundaryResolver
	Tokens   *TokenIssuer
	Events   events.Publisher
	Logger   *slog.Logger
	Now      func() time.Time
}

func (h *RegisterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req RegisterRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", "Request body must be a single JSON object with known fields.")
		return
	}

	// Validate everything before touching the keyring or database, so a
	// rejected request leaves no trace of the national ID.
	if err := ValidateNationalID(req.NationalID); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "invalid_id", "National ID must be 8 digits.")
		return
	}
	if fieldErrs := validateRegister(&req); len(fieldErrs) > 0 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", "One or more fields are invalid.", fieldErrs...)
		return
	}
	if !req.ConsentGranted {
		problem.Write(w, r, http.StatusUnprocessableEntity, "consent_required", "Explicit consent is required to register.")
		return
	}

	scope, err := h.Boundary.ResolveWard(req.WardID)
	if errors.Is(err, boundary.ErrUnknownWard) {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_unit", "Ward is not a known administrative unit.",
			problem.FieldError{Field: "ward_id", Code: "unknown_ward"})
		return
	}
	if err != nil {
		h.fail(ctx, w, r, "resolve ward", err)
		return
	}

	pepper, err := h.Keyring.Current(ctx, PepperKeyName)
	if err != nil {
		h.Logger.ErrorContext(ctx, "keyring unavailable", "err", err)
		problem.Write(w, r, http.StatusServiceUnavailable, "kms_unavailable", "Registration is temporarily unavailable.")
		return
	}

	now := h.Now().UTC()
	created, err := h.Store.CreateUser(ctx, NewUser{
		PublicID:       uuid.Must(uuid.NewV7()),
		NationalIDHash: hashNationalID(req.NationalID, pepper.Material),
		KeyVersion:     pepper.Version,
		DisplayName:    req.DisplayName,
		PreferredLang:  req.PreferredLang,
		WardID:         int32(scope.Ward.ID),
		ConstituencyID: int32(scope.Constituency.ID),
		CountyID:       int32(scope.County.ID),
		ConsentVersion: req.ConsentVersion,
		ConsentAt:      now,
		IPHash:         hashIP(clientIP(r), pepper.Material),
	})
	if errors.Is(err, ErrDuplicateNationalID) {
		problem.Write(w, r, http.StatusConflict, "id_already_registered", "This national ID is already registered.")
		return
	}
	if err != nil {
		h.fail(ctx, w, r, "create user", err)
		return
	}

	publicID := created.PublicID.String()
	tokens, err := h.Tokens.Issue(publicID)
	if err != nil {
		h.fail(ctx, w, r, "issue tokens", err)
		return
	}

	// The user is committed; a publish failure must not fail the request.
	// T-1.1.1.7 replaces this with a transactional outbox so the event
	// cannot be lost.
	evt := events.New("identity", EventUserRegistered, now, UserRegisteredData{
		UserID:         created.ID,
		PublicID:       publicID,
		WardID:         scope.Ward.ID,
		ConstituencyID: scope.Constituency.ID,
		CountyID:       scope.County.ID,
		ConsentVersion: req.ConsentVersion,
		KeyVersion:     pepper.Version,
	})
	if err := h.Events.Publish(ctx, evt); err != nil {
		h.Logger.ErrorContext(ctx, "publish user.registered failed", "user_id", created.ID, "err", err)
	}

	httpjson.Write(w, http.StatusCreated, RegisterResponse{
		UserID:        publicID,
		PublicID:      publicID,
		DisplayName:   req.DisplayName,
		PreferredLang: req.PreferredLang,
		Groups: []Group{
			{Level: 1, ID: scope.Ward.ID, Name: scope.Ward.Name},
			{Level: 2, ID: scope.Constituency.ID, Name: scope.Constituency.Name},
			{Level: 3, ID: scope.County.ID, Name: scope.County.Name},
			{Level: 4, ID: scope.National.ID, Name: scope.National.Name},
		},
		AccessToken:  tokens.Access,
		RefreshToken: tokens.Refresh,
		ExpiresIn:    tokens.ExpiresIn,
	})
}

func (h *RegisterHandler) fail(ctx context.Context, w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(ctx, "register failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again.")
}

// validateRegister normalizes req in place and returns field errors.
func validateRegister(req *RegisterRequest) []problem.FieldError {
	var errs []problem.FieldError

	req.DisplayName = strings.TrimSpace(req.DisplayName)
	switch n := utf8.RuneCountInString(req.DisplayName); {
	case n == 0:
		errs = append(errs, problem.FieldError{Field: "display_name", Code: "required"})
	case n > maxDisplayNameRunes:
		errs = append(errs, problem.FieldError{Field: "display_name", Code: "too_long"})
	}

	if req.PreferredLang == "" {
		req.PreferredLang = "sw"
	}
	if req.PreferredLang != "en" && req.PreferredLang != "sw" {
		errs = append(errs, problem.FieldError{Field: "preferred_lang", Code: "unsupported"})
	}

	if req.WardID <= 0 {
		errs = append(errs, problem.FieldError{Field: "ward_id", Code: "required"})
	}

	req.ConsentVersion = strings.TrimSpace(req.ConsentVersion)
	if req.ConsentVersion == "" {
		errs = append(errs, problem.FieldError{Field: "consent_version", Code: "required"})
	}
	return errs
}

// clientIP returns the connection's remote IP. Proxy headers are not trusted
// here; the gateway's trusted-proxy handling is configured with the API gateway.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
