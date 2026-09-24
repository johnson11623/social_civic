// Package authn authenticates requests with Bearer access tokens and carries
// the caller's identity through the request context.
package authn

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Verification errors returned by a Verifier.
var (
	ErrExpired = errors.New("authn: token expired")
	ErrInvalid = errors.New("authn: token invalid")
)

// Principal is the authenticated caller.
type Principal struct {
	Subject      string // user public id
	Ward         int    // IEBC codes of the user's scope
	Constituency int
	County       int
}

// Verifier validates an access token.
type Verifier interface {
	VerifyAccess(token string) (Principal, error)
}

// Rejecter writes the 401 response for code ("unauthenticated", "token_expired", "token_invalid").
type Rejecter func(w http.ResponseWriter, r *http.Request, code string)

type ctxKey struct{}

// Middleware requires a valid "Authorization: Bearer <access token>".
func Middleware(v Verifier, reject Rejecter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearer(r.Header.Get("Authorization"))
			if !ok {
				reject(w, r, "unauthenticated")
				return
			}
			p, err := v.VerifyAccess(token)
			switch {
			case errors.Is(err, ErrExpired):
				reject(w, r, "token_expired")
				return
			case err != nil:
				reject(w, r, "token_invalid")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
		})
	}
}

// FromContext returns the authenticated caller; ok is false outside Middleware.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

func bearer(h string) (string, bool) {
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	t := strings.TrimSpace(h[len(prefix):])
	return t, t != ""
}
