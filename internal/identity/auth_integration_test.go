package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/sms"
	"github.com/johnson11623/social_civic/pkg/ratelimit"
)

// Login and refresh flows against a real PostgreSQL (TEST_DATABASE_URL).

type authFixture struct {
	*fixture
	auth *AuthHandlers
	sms  *sms.Recorder
	pool *pgxpool.Pool
	now  time.Time
	mu   sync.Mutex
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	f, pool := newIntegrationFixture(t)
	a := &authFixture{fixture: f, sms: &sms.Recorder{}, pool: pool, now: fixedNow}
	clock := func() time.Time { a.mu.Lock(); defer a.mu.Unlock(); return a.now }
	tokens, err := NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), clock)
	if err != nil {
		t.Fatal(err)
	}
	f.handler.Tokens, f.handler.Now = tokens, clock
	a.auth = &AuthHandlers{
		Store:   NewPostgresStore(pool),
		Keyring: f.handler.Keyring,
		Tokens:  tokens,
		SMS:     a.sms,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:     clock,
	}
	return a
}

func (a *authFixture) advance(d time.Duration) { a.mu.Lock(); a.now = a.now.Add(d); a.mu.Unlock() }

func (a *authFixture) do(t *testing.T, h http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.RemoteAddr = "196.201.214.10:5000"
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func (a *authFixture) register(t *testing.T) {
	t.Helper()
	if rec := a.post(t, validBody()); rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
}

var codeRe = regexp.MustCompile(`\b(\d{6})\b`)

// requestCode asks for a login code and returns it from the recorded SMS.
func (a *authFixture) requestCode(t *testing.T) string {
	t.Helper()
	before := len(a.sms.Sent())
	rec := a.do(t, a.auth.RequestOTP, map[string]string{"national_id": "12345678"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("otp: %d %s", rec.Code, rec.Body)
	}
	sent := a.sms.Sent()
	if len(sent) != before+1 {
		t.Fatalf("expected one new SMS, have %d", len(sent)-before)
	}
	m := codeRe.FindStringSubmatch(sent[len(sent)-1].Text)
	if m == nil {
		t.Fatalf("no code in SMS %q", sent[len(sent)-1].Text)
	}
	return m[1]
}

func (a *authFixture) login(t *testing.T, code string) *httptest.ResponseRecorder {
	t.Helper()
	return a.do(t, a.auth.Login, map[string]string{"national_id": "12345678", "otp": code})
}

func tokensOf(t *testing.T, rec *httptest.ResponseRecorder) TokenResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var tr TokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&tr); err != nil {
		t.Fatal(err)
	}
	return tr
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p problem.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Code
}

func TestAuthIntegration_OTPLoginHappyPath(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)

	code := a.requestCode(t)
	sent := a.sms.Sent()[0]
	if sent.To != "+254712345678" {
		t.Errorf("SMS sent to %q", sent.To)
	}
	if !bytes.Contains([]byte(sent.Text), []byte("dakika 5")) { // user's preferred language is sw
		t.Errorf("SMS not in Kiswahili: %q", sent.Text)
	}
	var stored int
	_ = a.pool.QueryRow(context.Background(), "SELECT count(*) FROM otp_challenges c WHERE c::text LIKE '%"+code+"%'").Scan(&stored)
	if stored != 0 {
		t.Error("login code stored in the clear")
	}

	rec := a.login(t, code)
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("token responses must not be cached")
	}
	tr := tokensOf(t, rec)
	claims, err := a.auth.Tokens.Parse(tr.AccessToken, TokenTypeAccess)
	if err != nil || claims.Scope == nil || claims.Scope.Ward != 551 {
		t.Fatalf("access claims = %+v, %v", claims, err)
	}
	if tr.TokenType != "Bearer" || tr.ExpiresIn != 900 {
		t.Errorf("token response = %+v", tr)
	}

	// A code works once.
	if rec := a.login(t, code); rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "invalid_credentials" {
		t.Errorf("reused code: %d %s", rec.Code, rec.Body)
	}
}

func TestAuthIntegration_WrongAndExpiredCodes(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)

	// T-1.1.2.3: wrong code → 401.
	code := a.requestCode(t)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if rec := a.login(t, wrong); rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "invalid_credentials" {
		t.Fatalf("wrong code: %d %s", rec.Code, rec.Body)
	}
	tokensOf(t, a.login(t, code)) // one wrong attempt doesn't burn the code

	// Expired code → 401 (T-1.1.2.1: codes expire in 5 minutes).
	code = a.requestCode(t)
	a.advance(DefaultOTPTTL + time.Second)
	if rec := a.login(t, code); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired code: %d", rec.Code)
	}
}

func TestAuthIntegration_CodeLocksAfterMaxAttempts(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)
	code := a.requestCode(t)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < DefaultOTPMaxAttempts; i++ {
		a.login(t, wrong)
	}
	if rec := a.login(t, code); rec.Code != http.StatusUnauthorized {
		t.Errorf("correct code accepted after %d wrong attempts: %d", DefaultOTPMaxAttempts, rec.Code)
	}
	// A fresh code works again.
	tokensOf(t, a.login(t, a.requestCode(t)))
}

func TestAuthIntegration_NewCodeReplacesOld(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)
	first := a.requestCode(t)
	second := a.requestCode(t)
	if first != second {
		if rec := a.login(t, first); rec.Code != http.StatusUnauthorized {
			t.Errorf("superseded code accepted: %d", rec.Code)
		}
	}
	tokensOf(t, a.login(t, second))
}

func TestAuthIntegration_NoEnumeration(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)

	unknown := a.do(t, a.auth.RequestOTP, map[string]string{"national_id": "87654321"})
	known := a.do(t, a.auth.RequestOTP, map[string]string{"national_id": "12345678"})
	if unknown.Code != http.StatusAccepted || unknown.Body.String() != known.Body.String() {
		t.Errorf("otp responses differ: unknown %d %s, known %d %s", unknown.Code, unknown.Body, known.Code, known.Body)
	}
	if len(a.sms.Sent()) != 1 {
		t.Errorf("SMS sent for an unregistered ID")
	}
	// Same 401 for unknown IDs and wrong codes.
	u := a.do(t, a.auth.Login, map[string]string{"national_id": "87654321", "otp": "123456"})
	k := a.login(t, "999999")
	if u.Code != http.StatusUnauthorized || codeOf(t, u) != codeOf(t, k) {
		t.Errorf("login errors differ: %d %s vs %d %s", u.Code, u.Body, k.Code, k.Body)
	}
}

func TestAuthIntegration_SuspendedUserCannotLogIn(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)
	code := a.requestCode(t)
	if _, err := a.pool.Exec(context.Background(), "UPDATE users SET state = 2"); err != nil {
		t.Fatal(err)
	}
	if rec := a.login(t, code); rec.Code != http.StatusUnauthorized {
		t.Errorf("suspended user logged in: %d", rec.Code)
	}
	before := len(a.sms.Sent())
	a.do(t, a.auth.RequestOTP, map[string]string{"national_id": "12345678"})
	if len(a.sms.Sent()) != before {
		t.Error("login code sent to a suspended user")
	}
}

func TestAuthIntegration_RefreshRotationAndReuseDetection(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)
	first := tokensOf(t, a.login(t, a.requestCode(t)))
	refresh := func(tok string) *httptest.ResponseRecorder {
		return a.do(t, a.auth.Refresh, map[string]string{"refresh_token": tok})
	}

	// T-1.1.2.4: valid refresh → new pair.
	second := tokensOf(t, refresh(first.RefreshToken))
	if second.RefreshToken == first.RefreshToken || second.AccessToken == first.AccessToken {
		t.Error("refresh must rotate both tokens")
	}
	third := tokensOf(t, refresh(second.RefreshToken))

	// F-03: replaying a rotated token revokes every session of the user.
	if rec := refresh(first.RefreshToken); rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "token_reuse_detected" {
		t.Fatalf("reuse: %d %s", rec.Code, rec.Body)
	}
	// T-1.1.2.5: the newest token was revoked with the family → 401 token_revoked.
	if rec := refresh(third.RefreshToken); rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "token_revoked" {
		t.Errorf("after reuse: %d %s", rec.Code, rec.Body)
	}
	var active int
	_ = a.pool.QueryRow(context.Background(), "SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL").Scan(&active)
	if active != 0 {
		t.Errorf("%d refresh tokens still active after reuse detection", active)
	}
	// Logging in again starts a clean session.
	tokensOf(t, refresh(tokensOf(t, a.login(t, a.requestCode(t))).RefreshToken))
}

func TestAuthIntegration_RefreshRejections(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)
	tr := tokensOf(t, a.login(t, a.requestCode(t)))
	refresh := func(tok string) *httptest.ResponseRecorder {
		return a.do(t, a.auth.Refresh, map[string]string{"refresh_token": tok})
	}

	if rec := refresh(tr.AccessToken); codeOf(t, rec) != "token_invalid" {
		t.Errorf("access token accepted as refresh: %d %s", rec.Code, rec.Body)
	}
	if rec := refresh("garbage"); codeOf(t, rec) != "token_invalid" {
		t.Errorf("garbage: %d %s", rec.Code, rec.Body)
	}
	// A validly signed token the server never stored (e.g. from a lost session save).
	orphan, _ := a.auth.Tokens.Issue("someone", ScopeClaim{})
	if rec := refresh(orphan.Refresh); codeOf(t, rec) != "token_invalid" {
		t.Errorf("unknown jti: %d %s", rec.Code, rec.Body)
	}
	// Suspended users cannot refresh.
	_, _ = a.pool.Exec(context.Background(), "UPDATE users SET state = 2")
	if rec := refresh(tr.RefreshToken); codeOf(t, rec) != "token_revoked" {
		t.Errorf("suspended refresh: %d %s", rec.Code, rec.Body)
	}
	_, _ = a.pool.Exec(context.Background(), "UPDATE users SET state = 1")
	// Expired refresh token.
	a.advance(RefreshTokenTTL + time.Second)
	if rec := refresh(tr.RefreshToken); codeOf(t, rec) != "token_expired" {
		t.Errorf("expired refresh: %d %s", rec.Code, rec.Body)
	}
}

func TestAuthIntegration_ConcurrentRefreshOnlyOneWins(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)
	tr := tokensOf(t, a.login(t, a.requestCode(t)))

	const n = 8
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- a.do(t, a.auth.Refresh, map[string]string{"refresh_token": tr.RefreshToken}).Code
		}()
	}
	wg.Wait()
	close(codes)
	ok := 0
	for c := range codes {
		if c == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d concurrent refreshes succeeded, want exactly 1", ok)
	}
}

// memLimiter is a per-test in-memory limiter.
type memLimiter struct {
	mu sync.Mutex
	n  map[string]int
}

func (m *memLimiter) Allow(_ context.Context, r ratelimit.Rule, key string) (ratelimit.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.n == nil {
		m.n = map[string]int{}
	}
	m.n[r.Name+key]++
	return ratelimit.Decision{Allowed: m.n[r.Name+key] <= r.Limit, Limit: r.Limit, RetryAfter: r.Window}, nil
}

func TestAuthIntegration_OTPRequestsLimitedPerNationalID(t *testing.T) {
	a := newAuthFixture(t)
	a.register(t)
	a.auth.Limiter = &memLimiter{}
	for i := 0; i < otpPerIDRule.Limit; i++ {
		a.requestCode(t)
	}
	rec := a.do(t, a.auth.RequestOTP, map[string]string{"national_id": "12345678"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("4th code request: %d %v", rec.Code, rec.Header())
	}
}
