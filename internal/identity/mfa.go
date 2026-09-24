package identity

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/identity/identitydb"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/kms"
	"github.com/johnson11623/social_civic/pkg/pii"
)

// F-08 — TOTP enrolment and step-up.
//
// A user enrols an authenticator app once (enrol → activate with a code).
// Before a privileged action they step up: a fresh code buys an access
// token marked mfa, valid for the access token lifetime (15 min). Routes
// guard themselves with authn.RequireMFA(MFARequired).

// MFAHandlers serves /v1/users/me/mfa and /v1/auth/mfa behind authn.Middleware.
type MFAHandlers struct {
	Pool    *pgxpool.Pool
	Keyring kms.Keyring
	Tokens  *TokenIssuer
	Logger  *slog.Logger
	Now     func() time.Time
}

// MFARequired is the authn.RequireMFA rejection: 403 mfa_required.
func MFARequired(w http.ResponseWriter, r *http.Request) {
	problem.Write(w, r, http.StatusForbidden, "mfa_required", i18n.MsgMFARequired)
}

func mfaAD(userID int64) []byte { return []byte("user_mfa.secret:" + strconv.FormatInt(userID, 10)) }

func (h *MFAHandlers) internal(w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(r.Context(), "mfa failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
}

type mfaUser struct {
	id       int64
	publicID uuid.UUID
	scope    ScopeClaim
}

func (h *MFAHandlers) caller(w http.ResponseWriter, r *http.Request) (mfaUser, bool) {
	p, ok := authn.FromContext(r.Context())
	id, err := uuid.Parse(p.Subject)
	if !ok || err != nil {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return mfaUser{}, false
	}
	q := identitydb.New(h.Pool)
	row, err := q.GetUserIDByPublicID(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.State != 1) {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return mfaUser{}, false
	}
	if err != nil {
		h.internal(w, r, "caller", err)
		return mfaUser{}, false
	}
	u, err := q.GetUserByID(r.Context(), row.ID)
	if err != nil {
		h.internal(w, r, "caller scope", err)
		return mfaUser{}, false
	}
	return mfaUser{id: row.ID, publicID: id, scope: ScopeClaim{
		Ward: int(u.WardID), Constituency: int(u.ConstituencyID), County: int(u.CountyID),
	}}, true
}

// Status serves GET /v1/users/me/mfa.
func (h *MFAHandlers) Status(w http.ResponseWriter, r *http.Request) {
	u, ok := h.caller(w, r)
	if !ok {
		return
	}
	m, err := identitydb.New(h.Pool).GetMFA(r.Context(), u.id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		h.internal(w, r, "status", err)
		return
	}
	p, _ := authn.FromContext(r.Context())
	httpjson.Write(w, http.StatusOK, map[string]bool{"enabled": err == nil && m.EnabledAt != nil, "stepped_up": p.MFA})
}

// EnrolResponse is returned by POST /v1/users/me/mfa/totp.
type EnrolResponse struct {
	Secret     string `json:"secret"`      // base32, for typing into the app
	OTPAuthURI string `json:"otpauth_uri"` // for a QR code
}

// Enrol serves POST /v1/users/me/mfa/totp: a new secret, pending until a
// code from it is verified. 409 once MFA is enabled.
func (h *MFAHandlers) Enrol(w http.ResponseWriter, r *http.Request) {
	u, ok := h.caller(w, r)
	if !ok {
		return
	}
	secret, err := NewTOTPSecret()
	if err != nil {
		h.internal(w, r, "secret", err)
		return
	}
	key, err := h.Keyring.Current(r.Context(), PIIKeyName)
	if err != nil {
		h.internal(w, r, "key", err)
		return
	}
	sealed, err := pii.Seal(key.Material, secret, mfaAD(u.id))
	if err != nil {
		h.internal(w, r, "seal", err)
		return
	}
	n, err := identitydb.New(h.Pool).StartMFAEnrolment(r.Context(), identitydb.StartMFAEnrolmentParams{
		UserID: u.id, SecretEnc: sealed, KeyVersion: key.Version,
	})
	if err != nil {
		h.internal(w, r, "enrol", err)
		return
	}
	if n == 0 {
		problem.Write(w, r, http.StatusConflict, "mfa_already_enabled", i18n.MsgMFAAlreadyEnabled)
		return
	}
	httpjson.Write(w, http.StatusCreated, EnrolResponse{
		Secret: TOTPSecretText(secret), OTPAuthURI: TOTPURI(secret, u.publicID.String()[:8]),
	})
}

// CodeRequest carries a 6-digit authenticator code.
type CodeRequest struct {
	Code string `json:"code"`
}

// MFATokenResponse is a stepped-up access token.
type MFATokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Activate serves POST /v1/users/me/mfa/totp/verify: the first valid code
// enables MFA and steps the session up.
func (h *MFAHandlers) Activate(w http.ResponseWriter, r *http.Request) { h.verify(w, r, true) }

// StepUp serves POST /v1/auth/mfa: a valid code from the enrolled app
// returns an access token marked mfa.
func (h *MFAHandlers) StepUp(w http.ResponseWriter, r *http.Request) { h.verify(w, r, false) }

func (h *MFAHandlers) verify(w http.ResponseWriter, r *http.Request, activating bool) {
	u, ok := h.caller(w, r)
	if !ok {
		return
	}
	var req CodeRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	q := identitydb.New(h.Pool)
	m, err := q.GetMFA(r.Context(), u.id)
	switch {
	case errors.Is(err, pgx.ErrNoRows), err == nil && activating && m.EnabledAt != nil,
		err == nil && !activating && m.EnabledAt == nil:
		// Nothing pending to activate, or nothing enabled to step up with.
		problem.Write(w, r, http.StatusConflict, "mfa_not_enrolled", i18n.MsgMFANotEnrolled)
		return
	case err != nil:
		h.internal(w, r, "load", err)
		return
	}
	key, err := h.Keyring.Current(r.Context(), PIIKeyName)
	if err != nil {
		h.internal(w, r, "key", err)
		return
	}
	secret, err := pii.Open(key.Material, m.SecretEnc, mfaAD(u.id))
	if err != nil {
		h.internal(w, r, "open", err)
		return
	}
	step, ok := VerifyTOTP(secret, req.Code, h.Now(), m.LastStep)
	if !ok {
		problem.Write(w, r, http.StatusUnauthorized, "invalid_code", i18n.MsgMFAInvalidCode)
		return
	}
	n, err := q.AcceptMFAStep(r.Context(), identitydb.AcceptMFAStepParams{Step: step, UserID: u.id})
	if err != nil {
		h.internal(w, r, "accept", err)
		return
	}
	if n == 0 { // the same code was used concurrently
		problem.Write(w, r, http.StatusUnauthorized, "invalid_code", i18n.MsgMFAInvalidCode)
		return
	}
	token, expires, err := h.Tokens.IssueMFAAccess(u.publicID.String(), u.scope)
	if err != nil {
		h.internal(w, r, "token", err)
		return
	}
	httpjson.Write(w, http.StatusOK, MFATokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: expires})
}
