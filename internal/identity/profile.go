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
	MediaCDN string           // public base of processed media (profile photos)
	Logger   *slog.Logger
}

// Avatar is the user's profile photo.
type Avatar struct {
	MediaID string `json:"media_id"`
	URL     string `json:"url"`
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
	Avatar        *Avatar       `json:"avatar,omitempty"`
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
	if u.AvatarKey != "" {
		out.Avatar = &Avatar{MediaID: u.AvatarID, URL: h.MediaCDN + "/" + u.AvatarKey}
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

// SetAvatarRequest is the body of PUT /v1/users/me/avatar.
type SetAvatarRequest struct {
	MediaID string `json:"media_id"` // a photo uploaded through /v1/media and processed
}

// SetAvatar serves PUT /v1/users/me/avatar: uses one of the caller's own
// processed photos as their profile photo.
func (h *ProfileHandlers) SetAvatar(w http.ResponseWriter, r *http.Request) {
	u, ok := h.user(w, r)
	if !ok {
		return
	}
	var req SetAvatarRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	q := identitydb.New(h.Pool)
	var mediaRow int64
	id, err := uuid.Parse(req.MediaID)
	if err != nil {
		err = pgx.ErrNoRows // an unknown id: same answer as any unusable media
	} else {
		mediaRow, err = q.GetOwnReadyImage(r.Context(), identitydb.GetOwnReadyImageParams{PublicID: id, OwnerID: u.ID})
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		h.internal(w, r, "avatar media", err)
		return
	}
	if err != nil {
		// Not a photo, not the caller's, or not processed yet.
		problem.Write(w, r, http.StatusUnprocessableEntity, "media_not_ready", i18n.MsgMediaNotReady,
			problem.FieldError{Field: "media_id", Code: "not_ready"})
		return
	}
	if err := q.SetAvatar(r.Context(), identitydb.SetAvatarParams{ID: u.ID, MediaID: pgtype.Int8{Int64: mediaRow, Valid: true}}); err != nil {
		h.internal(w, r, "set avatar", err)
		return
	}
	h.reload(w, r, u)
}

// RemoveAvatar serves DELETE /v1/users/me/avatar: back to initials.
func (h *ProfileHandlers) RemoveAvatar(w http.ResponseWriter, r *http.Request) {
	u, ok := h.user(w, r)
	if !ok {
		return
	}
	if err := identitydb.New(h.Pool).SetAvatar(r.Context(), identitydb.SetAvatarParams{ID: u.ID}); err != nil {
		h.internal(w, r, "remove avatar", err)
		return
	}
	h.reload(w, r, u)
}

// reload answers with the profile as it now is.
func (h *ProfileHandlers) reload(w http.ResponseWriter, r *http.Request, u identitydb.GetProfileRow) {
	fresh, err := identitydb.New(h.Pool).GetProfile(r.Context(), u.PublicID)
	if err != nil {
		h.internal(w, r, "reload", err)
		return
	}
	h.write(w, r, fresh, http.StatusOK)
}
