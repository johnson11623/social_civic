package identity

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/identity/identitydb"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
)

// ProfileHandlers serves GET and PATCH /v1/users/me behind authn.Middleware:
// what the account page shows and the settings a user may change.
type ProfileHandlers struct {
	Pool     *pgxpool.Pool
	Boundary BoundaryResolver // names the ward
	Logger   *slog.Logger
}

// WardRef names the user's ward and its parents.
type WardRef struct {
	WardID       int    `json:"ward_id"`
	Name         string `json:"name"`
	Constituency string `json:"constituency"`
	County       string `json:"county"`
}

// ConsentState is the user's consent to the current policy version.
type ConsentState struct {
	Version     string     `json:"version"`
	Active      bool       `json:"active"`
	GrantedAt   *time.Time `json:"granted_at,omitempty"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty"`
}

// ErasureState is an erasure request still in progress.
type ErasureState struct {
	RequestID    string    `json:"request_id"`
	State        string    `json:"state"` // pending | failed (the DPO is handling it)
	RequestedAt  time.Time `json:"requested_at"`
	CompletionBy time.Time `json:"completion_by"`
}

// Profile is the body of GET and PATCH /v1/users/me.
type Profile struct {
	PublicID      string        `json:"public_id"`
	DisplayName   string        `json:"display_name"`
	PreferredLang string        `json:"preferred_lang"`
	MemberSince   time.Time     `json:"member_since"`
	Ward          *WardRef      `json:"ward,omitempty"`
	Consent       ConsentState  `json:"consent"`
	MFAEnabled    bool          `json:"mfa_enabled"`
	Erasure       *ErasureState `json:"erasure,omitempty"`
}

// UpdateProfileRequest is the body of PATCH /v1/users/me; omitted fields
// stay as they are.
type UpdateProfileRequest struct {
	DisplayName   *string `json:"display_name"`
	PreferredLang *string `json:"preferred_lang"`
}

func (h *ProfileHandlers) internal(w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(r.Context(), "profile failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
}

func (h *ProfileHandlers) user(w http.ResponseWriter, r *http.Request) (identitydb.GetProfileRow, bool) {
	p, ok := authn.FromContext(r.Context())
	id, err := uuid.Parse(p.Subject)
	if !ok || err != nil {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return identitydb.GetProfileRow{}, false
	}
	u, err := identitydb.New(h.Pool).GetProfile(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return u, false
	}
	if err != nil {
		h.internal(w, r, "load", err)
		return u, false
	}
	return u, true
}

// Get serves GET /v1/users/me.
func (h *ProfileHandlers) Get(w http.ResponseWriter, r *http.Request) {
	u, ok := h.user(w, r)
	if !ok {
		return
	}
	h.write(w, r, u, http.StatusOK)
}

func (h *ProfileHandlers) write(w http.ResponseWriter, r *http.Request, u identitydb.GetProfileRow, status int) {
	ctx := r.Context()
	q := identitydb.New(h.Pool)
	out := Profile{
		PublicID: u.PublicID.String(), DisplayName: u.DisplayName, PreferredLang: u.PreferredLang, MemberSince: u.CreatedAt,
		Consent: ConsentState{Version: CurrentConsentVersion},
	}
	if h.Boundary != nil {
		if s, err := h.Boundary.ResolveWard(int(u.WardID)); err == nil {
			out.Ward = &WardRef{WardID: int(u.WardID), Name: s.Ward.DisplayName,
				Constituency: s.Constituency.DisplayName, County: s.County.DisplayName}
		}
	}
	c, err := q.GetConsent(ctx, identitydb.GetConsentParams{UserID: u.ID, Version: CurrentConsentVersion})
	switch {
	case err == nil:
		granted := c.GrantedAt
		out.Consent.GrantedAt, out.Consent.WithdrawnAt = &granted, c.WithdrawnAt
		out.Consent.Active = c.WithdrawnAt == nil
	case !errors.Is(err, pgx.ErrNoRows):
		h.internal(w, r, "consent", err)
		return
	}
	m, err := q.GetMFA(ctx, u.ID)
	switch {
	case err == nil:
		out.MFAEnabled = m.EnabledAt != nil
	case !errors.Is(err, pgx.ErrNoRows):
		h.internal(w, r, "mfa", err)
		return
	}
	e, err := q.GetOpenErasure(ctx, u.ID)
	switch {
	case err == nil:
		state := "pending"
		if e.State == erasureStateFailed {
			state = "failed"
		}
		out.Erasure = &ErasureState{RequestID: e.PublicID.String(), State: state, RequestedAt: e.RequestedAt, CompletionBy: e.CompletionBy}
	case !errors.Is(err, pgx.ErrNoRows):
		h.internal(w, r, "erasure", err)
		return
	}
	httpjson.Write(w, status, out)
}

// Update serves PATCH /v1/users/me: display name and preferred language
// (for SMS and email; the web app's language is the user's own choice).
func (h *ProfileHandlers) Update(w http.ResponseWriter, r *http.Request) {
	u, ok := h.user(w, r)
	if !ok {
		return
	}
	var req UpdateProfileRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	var errs []problem.FieldError
	params := identitydb.UpdateProfileParams{ID: u.ID}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		switch n := utf8.RuneCountInString(name); {
		case n == 0:
			errs = append(errs, problem.FieldError{Field: "display_name", Code: "required"})
		case n > maxDisplayNameRunes:
			errs = append(errs, problem.FieldError{Field: "display_name", Code: "too_long"})
		}
		params.DisplayName = pgtype.Text{String: name, Valid: true}
	}
	if req.PreferredLang != nil {
		if *req.PreferredLang != "en" && *req.PreferredLang != "sw" {
			errs = append(errs, problem.FieldError{Field: "preferred_lang", Code: "unsupported"})
		}
		params.PreferredLang = pgtype.Text{String: *req.PreferredLang, Valid: true}
	}
	if len(errs) > 0 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed, errs...)
		return
	}
	row, err := identitydb.New(h.Pool).UpdateProfile(r.Context(), params)
	if err != nil {
		h.internal(w, r, "update", err)
		return
	}
	u.DisplayName, u.PreferredLang = row.DisplayName, row.PreferredLang
	h.write(w, r, u, http.StatusOK)
}
