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
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/kms"
)

// EventUserRegistered is the CloudEvents type for a completed registration.
const EventUserRegistered = events.TopicUserRegistered

const maxDisplayNameRunes = 100

// BoundaryResolver resolves a ward to its constituency, county and national units.
type BoundaryResolver interface {
	ResolveWard(code int) (boundary.Scope, error)
}

// RegisterRequest is the body of POST /v1/auth/register.
type RegisterRequest struct {
	NationalID     string `json:"national_id"`
	DisplayName    string `json:"display_name"`
	PreferredLang  string `json:"preferred_lang"`
	WardID         int    `json:"ward_id"` // IEBC ward code, 1–1450
	ConsentVersion string `json:"consent_version"`
	ConsentGranted bool   `json:"consent_granted"`
}

// Group is one of the four memberships a new user joins. ID is the IEBC code
// within the level (national is 1).
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
	WardID         int    `json:"ward_id"` // IEBC ward code, 1–1450
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
	Logger   *slog.Logger
	Now      func() time.Time
}

func (h *RegisterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req RegisterRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}

	// Validate everything before touching the keyring or database, so a
	// rejected request leaves no trace of the national ID.
	if err := ValidateNationalID(req.NationalID); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "invalid_id", i18n.MsgInvalidNationalID)
		return
	}
	if fieldErrs := validateRegister(&req); len(fieldErrs) > 0 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed, fieldErrs...)
		return
	}
	if !req.ConsentGranted {
		problem.Write(w, r, http.StatusUnprocessableEntity, "consent_required", i18n.MsgConsentRequired)
		return
	}

	scope, err := h.Boundary.ResolveWard(req.WardID)
	if errors.Is(err, boundary.ErrUnknownWard) {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_unit", i18n.MsgUnknownWard,
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
		problem.Write(w, r, http.StatusServiceUnavailable, "kms_unavailable", i18n.MsgRegistrationUnavailable)
		return
	}

	now := h.Now().UTC()
	// user.registered is enqueued in the same transaction as the user, so it is
	// published if and only if the registration commits (T-1.1.1.7).
	userRegistered := func(c CreatedUser) events.Event {
		return events.New("identity", EventUserRegistered, now, UserRegisteredData{
			UserID:         c.ID,
			PublicID:       c.PublicID.String(),
			WardID:         scope.Ward.Code,
			ConstituencyID: scope.Constituency.Code,
			CountyID:       scope.County.Code,
			ConsentVersion: req.ConsentVersion,
			KeyVersion:     pepper.Version,
		})
	}
	created, err := h.Store.CreateUser(ctx, NewUser{
		PublicID:       uuid.Must(uuid.NewV7()),
		NationalIDHash: hashNationalID(req.NationalID, pepper.Material),
		KeyVersion:     pepper.Version,
		DisplayName:    req.DisplayName,
		PreferredLang:  req.PreferredLang,
		WardID:         int32(scope.Ward.Code),
		ConstituencyID: int32(scope.Constituency.Code),
		CountyID:       int32(scope.County.Code),
		ConsentVersion: req.ConsentVersion,
		ConsentAt:      now,
		IPHash:         hashIP(clientIP(r), pepper.Material),
	}, userRegistered)
	if errors.Is(err, ErrDuplicateNationalID) {
		problem.Write(w, r, http.StatusConflict, "id_already_registered", i18n.MsgIDAlreadyRegistered)
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

	httpjson.Write(w, http.StatusCreated, RegisterResponse{
		UserID:        publicID,
		PublicID:      publicID,
		DisplayName:   req.DisplayName,
		PreferredLang: req.PreferredLang,
		Groups: []Group{
			{Level: 1, ID: scope.Ward.Code, Name: scope.Ward.DisplayName},
			{Level: 2, ID: scope.Constituency.Code, Name: scope.Constituency.DisplayName},
			{Level: 3, ID: scope.County.Code, Name: scope.County.DisplayName},
			{Level: 4, ID: scope.National.Code, Name: scope.National.DisplayName},
		},
		AccessToken:  tokens.Access,
		RefreshToken: tokens.Refresh,
		ExpiresIn:    tokens.ExpiresIn,
	})
}

func (h *RegisterHandler) fail(ctx context.Context, w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(ctx, "register failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
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
