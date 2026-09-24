package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/identity/identitydb"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Erasure deadlines (US-11): acknowledge within 48 hours, complete within 30 days.
const (
	ErasureAckWithin       = 48 * time.Hour
	ErasureCompleteWithin  = 30 * 24 * time.Hour
	ErasureStepMaxAttempts = 5
	StepIdentityAnonymize  = "identity.anonymize"
	StepMembershipRevoke   = "membership.revoke_roles"
	maxErasureReasonRunes  = 500
	erasureStatePending    = 1
	erasureStateCompleted  = 2
	erasureStepStateFailed = 3
	deletedUserDisplayName = "[deleted user]"
	erasedKeyVersion       = "erased"
)

// ErasureSteps are the saga steps created for every request. Membership
// revokes the user's roles (its step is registered by the worker); Channel
// & Post (tombstone authored content) adds its own when built. Posts already
// render the author as "[deleted user]" because the display name is
// overwritten. Group membership needs no step: it is the scope on the user
// row, which anonymization clears.
var ErasureSteps = []string{StepIdentityAnonymize, StepMembershipRevoke}

// RetainedAfterErasure is reported to the user and the audit log (T-1.1.3.8):
// what is kept after erasure and why.
var RetainedAfterErasure = []string{
	"users.id, users.public_id: pseudonymous key linking retained records and tombstoned content",
	"users.ward_id, constituency_id, county_id: administrative unit for aggregate statistics",
	"consents.version, granted_at, withdrawn_at: evidence of lawful processing (DPA 2019 s.30)",
	"erasure_requests: evidence the erasure was carried out (DPA 2019 s.40)",
}

// ErrErasureInProgress is returned when the user already has an open request.
var ErrErasureInProgress = errors.New("identity: erasure already requested")

// ErasureRequestedData is the payload of erasure.requested.
type ErasureRequestedData struct {
	UserID       int64     `json:"user_id"`
	PublicID     string    `json:"public_id"`
	RequestID    string    `json:"request_id"`
	CompletionBy time.Time `json:"completion_by"`
	Steps        []string  `json:"steps"`
}

// UserErasedData is the payload of user.erased.
type UserErasedData struct {
	UserID         int64     `json:"user_id"`
	PublicID       string    `json:"public_id"`
	RequestID      string    `json:"request_id"`
	ErasedAt       time.Time `json:"erased_at"`
	RetainedFields []string  `json:"retained_fields"`
}

// ErasureRequest is what the store records for a new request.
type ErasureRequest struct {
	PublicID     uuid.UUID
	UserPublicID uuid.UUID
	Reason       string
	RequestedAt  time.Time
	AckBy        time.Time
	CompletionBy time.Time
}

// ErasureStore persists erasure requests.
type ErasureStore interface {
	// RequestErasure records the request and its steps, revokes all of the
	// user's sessions, and enqueues event, atomically.
	RequestErasure(ctx context.Context, req ErasureRequest, event func(userID int64) events.Event) error
}

// RequestErasure implements ErasureStore.
func (s *PostgresStore) RequestErasure(ctx context.Context, req ErasureRequest, event func(int64) events.Event) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		u, err := q.GetUserIDByPublicID(ctx, req.UserPublicID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}
		if u.State == UserErased {
			return ErrErasureInProgress
		}
		id, err := q.InsertErasureRequest(ctx, identitydb.InsertErasureRequestParams{
			PublicID: req.PublicID, UserID: u.ID,
			Reason:      pgtype.Text{String: req.Reason, Valid: req.Reason != ""},
			RequestedAt: req.RequestedAt, AckBy: req.AckBy, CompletionBy: req.CompletionBy,
		})
		if err != nil {
			return err
		}
		for _, step := range ErasureSteps {
			if err := q.InsertErasureStep(ctx, identitydb.InsertErasureStepParams{RequestID: id, Step: step}); err != nil {
				return err
			}
		}
		// Stop processing at once: no session outlives the request.
		if _, err := q.RevokeAllUserRefreshTokens(ctx, identitydb.RevokeAllUserRefreshTokensParams{UserID: u.ID, Reason: "erasure"}); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, events.TopicErasureRequested, strconv.FormatInt(u.ID, 10), event(u.ID))
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "uniq_erasure_open" {
		return ErrErasureInProgress
	}
	return err
}

// ErasureRequestBody is the body of POST /v1/users/me/erasure.
type ErasureRequestBody struct {
	Reason string `json:"reason"`
}

// ErasureResponse is its 202 body.
type ErasureResponse struct {
	RequestID    string    `json:"request_id"`
	State        string    `json:"state"`
	AckBy        time.Time `json:"ack_by"`
	CompletionBy time.Time `json:"completion_by"`
	Retained     []string  `json:"retained"`
}

// ErasureHandlers serves POST /v1/users/me/erasure (T-1.1.3.5). Routes must be
// wrapped in authn.Middleware.
type ErasureHandlers struct {
	Store  ErasureStore
	Logger *slog.Logger
	Now    func() time.Time
}

// Request serves POST /v1/users/me/erasure.
func (h *ErasureHandlers) Request(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := authn.FromContext(ctx)
	userPublicID, err := uuid.Parse(p.Subject)
	if !ok || err != nil {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return
	}
	var body ErasureRequestBody
	if err := httpjson.DecodeStrict(w, r, &body); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if utf8.RuneCountInString(body.Reason) > maxErasureReasonRunes {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed,
			problem.FieldError{Field: "reason", Code: "too_long"})
		return
	}

	now := h.Now().UTC()
	req := ErasureRequest{
		PublicID: uuid.Must(uuid.NewV7()), UserPublicID: userPublicID, Reason: body.Reason,
		RequestedAt: now, AckBy: now.Add(ErasureAckWithin), CompletionBy: now.Add(ErasureCompleteWithin),
	}
	err = h.Store.RequestErasure(ctx, req, func(userID int64) events.Event {
		return events.New("identity", events.TopicErasureRequested, now, ErasureRequestedData{
			UserID: userID, PublicID: userPublicID.String(), RequestID: req.PublicID.String(),
			CompletionBy: req.CompletionBy, Steps: ErasureSteps,
		})
	})
	switch {
	case errors.Is(err, ErrErasureInProgress):
		problem.Write(w, r, http.StatusConflict, "erasure_in_progress", i18n.MsgErasureInProgress)
	case errors.Is(err, ErrUserNotFound):
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
	case err != nil:
		h.Logger.ErrorContext(ctx, "erasure request failed", "err", err)
		problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
	default:
		httpjson.Write(w, http.StatusAccepted, ErasureResponse{
			RequestID: req.PublicID.String(), State: "received",
			AckBy: req.AckBy, CompletionBy: req.CompletionBy, Retained: RetainedAfterErasure,
		})
	}
}

// StepFunc performs one erasure step inside the saga's transaction.
type StepFunc func(ctx context.Context, tx pgx.Tx, userID int64) error

// ErasureProcessor is the saga orchestrator run by the worker (T-1.1.3.7).
// Each step runs in a savepoint: success marks it done; failure is recorded
// and retried until ErasureStepMaxAttempts, after which the request is marked
// failed and shows on the DPO view (T-1.1.3.9).
type ErasureProcessor struct {
	Pool     *pgxpool.Pool
	Steps    map[string]StepFunc // default: DefaultErasureSteps()
	Logger   *slog.Logger
	Now      func() time.Time
	Interval time.Duration // idle poll interval, default 1s
}

// DefaultErasureSteps returns the step implementations owned by identity.
func DefaultErasureSteps() map[string]StepFunc {
	return map[string]StepFunc{StepIdentityAnonymize: anonymizeUser}
}

// anonymizeUser erases identity's PII for the user. The national ID hash is
// replaced with random bytes, so the ID can register again and the old hash
// no longer links to this record.
func anonymizeUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	q := identitydb.New(tx)
	placeholder := make([]byte, 32)
	if _, err := rand.Read(placeholder); err != nil {
		return err
	}
	for _, f := range []func() error{
		func() error { return q.DeleteVerificationAttemptsForUser(ctx, userID) },
		func() error {
			return q.AnonymizeUser(ctx, identitydb.AnonymizeUserParams{ID: userID, NationalIDHash: placeholder})
		},
		func() error { return q.ScrubConsentIPs(ctx, userID) },
		func() error { return q.DeleteUserOTPs(ctx, userID) },
		func() error { return q.DeleteMFA(ctx, userID) },
		func() error {
			_, err := q.RevokeAllUserRefreshTokens(ctx, identitydb.RevokeAllUserRefreshTokensParams{UserID: userID, Reason: "erasure"})
			return err
		},
	} {
		if err := f(); err != nil {
			return err
		}
	}
	return nil
}

// RunOnce executes at most one pending step. It reports whether a step was found.
func (p *ErasureProcessor) RunOnce(ctx context.Context) (bool, error) {
	steps := p.Steps
	if steps == nil {
		steps = DefaultErasureSteps()
	}
	found := false
	err := pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		s, err := q.ClaimPendingErasureStep(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true

		stepErr := errors.New("no implementation registered for step " + s.Step)
		if fn, ok := steps[s.Step]; ok {
			stepErr = pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error { return fn(ctx, sp, s.UserID) })
		}
		if stepErr != nil {
			state, err := q.RecordErasureStepFailure(ctx, identitydb.RecordErasureStepFailureParams{
				RequestID: s.RequestID, Step: s.Step, LastError: truncate(stepErr.Error(), 1000), MaxAttempts: ErasureStepMaxAttempts,
			})
			if err != nil {
				return err
			}
			p.Logger.ErrorContext(ctx, "erasure step failed", "request", s.RequestPublicID, "step", s.Step, "attempt", s.Attempts+1, "err", stepErr)
			if state == erasureStepStateFailed {
				p.Logger.ErrorContext(ctx, "ALERT erasure step gave up; DPO action required", "request", s.RequestPublicID, "step", s.Step)
				return q.FailErasureRequest(ctx, s.RequestID)
			}
			return nil
		}

		if err := q.MarkErasureStepDone(ctx, identitydb.MarkErasureStepDoneParams{RequestID: s.RequestID, Step: s.Step}); err != nil {
			return err
		}
		remaining, err := q.CountUnfinishedErasureSteps(ctx, s.RequestID)
		if err != nil || remaining > 0 {
			return err
		}
		now := p.Now().UTC()
		if err := q.CompleteErasureRequest(ctx, identitydb.CompleteErasureRequestParams{
			ID: s.RequestID, CompletedAt: &now, RetainedFields: RetainedAfterErasure,
		}); err != nil {
			return err
		}
		u, err := q.GetUserByID(ctx, s.UserID)
		if err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, events.TopicUserErased, strconv.FormatInt(s.UserID, 10),
			events.New("identity", events.TopicUserErased, now, UserErasedData{
				UserID: s.UserID, PublicID: u.PublicID.String(), RequestID: s.RequestPublicID.String(),
				ErasedAt: now, RetainedFields: RetainedAfterErasure,
			}))
	})
	return found, err
}

// Run processes steps until ctx is cancelled.
func (p *ErasureProcessor) Run(ctx context.Context) error {
	interval := p.Interval
	if interval <= 0 {
		interval = time.Second
	}
	for {
		found, err := p.RunOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			p.Logger.ErrorContext(ctx, "erasure processor failed", "err", err)
		}
		if found && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

// IncompleteErasure is one row of the DPO view (T-1.1.3.9).
type IncompleteErasure = identitydb.DpoIncompleteErasure

// ListIncompleteErasures returns every erasure not yet complete, overdue first.
func (s *PostgresStore) ListIncompleteErasures(ctx context.Context) ([]IncompleteErasure, error) {
	return identitydb.New(s.pool).ListIncompleteErasures(ctx)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return fmt.Sprintf("%s…", s[:n])
}
