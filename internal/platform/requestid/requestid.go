// Package requestid assigns every request a UUIDv7 correlation id and echoes
// it in the X-Request-ID response header (API Spec §1.3, finding F-19).
//
// Unlike chi's middleware.RequestID it does not embed the server hostname.
// Client-supplied ids are ignored so they cannot be used to forge log entries.
package requestid

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

const Header = "X-Request-ID"

// Middleware stores the id under chi's RequestIDKey so middleware.GetReqID works.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := "req_" + uuid.Must(uuid.NewV7()).String()
		w.Header().Set(Header, id)
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
