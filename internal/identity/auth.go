package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/sms"
	"github.com/johnson11623/social_civic/pkg/kms"
	"github.com/johnson11623/social_civic/pkg/pii"
	"github.com/johnson11623/social_civic/pkg/ratelimit"
)

// Login defaults (T-1.1.2.1: codes expire in 5 minutes).
const (
	DefaultOTPTTL         = 5 * time.Minute
	DefaultOTPMaxAttempts = 5
	otpDigits             = 6
)

// otpPerIDRule limits code requests per national ID, independent of IP, so
// one person's phone cannot be flooded with SMS (and SMS costs stay bounded).
var otpPerIDRule = ratelimit.Rule{Name: "otp-id", Limit: 3, Window: 15 * time.Minute}

// AuthHandlers serves login (Feature 1.1.2):
//
//	POST /v1/auth/otp      request a login code by SMS
//	POST /v1/auth/login    exchange national ID + code for tokens
//	POST /v1/auth/refresh  rotate a refresh token
type AuthHandlers struct {
	Store          AuthStore
	Keyring        kms.Keyring
	Tokens         *TokenIssuer
	SMS            sms.Sender
	Limiter        ratelimit.Limiter // per-ID code limit; nil disables it
	Logger         *slog.Logger
	Now            func() time.Time
	OTPTTL         time.Duration
	OTPMaxAttempts int
}

// OTPRequest is the body of POST /v1/auth/otp.
type OTPRequest struct {
	NationalID string `json:"national_id"`
}

// OTPResponse is always the same whether or not the ID is registered, so the
// endpoint cannot be used to discover who is registered.
type OTPResponse struct {
	OTPRequested bool `json:"otp_requested"`
	ExpiresIn    int  `json:"expires_in"`
}

// LoginRequest is the body of POST /v1/auth/login.
type LoginRequest struct {
	NationalID string `json:"national_id"`
	OTP        string `json:"otp"`
}

// RefreshRequest is the body of POST /v1/auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// TokenResponse is returned by login and refresh.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (h *AuthHandlers) ttl() time.Duration {
	if h.OTPTTL > 0 {
		return h.OTPTTL
	}
	return DefaultOTPTTL
}

func (h *AuthHandlers) maxAttempts() int {
	if h.OTPMaxAttempts > 0 {
		return h.OTPMaxAttempts
	}
	return DefaultOTPMaxAttempts
}

// RequestOTP serves POST /v1/auth/otp (T-1.1.2.1).
func (h *AuthHandlers) RequestOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req OTPRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	if err := ValidateNationalID(req.NationalID); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "invalid_id", i18n.MsgInvalidNationalID)
		return
	}
	pepper, ok := h.pepper(w, r)
	if !ok {
		return
	}
	idHash := hashNationalID(req.NationalID, pepper.Material)

	if h.Limiter != nil {
		d, err := h.Limiter.Allow(ctx, otpPerIDRule, hex.EncodeToString(idHash[:16]))
		if err != nil {
			h.Logger.ErrorContext(ctx, "otp limiter unavailable; allowing", "err", err)
		} else if !d.Allowed {
			w.Header().Set("Retry-After", fmt.Sprint(int(d.RetryAfter.Seconds())+1))
			problem.Write(w, r, http.StatusTooManyRequests, "rate_limited", i18n.MsgRateLimited)
			return
		}
	}

	accepted := OTPResponse{OTPRequested: true, ExpiresIn: int(h.ttl().Seconds())}
	user, err := h.Store.FindUserByNationalIDHash(ctx, idHash)
	switch {
	case errors.Is(err, ErrUserNotFound):
		httpjson.Write(w, http.StatusAccepted, accepted)
		return
	case err != nil:
		h.fail(ctx, w, r, "find user", err)
		return
	case user.State != UserActive || user.MSISDNCiphertext == nil:
		h.Logger.InfoContext(ctx, "otp not sent", "user_id", user.ID, "state", user.State, "has_phone", user.MSISDNCiphertext != nil)
		httpjson.Write(w, http.StatusAccepted, accepted)
		return
	}

	code, err := generateOTP()
	if err != nil {
		h.fail(ctx, w, r, "generate otp", err)
		return
	}
	now := h.Now()
	if err := h.Store.CreateOTP(ctx, user.ID, hashOTP(user.ID, code, pepper.Material), pepper.Version, now, now.Add(h.ttl())); err != nil {
		h.fail(ctx, w, r, "store otp", err)
		return
	}
	phone, err := h.decryptPhone(ctx, user)
	if err != nil {
		h.fail(ctx, w, r, "decrypt phone", err)
		return
	}
	lang := i18n.Lang(user.PreferredLang)
	text := fmt.Sprintf(i18n.T(lang, i18n.MsgSMSLoginCode), code, int(h.ttl().Minutes()))
	if err := h.SMS.Send(ctx, phone, text); err != nil {
		// Answer as if sent: a different response would reveal the ID is registered.
		h.Logger.ErrorContext(ctx, "otp sms failed", "user_id", user.ID, "err", err)
	}
	httpjson.Write(w, http.StatusAccepted, accepted)
}

// Login serves POST /v1/auth/login (T-1.1.2.2, T-1.1.2.3).
// Every failure returns the same 401 so callers cannot tell which part was wrong.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req LoginRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	if err := ValidateNationalID(req.NationalID); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "invalid_id", i18n.MsgInvalidNationalID)
		return
	}
	if !isOTPFormat(req.OTP) {
		h.invalidCredentials(w, r)
		return
	}
	pepper, ok := h.pepper(w, r)
	if !ok {
		return
	}
	user, err := h.Store.FindUserByNationalIDHash(ctx, hashNationalID(req.NationalID, pepper.Material))
	if errors.Is(err, ErrUserNotFound) {
		h.invalidCredentials(w, r)
		return
	}
	if err != nil {
		h.fail(ctx, w, r, "find user", err)
		return
	}
	if user.State != UserActive {
		h.invalidCredentials(w, r)
		return
	}

	want := hashOTP(user.ID, req.OTP, pepper.Material)
	err = h.Store.VerifyOTP(ctx, user.ID, h.Now(), h.maxAttempts(), func(stored []byte) bool {
		return hmac.Equal(stored, want)
	})
	switch {
	case errors.Is(err, ErrNoActiveOTP), errors.Is(err, ErrOTPExpired), errors.Is(err, ErrOTPMismatch):
		h.invalidCredentials(w, r)
		return
	case err != nil:
		h.fail(ctx, w, r, "verify otp", err)
		return
	}

	tokens, err := h.Tokens.Issue(user.PublicID.String(), user.Scope)
	if err != nil {
		h.fail(ctx, w, r, "issue tokens", err)
		return
	}
	if err := h.Store.SaveRefreshToken(ctx, user.ID, uuid.Must(uuid.NewV7()), tokens); err != nil {
		h.fail(ctx, w, r, "save session", err)
		return
	}
	writeTokens(w, tokens)
}

// Refresh serves POST /v1/auth/refresh (T-1.1.2.4, T-1.1.2.5, F-03).
func (h *AuthHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req RefreshRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	claims, err := h.Tokens.Parse(req.RefreshToken, TokenTypeRefresh)
	switch {
	case errors.Is(err, ErrTokenExpired):
		problem.Write(w, r, http.StatusUnauthorized, "token_expired", i18n.MsgTokenExpired)
		return
	case err != nil:
		problem.Write(w, r, http.StatusUnauthorized, "token_invalid", i18n.MsgTokenInvalid)
		return
	}
	jti := uuid.MustParse(claims.ID) // Parse validated it

	tokens, err := h.Store.RotateRefreshToken(ctx, jti, h.Now(), func(u SessionUser) (TokenPair, error) {
		if u.PublicID.String() != claims.Subject {
			return TokenPair{}, errors.New("refresh token subject does not match its owner")
		}
		return h.Tokens.Issue(u.PublicID.String(), u.Scope)
	})
	switch {
	case errors.Is(err, ErrRefreshReused):
		h.Logger.WarnContext(ctx, "SECURITY refresh token reuse detected; all sessions revoked", "subject", claims.Subject, "jti", jti)
		problem.Write(w, r, http.StatusUnauthorized, "token_reuse_detected", i18n.MsgTokenReuseDetected)
	case errors.Is(err, ErrRefreshRevoked), errors.Is(err, ErrUserInactive):
		problem.Write(w, r, http.StatusUnauthorized, "token_revoked", i18n.MsgTokenRevoked)
	case errors.Is(err, ErrRefreshExpired):
		problem.Write(w, r, http.StatusUnauthorized, "token_expired", i18n.MsgTokenExpired)
	case errors.Is(err, ErrRefreshNotFound):
		problem.Write(w, r, http.StatusUnauthorized, "token_invalid", i18n.MsgTokenInvalid)
	case err != nil:
		h.fail(ctx, w, r, "rotate refresh token", err)
	default:
		writeTokens(w, tokens)
	}
}

func (h *AuthHandlers) pepper(w http.ResponseWriter, r *http.Request) (kms.Key, bool) {
	k, err := h.Keyring.Current(r.Context(), PepperKeyName)
	if err != nil {
		h.Logger.ErrorContext(r.Context(), "keyring unavailable", "err", err)
		problem.Write(w, r, http.StatusServiceUnavailable, "kms_unavailable", i18n.MsgRegistrationUnavailable)
		return kms.Key{}, false
	}
	return k, true
}

// decryptPhone opens the stored number. Only the current PII key version is
// supported until key rotation (T-X.4) adds versioned lookups.
func (h *AuthHandlers) decryptPhone(ctx context.Context, u LoginUser) (string, error) {
	key, err := h.Keyring.Current(ctx, PIIKeyName)
	if err != nil {
		return "", err
	}
	if key.Version != u.MSISDNKeyVersion {
		return "", fmt.Errorf("phone encrypted with key %s, current is %s", u.MSISDNKeyVersion, key.Version)
	}
	b, err := pii.Open(key.Material, u.MSISDNCiphertext, msisdnAAD(u.PublicID))
	return string(b), err
}

func (h *AuthHandlers) invalidCredentials(w http.ResponseWriter, r *http.Request) {
	problem.Write(w, r, http.StatusUnauthorized, "invalid_credentials", i18n.MsgInvalidCredentials)
}

func (h *AuthHandlers) fail(ctx context.Context, w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(ctx, "auth failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
}

func writeTokens(w http.ResponseWriter, t TokenPair) {
	w.Header().Set("Cache-Control", "no-store")
	httpjson.Write(w, http.StatusOK, TokenResponse{
		AccessToken: t.Access, RefreshToken: t.Refresh, TokenType: "Bearer", ExpiresIn: t.ExpiresIn,
	})
}

// generateOTP returns a uniformly random 6-digit code from crypto/rand.
func generateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", otpDigits, n.Int64()), nil
}

func isOTPFormat(s string) bool {
	if len(s) != otpDigits {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
