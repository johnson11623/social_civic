package moderation

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// registerRoutes mounts the moderation API as cmd/api does (auth only; the
// consent guard and rate limits are covered elsewhere).
func registerRoutes(r chi.Router, auth func(http.Handler) http.Handler, h *Handlers) {
	r.With(auth).Post("/v1/reports", h.Report)
	r.With(auth).Post("/v1/moderation/actions", h.Act)
	r.With(auth).Get("/v1/moderation/queue", h.Queue)
	r.With(auth).Get("/v1/posts/{post_id}/moderation", h.History)
	r.With(auth).Post("/v1/appeals", h.File)
	r.With(auth).Get("/v1/appeals", h.Open)
	r.With(auth).Post("/v1/appeals/{appeal_id}/decision", h.Decide)
}
