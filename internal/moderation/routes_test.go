package moderation

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// registerRoutes mounts the moderation API as cmd/api does (auth only; the
// consent guard and rate limits are covered elsewhere).
func registerRoutes(r chi.Router, auth func(http.Handler) http.Handler, h *Handlers) {
	r.With(auth).Post("/v1/reports", h.Report)
}
