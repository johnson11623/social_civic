package ratelimit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// memLimiter is an in-memory fixed window for middleware tests.
type memLimiter struct {
	mu     sync.Mutex
	counts map[string]int
	err    error
}

func (m *memLimiter) Allow(_ context.Context, rule Rule, key string) (Decision, error) {
	if m.err != nil {
		return Decision{}, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counts == nil {
		m.counts = map[string]int{}
	}
	m.counts[rule.Name+key]++
	n := m.counts[rule.Name+key]
	return Decision{Allowed: n <= rule.Limit, Limit: rule.Limit, Remaining: max(0, rule.Limit-n), RetryAfter: 1500 * time.Millisecond}, nil
}

var registerRule = Rule{Name: "register", Limit: 5, Window: time.Hour}

func handler(l Limiter) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	reject := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }
	return Middleware(l, registerRule, ByClientIP([]byte("test-secret")), reject, slog.New(slog.NewTextHandler(io.Discard, nil)))(ok)
}

func call(h http.Handler, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", nil)
	req.RemoteAddr = ip + ":40000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Backlog T-1.1.1.10: 6 registrations from one IP in an hour → the 6th is 429.
func TestMiddlewareSixthRequestIsLimited(t *testing.T) {
	h := handler(&memLimiter{})
	for i := 1; i <= 5; i++ {
		rec := call(h, "196.201.214.10")
		if rec.Code != http.StatusCreated {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
		if got := rec.Header().Get("X-RateLimit-Remaining"); got != string(rune('0'+5-i)) {
			t.Errorf("request %d: remaining = %s", i, got)
		}
	}
	rec := call(h, "196.201.214.10")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request: status %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" { // 1.5s rounds up
		t.Errorf("Retry-After = %q, want 2", got)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "5" || rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestMiddlewareKeysByIP(t *testing.T) {
	h := handler(&memLimiter{})
	for i := 0; i < 6; i++ {
		call(h, "196.201.214.10")
	}
	if rec := call(h, "41.90.64.1"); rec.Code != http.StatusCreated {
		t.Errorf("a different IP was limited: %d", rec.Code)
	}
}

func TestMiddlewareFailsOpenWhenLimiterIsDown(t *testing.T) {
	h := handler(&memLimiter{err: errors.New("redis: connection refused")})
	for i := 0; i < 10; i++ {
		if rec := call(h, "196.201.214.10"); rec.Code != http.StatusCreated {
			t.Fatalf("request %d: status %d; limiter outage must not block registration", i, rec.Code)
		}
	}
}

func TestByClientIPNeverExposesTheIP(t *testing.T) {
	key := ByClientIP([]byte("secret"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "196.201.214.10:1234"
	k1 := key(req)
	if strings.Contains(k1, "196") || len(k1) != 32 {
		t.Errorf("key = %q", k1)
	}
	req.RemoteAddr = "196.201.214.10:5678" // same IP, different port
	if key(req) != k1 {
		t.Error("key must not depend on the source port")
	}
	req.Header.Set("X-Forwarded-For", "1.2.3.4") // spoofable header is ignored
	if key(req) != k1 {
		t.Error("key must ignore X-Forwarded-For")
	}
	if ByClientIP([]byte("other"))(req) == k1 {
		t.Error("key must depend on the secret")
	}
}
