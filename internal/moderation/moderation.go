// Package moderation handles reports, harm-based moderation actions and
// appeals (EPIC 3.1).
package moderation

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/membership"
	"github.com/johnson11623/social_civic/internal/moderation/moderationdb"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/post"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Reasons is the harm-based policy (T-3.1.2.1): the only grounds on which
// a post may be reported or removed. Criticism, disagreement or unpopular
// opinions are not among them.
var Reasons = []string{"hate_speech", "incitement", "privacy", "child_safety"}

// ValidReason reports whether code is in the policy.
func ValidReason(code string) bool {
	for _, r := range Reasons {
		if r == code {
			return true
		}
	}
	return false
}

// Queues name the moderator role that reviews a post at each level.
var Queues = map[int16]string{
	post.LevelWard: membership.RoleWardMod, post.LevelConstituency: membership.RoleConstMod,
	post.LevelCounty: membership.RoleCountyMod, post.LevelNational: membership.RoleNatMod,
}

const (
	maxDetailsRunes = 500
	maxNotesRunes   = 1000

	reportOpen      = 1
	reportActioned  = 2
	reportDismissed = 3
)

// Handlers serves the moderation API behind authn.Middleware.
type Handlers struct {
	Pool    *pgxpool.Pool
	Posts   *post.Store
	Members *membership.Handlers // caller and roles
	Cache   post.FeedCache       // optional: feed invalidation after actions
	Logger  *slog.Logger
	Now     func() time.Time
}

func (h *Handlers) internal(w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(r.Context(), "moderation failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
}

// viewer adapts a member to the post package's visibility check.
func viewer(m membership.Member) post.ActiveUser {
	return post.ActiveUser{ID: m.ID, PublicID: m.PublicID, WardID: m.Ward, ConstituencyID: m.Constituency, CountyID: m.County}
}

// loadPost resolves a post id from a request body, writing 404 when unknown.
func (h *Handlers) loadPost(w http.ResponseWriter, r *http.Request, raw string) (post.Post, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgPostNotFound)
		return post.Post{}, false
	}
	p, err := h.Posts.PostByPublicID(r.Context(), id)
	if errors.Is(err, post.ErrNotFound) {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgPostNotFound)
		return post.Post{}, false
	}
	if err != nil {
		h.internal(w, r, "load post", err)
		return post.Post{}, false
	}
	return p, true
}

// ---- Reports (Feature 3.1.1) --------------------------------------------------------

// ErrAlreadyReported is returned for a second report of a post by one user.
var ErrAlreadyReported = errors.New("moderation: already reported")

// PostReportedData is the payload of post.reported (T-3.1.1.5).
type PostReportedData struct {
	ReportID   string    `json:"report_id"`
	PostID     string    `json:"post_id"`
	ReporterID int64     `json:"reporter_id"`
	ReasonCode string    `json:"reason_code"`
	PostLevel  int16     `json:"post_level"`
	Queue      string    `json:"queue"`
	CreatedAt  time.Time `json:"created_at"`
}

// ReportRequest is the body of POST /v1/reports.
type ReportRequest struct {
	PostID     string `json:"post_id"`
	ReasonCode string `json:"reason_code"`
	Details    string `json:"details"`
}

// ReportResponse is returned by POST /v1/reports.
type ReportResponse struct {
	ReportID string `json:"report_id"`
	State    string `json:"state"`
	Queue    string `json:"queue"`
}

// Report serves POST /v1/reports (T-3.1.1.2–T-3.1.1.5): a member reports a
// post they can see, for a harm in the policy; it lands in the queue of the
// moderators at the post's current level.
func (h *Handlers) Report(w http.ResponseWriter, r *http.Request) {
	me, _, ok := h.Members.Caller(w, r)
	if !ok {
		return
	}
	var req ReportRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	if !ValidReason(req.ReasonCode) {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_reason", i18n.MsgInvalidReason,
			problem.FieldError{Field: "reason_code", Code: "invalid"})
		return
	}
	details := strings.TrimSpace(req.Details)
	if utf8.RuneCountInString(details) > maxDetailsRunes {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgDetailsTooLong,
			problem.FieldError{Field: "details", Code: "too_long"})
		return
	}
	p, ok := h.loadPost(w, r, req.PostID)
	if !ok {
		return
	}
	if !post.CanView(viewer(me), p.Level, p.WardID, p.ConstituencyID, p.CountyID) {
		problem.Write(w, r, http.StatusForbidden, "out_of_scope", i18n.MsgOutOfScope)
		return
	}
	if p.State != post.StateActive && p.State != post.StateFrozen {
		problem.Write(w, r, http.StatusConflict, "post_not_active", i18n.MsgPostNotActive)
		return
	}

	queue := Queues[p.Level]
	id := uuid.Must(uuid.NewV7())
	err := pgx.BeginFunc(r.Context(), h.Pool, func(tx pgx.Tx) error {
		row, err := moderationdb.New(tx).InsertReport(r.Context(), moderationdb.InsertReportParams{
			PublicID: id, PostID: p.ID, ReporterID: me.ID, ReasonCode: req.ReasonCode,
			Details: pgtype.Text{String: details, Valid: details != ""}, PostLevel: p.Level,
		})
		if err != nil {
			return err
		}
		return outbox.Enqueue(r.Context(), tx, events.TopicPostReported, p.PublicID.String(),
			events.New("moderation", events.TopicPostReported, row.CreatedAt, PostReportedData{
				ReportID: id.String(), PostID: p.PublicID.String(), ReporterID: me.ID, ReasonCode: req.ReasonCode,
				PostLevel: p.Level, Queue: queue, CreatedAt: row.CreatedAt,
			}))
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.ConstraintName == "reports_one_per_user":
		problem.Write(w, r, http.StatusConflict, "already_reported", i18n.MsgAlreadyReported)
	case err != nil:
		h.internal(w, r, "report", err)
	default:
		httpjson.Write(w, http.StatusCreated, ReportResponse{ReportID: id.String(), State: "open", Queue: queue})
	}
}

// txFunc runs fn in a transaction on the pool.
func (h *Handlers) tx(ctx context.Context, fn func(q *moderationdb.Queries, tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, h.Pool, func(tx pgx.Tx) error { return fn(moderationdb.New(tx), tx) })
}
