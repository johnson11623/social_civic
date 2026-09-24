package ratelimit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
)

// KeyFunc derives the rate-limit key for a request.
type KeyFunc func(*http.Request) string

// ByClientIP keys requests by the connection's IP, HMAC'd with secret so raw
// IPs (personal data under the DPA) are never written to Redis.
//
// Proxy headers are not trusted; once the API gateway fronts the service,
// the gateway's verified client IP must be used instead.
func ByClientIP(secret []byte) KeyFunc {
	return func(r *http.Request) string {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(ip))
		return hex.EncodeToString(mac.Sum(nil)[:16])
	}
}

// Rejecter writes the 429 response (the caller localizes it).
type Rejecter func(w http.ResponseWriter, r *http.Request)

// Middleware enforces rule per key and sets X-RateLimit-Limit,
// X-RateLimit-Remaining and, when limited, Retry-After (API Spec §1.3).
//
// If the limiter itself fails (Redis down), requests are allowed and the
// failure is logged: an outage of the counter store must not stop citizens
// from registering.
func Middleware(l Limiter, rule Rule, key KeyFunc, reject Rejecter, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d, err := l.Allow(r.Context(), rule, key(r))
			if err != nil {
				logger.ErrorContext(r.Context(), "rate limiter unavailable; allowing request", "rule", rule.Name, "err", err)
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Set("X-RateLimit-Limit", strconv.Itoa(d.Limit))
			h.Set("X-RateLimit-Remaining", strconv.Itoa(d.Remaining))
			if !d.Allowed {
				h.Set("Retry-After", strconv.Itoa(int(math.Ceil(d.RetryAfter.Seconds()))))
				reject(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
