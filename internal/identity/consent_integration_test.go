package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/johnson11623/social_civic/internal/platform/authn"
)

// Story 1.1.3.1 against a real PostgreSQL, through the real middleware chain.

type consentFixture struct {
	*authFixture
	router http.Handler
	access string
}

func newConsentFixture(t *testing.T) *consentFixture {
	t.Helper()
	a := newAuthFixture(t)
	rec := a.post(t, validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	var reg RegisterResponse
	_ = json.NewDecoder(rec.Body).Decode(&reg)

	store := NewPostgresStore(a.pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	consent := &ConsentHandlers{Store: store, Logger: logger, Now: a.clock}
	requireAuth := authn.Middleware(a.auth.Tokens, Unauthenticated)

	r := chi.NewRouter()
	r.With(requireAuth).Post("/v1/users/me/consent/withdraw", consent.Withdraw)
	// Stand-in for a consent-dependent feature such as posting (T-1.1.3.3).
	r.With(requireAuth, RequireConsent(store, logger)).Post("/v1/channels/1/posts", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	return &consentFixture{authFixture: a, router: r, access: reg.AccessToken}
}

func (c *consentFixture) call(t *testing.T, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	return rec
}

func (c *consentFixture) withdraw(t *testing.T, version string) *httptest.ResponseRecorder {
	return c.call(t, "/v1/users/me/consent/withdraw", c.access, map[string]string{"version": version})
}

func (c *consentFixture) post(t *testing.T) int {
	return c.call(t, "/v1/channels/1/posts", c.access, map[string]string{"content": "hi"}).Code
}

func TestConsentIntegration_WithdrawThenFeatureIsBlocked(t *testing.T) {
	c := newConsentFixture(t)

	if code := c.post(t); code != http.StatusCreated {
		t.Fatalf("before withdrawal, posting = %d", code)
	}

	// T-1.1.3.1: withdraw → 200, withdrawn_at set.
	rec := c.withdraw(t, CurrentConsentVersion)
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body)
	}
	var resp WithdrawConsentResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.Withdrawn || resp.Version != CurrentConsentVersion || !resp.EffectiveAt.Equal(fixedNow) {
		t.Errorf("response = %+v", resp)
	}
	var withdrawnAt *time.Time
	_ = c.pool.QueryRow(context.Background(), "SELECT withdrawn_at FROM consents").Scan(&withdrawnAt)
	if withdrawnAt == nil || !withdrawnAt.Equal(fixedNow) {
		t.Errorf("consents.withdrawn_at = %v", withdrawnAt)
	}

	// T-1.1.3.3: withdraw, then post → 403 consent_required.
	rec = c.call(t, "/v1/channels/1/posts", c.access, map[string]string{"content": "hi"})
	if rec.Code != http.StatusForbidden || codeOf(t, rec) != "consent_required" {
		t.Errorf("after withdrawal, posting = %d %s", rec.Code, rec.Body)
	}

	// T-1.1.3.2 / T-1.1.3.4: consent.withdrawn enqueued in the same transaction
	// (it feeds the audit log), keyed by user id, without ID material.
	var topic, key, payload string
	err := c.pool.QueryRow(context.Background(),
		"SELECT topic, partition_key, payload::text FROM outbox WHERE topic = 'consent.withdrawn'").Scan(&topic, &key, &payload)
	if err != nil {
		t.Fatal(err)
	}
	var userID string
	_ = c.pool.QueryRow(context.Background(), "SELECT id::text FROM users").Scan(&userID)
	if key != userID || !strings.Contains(payload, `"consent_version": "2026-01"`) || strings.Contains(payload, "12345678") {
		t.Errorf("event key=%s payload=%s", key, payload)
	}
}

func TestConsentIntegration_WithdrawIsIdempotent(t *testing.T) {
	c := newConsentFixture(t)
	first := c.withdraw(t, CurrentConsentVersion)
	c.advance(5 * time.Minute) // within the access token's lifetime
	second := c.withdraw(t, CurrentConsentVersion)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("withdraw twice: %d, %d", first.Code, second.Code)
	}
	var a, b WithdrawConsentResponse
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if !a.EffectiveAt.Equal(b.EffectiveAt) {
		t.Errorf("second withdrawal changed effective_at: %s → %s", a.EffectiveAt, b.EffectiveAt)
	}
	if n := count(t, c.pool, "SELECT count(*) FROM outbox WHERE topic = 'consent.withdrawn'"); n != 1 {
		t.Errorf("consent.withdrawn events = %d, want 1", n)
	}
}

func TestConsentIntegration_Rejections(t *testing.T) {
	c := newConsentFixture(t)

	if rec := c.withdraw(t, "1999-01"); rec.Code != http.StatusNotFound || codeOf(t, rec) != "consent_not_found" {
		t.Errorf("unknown version: %d %s", rec.Code, rec.Body)
	}
	if rec := c.withdraw(t, " "); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("blank version: %d", rec.Code)
	}

	// No token, bad token, refresh token instead of access token → 401.
	pair, _ := c.auth.Tokens.Issue("x", ScopeClaim{})
	for name, tok := range map[string]string{"none": "", "garbage": "abc", "refresh token": pair.Refresh} {
		rec := c.call(t, "/v1/users/me/consent/withdraw", tok, map[string]string{"version": CurrentConsentVersion})
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: %d %v", name, rec.Code, rec.Header())
		}
	}
	// Expired access token → 401 token_expired.
	c.advance(AccessTokenTTL + time.Second)
	if rec := c.withdraw(t, CurrentConsentVersion); codeOf(t, rec) != "token_expired" {
		t.Errorf("expired token: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, c.pool, "SELECT count(*) FROM consents WHERE withdrawn_at IS NOT NULL"); n != 0 {
		t.Errorf("a rejected request withdrew consent")
	}
}

func TestConsentIntegration_SuspendedUserHasNoConsent(t *testing.T) {
	c := newConsentFixture(t)
	_, _ = c.pool.Exec(context.Background(), "UPDATE users SET state = 2")
	if code := c.post(t); code != http.StatusForbidden {
		t.Errorf("suspended user posting = %d, want 403", code)
	}
}
