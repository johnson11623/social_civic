package identity

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/pkg/kms"
)

// F-08 — TOTP enrolment and step-up against a real PostgreSQL.

type mfaFixture struct {
	router http.Handler
	tokens *TokenIssuer
	access string
	now    time.Time
}

func newMFAFixture(t *testing.T) *mfaFixture {
	t.Helper()
	pool := integrationPool(t)
	if _, err := pool.Exec(context.Background(), "TRUNCATE user_mfa"); err != nil {
		t.Fatal(err)
	}
	f := &mfaFixture{now: time.Unix(1_900_000_000, 0)}
	var err error
	f.tokens, err = NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	publicID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (public_id, national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
		VALUES ($1, $2, 'v1', 'Mod', 551, 111, 22)`, publicID, bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	pair, err := f.tokens.Issue(publicID.String(), ScopeClaim{Ward: 551, Constituency: 111, County: 22})
	if err != nil {
		t.Fatal(err)
	}
	f.access = pair.Access
	keyring := kms.NewStatic()
	keyring.Set(PIIKeyName, bytes.Repeat([]byte{9}, 32), "pii-v1")
	h := &MFAHandlers{Pool: pool, Keyring: keyring, Tokens: f.tokens,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return f.now }}
	auth := authn.Middleware(f.tokens, Unauthenticated)
	r := chi.NewRouter()
	r.With(auth).Get("/v1/users/me/mfa", h.Status)
	r.With(auth).Post("/v1/users/me/mfa/totp", h.Enrol)
	r.With(auth).Post("/v1/users/me/mfa/totp/verify", h.Activate)
	r.With(auth).Post("/v1/auth/mfa", h.StepUp)
	r.With(auth, authn.RequireMFA(MFARequired)).Post("/v1/privileged", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	f.router = r
	return f
}

func (f *mfaFixture) do(t *testing.T, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	method := http.MethodPost
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else if !strings.Contains(path, "totp") && !strings.HasSuffix(path, "privileged") {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func mfaCode(t *testing.T, secretText string, at time.Time) string {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretText)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(secret, totpStepAt(at))
}

func errCode(rec *httptest.ResponseRecorder) string {
	var p struct{ Code string }
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Code
}

func TestMFA_EnrolActivateAndStepUp(t *testing.T) {
	f := newMFAFixture(t)

	// Privileged routes refuse ordinary tokens with a code clients can act on.
	if rec := f.do(t, "/v1/privileged", f.access, nil); rec.Code != http.StatusForbidden || errCode(rec) != "mfa_required" {
		t.Fatalf("without MFA: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, "/v1/auth/mfa", f.access, map[string]string{"code": "123456"}); rec.Code != http.StatusConflict || errCode(rec) != "mfa_not_enrolled" {
		t.Errorf("step-up before enrolling: %d %s", rec.Code, rec.Body)
	}

	rec := f.do(t, "/v1/users/me/mfa/totp", f.access, nil)
	var enrol EnrolResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &enrol)
	if rec.Code != http.StatusCreated || len(enrol.Secret) != 32 || !strings.HasPrefix(enrol.OTPAuthURI, "otpauth://totp/") {
		t.Fatalf("enrol: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, "/v1/users/me/mfa/totp/verify", f.access, map[string]string{"code": "000000"}); rec.Code != http.StatusUnauthorized || errCode(rec) != "invalid_code" {
		t.Errorf("wrong code: %d %s", rec.Code, rec.Body)
	}
	code := mfaCode(t, enrol.Secret, f.now)
	rec = f.do(t, "/v1/users/me/mfa/totp/verify", f.access, map[string]string{"code": code})
	var stepped MFATokenResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &stepped)
	if rec.Code != http.StatusOK || stepped.AccessToken == "" || stepped.ExpiresIn != 900 {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, "/v1/privileged", stepped.AccessToken, nil); rec.Code != http.StatusNoContent {
		t.Errorf("stepped-up token refused: %d", rec.Code)
	}
	var status map[string]bool
	_ = json.Unmarshal(f.do(t, "/v1/users/me/mfa", stepped.AccessToken, nil).Body.Bytes(), &status)
	if !status["enabled"] || !status["stepped_up"] {
		t.Errorf("status = %v", status)
	}

	// Enabled: no re-enrolment; the used code can't be replayed.
	if rec := f.do(t, "/v1/users/me/mfa/totp", f.access, nil); rec.Code != http.StatusConflict || errCode(rec) != "mfa_already_enabled" {
		t.Errorf("re-enrol: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, "/v1/auth/mfa", f.access, map[string]string{"code": code}); rec.Code != http.StatusUnauthorized {
		t.Errorf("replayed code: %d", rec.Code)
	}
	// The next step's code steps up an ordinary session.
	f.now = f.now.Add(30 * time.Second)
	rec = f.do(t, "/v1/auth/mfa", f.access, map[string]string{"code": mfaCode(t, enrol.Secret, f.now)})
	_ = json.Unmarshal(rec.Body.Bytes(), &stepped)
	if rec.Code != http.StatusOK {
		t.Fatalf("step-up: %d %s", rec.Code, rec.Body)
	}
	c, err := f.tokens.Parse(stepped.AccessToken, TokenTypeAccess)
	if err != nil || !c.MFA || c.Scope == nil || c.Scope.Ward != 551 {
		t.Errorf("stepped-up claims = %+v, %v", c, err)
	}
}
