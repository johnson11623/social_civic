package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/johnson11623/social_civic/internal/identity/identitydb"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// CurrentConsentVersion is the privacy notice version users consent to at
// registration. Features that process personal data require active consent
// to this version (RequireConsent).
const CurrentConsentVersion = "2026-01"

// EventConsentWithdrawn is published when a user withdraws consent (T-1.1.3.2).
const EventConsentWithdrawn = events.TopicConsentWithdrawn

// ErrConsentNotFound is returned when the user never granted that version.
var ErrConsentNotFound = errors.New("identity: consent not found")

// ConsentWithdrawnData is the payload of consent.withdrawn.
type ConsentWithdrawnData struct {
	UserID         int64     `json:"user_id"`
	PublicID       string    `json:"public_id"`
	ConsentVersion string    `json:"consent_version"`
	WithdrawnAt    time.Time `json:"withdrawn_at"`
}

// ConsentStore persists consent changes.
type ConsentStore interface {
	// WithdrawConsent marks the consent withdrawn and enqueues event, atomically.
	// Withdrawing an already-withdrawn consent returns the original time and
	// enqueues nothing.
	WithdrawConsent(ctx context.Context, publicID uuid.UUID, version string, at time.Time, event func(userID int64, at time.Time) events.Event) (time.Time, error)
	HasActiveConsent(ctx context.Context, publicID uuid.UUID, version string) (bool, error)
}

// WithdrawConsent implements ConsentStore.
func (s *PostgresStore) WithdrawConsent(ctx context.Context, publicID uuid.UUID, version string, at time.Time, event func(int64, time.Time) events.Event) (time.Time, error) {
	var withdrawnAt time.Time
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		u, err := q.GetUserIDByPublicID(ctx, publicID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}
		ts, err := q.WithdrawConsent(ctx, identitydb.WithdrawConsentParams{UserID: u.ID, Version: version, WithdrawnAt: &at})
		if errors.Is(err, pgx.ErrNoRows) {
			// Already withdrawn (idempotent) or never granted.
			c, err := q.GetConsent(ctx, identitydb.GetConsentParams{UserID: u.ID, Version: version})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrConsentNotFound
			}
			if err != nil {
				return err
			}
			withdrawnAt = *c.WithdrawnAt
			return nil
		}
		if err != nil {
			return err
		}
		withdrawnAt = *ts
		return outbox.Enqueue(ctx, tx, EventConsentWithdrawn, strconv.FormatInt(u.ID, 10), event(u.ID, withdrawnAt))
	})
	return withdrawnAt, err
}

// HasActiveConsent implements ConsentStore. Suspended and erased users have none.
func (s *PostgresStore) HasActiveConsent(ctx context.Context, publicID uuid.UUID, version string) (bool, error) {
	return identitydb.New(s.pool).HasActiveConsent(ctx, identitydb.HasActiveConsentParams{PublicID: publicID, Version: version})
}

// WithdrawConsentRequest is the body of POST /v1/users/me/consent/withdraw.
type WithdrawConsentRequest struct {
	Version string `json:"version"`
}

// WithdrawConsentResponse is its 200 body.
type WithdrawConsentResponse struct {
	Withdrawn   bool      `json:"withdrawn"`
	Version     string    `json:"version"`
	EffectiveAt time.Time `json:"effective_at"`
}

// ConsentHandlers serves consent endpoints (Story 1.1.3.1). Routes must be
// wrapped in authn.Middleware.
type ConsentHandlers struct {
	Store  ConsentStore
	Logger *slog.Logger
	Now    func() time.Time
}

// Withdraw serves POST /v1/users/me/consent/withdraw (T-1.1.3.1).
func (h *ConsentHandlers) Withdraw(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := authn.FromContext(ctx)
	publicID, err := uuid.Parse(p.Subject)
	if !ok || err != nil {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return
	}
	var req WithdrawConsentRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	req.Version = strings.TrimSpace(req.Version)
	if req.Version == "" || len(req.Version) > 32 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed,
			problem.FieldError{Field: "version", Code: "required"})
		return
	}

	at, err := h.Store.WithdrawConsent(ctx, publicID, req.Version, h.Now().UTC(), func(userID int64, at time.Time) events.Event {
		return events.New("identity", EventConsentWithdrawn, at, ConsentWithdrawnData{
			UserID: userID, PublicID: publicID.String(), ConsentVersion: req.Version, WithdrawnAt: at,
		})
	})
	switch {
	case errors.Is(err, ErrConsentNotFound):
		problem.Write(w, r, http.StatusNotFound, "consent_not_found", i18n.MsgConsentNotFound)
	case errors.Is(err, ErrUserNotFound):
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
	case err != nil:
		h.Logger.ErrorContext(ctx, "withdraw consent failed", "err", err)
		problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
	default:
		httpjson.Write(w, http.StatusOK, WithdrawConsentResponse{Withdrawn: true, Version: req.Version, EffectiveAt: at})
	}
}

// RequireConsent blocks features that depend on consent to processing
// (posting, interacting) once the caller has withdrawn it (T-1.1.3.3).
// It must run after authn.Middleware.
func RequireConsent(store ConsentStore, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := authn.FromContext(r.Context())
			publicID, err := uuid.Parse(p.Subject)
			if !ok || err != nil {
				problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
				return
			}
			active, err := store.HasActiveConsent(r.Context(), publicID, CurrentConsentVersion)
			if err != nil {
				logger.ErrorContext(r.Context(), "consent check failed", "err", err)
				problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
				return
			}
			if !active {
				problem.Write(w, r, http.StatusForbidden, "consent_required", i18n.MsgConsentWithdrawn)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Unauthenticated writes the localized 401 for authn.Middleware.
func Unauthenticated(w http.ResponseWriter, r *http.Request, code string) {
	msg := i18n.MsgUnauthenticated
	switch code {
	case "token_expired":
		msg = i18n.MsgTokenExpired
	case "token_invalid":
		msg = i18n.MsgTokenInvalid
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="civic-platform"`)
	problem.Write(w, r, http.StatusUnauthorized, code, msg)
}
