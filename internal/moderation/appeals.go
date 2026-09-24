package moderation

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/johnson11623/social_civic/internal/membership"
	"github.com/johnson11623/social_civic/internal/moderation/moderationdb"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/post"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Feature 3.1.3 — appeals.

// DecisionWindow is how long the committee has to decide (due_at).
const DecisionWindow = 14 * 24 * time.Hour

const (
	maxStatementRunes = 2000

	appealOpen       = 1
	appealUpheld     = 2
	appealOverturned = 3
)

var appealStates = map[int16]string{appealOpen: "open", appealUpheld: "upheld", appealOverturned: "overturned"}

// AppealFiledData is the payload of appeal.filed (T-3.1.3.3).
type AppealFiledData struct {
	AppealID    string    `json:"appeal_id"`
	ActionID    string    `json:"action_id"`
	PostID      string    `json:"post_id"`
	AppellantID int64     `json:"appellant_id"`
	ModeratorID int64     `json:"moderator_id"`
	FiledAt     time.Time `json:"filed_at"`
	DueAt       time.Time `json:"due_at"`
}

// AppealResolvedData is the payload of appeal.resolved (T-3.1.3.5); the
// notification service tells both the author and the moderator (T-3.1.3.6).
type AppealResolvedData struct {
	AppealID    string    `json:"appeal_id"`
	ActionID    string    `json:"action_id"`
	PostID      string    `json:"post_id"`
	Decision    string    `json:"decision"`
	AuthorID    int64     `json:"author_id"`
	ModeratorID int64     `json:"moderator_id"`
	DecidedBy   int64     `json:"decided_by"`
	PostState   int16     `json:"post_state"`
	DecidedAt   time.Time `json:"decided_at"`
}

// AppealRequest is the body of POST /v1/appeals.
type AppealRequest struct {
	ModerationID string `json:"moderation_id"`
	Statement    string `json:"statement"`
}

// AppealResponse is returned by POST /v1/appeals.
type AppealResponse struct {
	AppealID string    `json:"appeal_id"`
	State    string    `json:"state"`
	DueAt    time.Time `json:"due_at"`
}

// File serves POST /v1/appeals (T-3.1.3.1–T-3.1.3.3): the author of a
// moderated post appeals the decision within its 14-day window.
func (h *Handlers) File(w http.ResponseWriter, r *http.Request) {
	me, _, ok := h.Members.Caller(w, r)
	if !ok {
		return
	}
	var req AppealRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	statement := strings.TrimSpace(req.Statement)
	if n := utf8.RuneCountInString(statement); n == 0 || n > maxStatementRunes {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgStatementInvalid,
			problem.FieldError{Field: "statement", Code: "invalid_length"})
		return
	}
	actionID, err := uuid.Parse(req.ModerationID)
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "action_not_found", i18n.MsgActionNotFound)
		return
	}
	a, err := moderationdb.New(h.Pool).GetActionForAppeal(r.Context(), actionID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem.Write(w, r, http.StatusNotFound, "action_not_found", i18n.MsgActionNotFound)
		return
	}
	if err != nil {
		h.internal(w, r, "load action", err)
		return
	}
	now := h.Now().UTC()
	switch {
	case a.AuthorID != me.ID:
		problem.Write(w, r, http.StatusForbidden, "not_author", i18n.MsgNotAuthor)
		return
	case a.AppealDueAt == nil || a.OverturnedAt != nil:
		problem.Write(w, r, http.StatusConflict, "not_appealable", i18n.MsgNotAppealable)
		return
	case !now.Before(*a.AppealDueAt):
		problem.Write(w, r, http.StatusConflict, "outside_window", i18n.MsgOutsideWindow)
		return
	}

	id := uuid.Must(uuid.NewV7())
	due := now.Add(DecisionWindow)
	err = h.tx(r.Context(), func(q *moderationdb.Queries, tx pgx.Tx) error {
		row, err := q.InsertAppeal(r.Context(), moderationdb.InsertAppealParams{
			PublicID: id, ActionID: a.ID, AppellantID: me.ID, Statement: statement, DueAt: due,
		})
		if err != nil {
			return err
		}
		return outbox.Enqueue(r.Context(), tx, events.TopicAppealFiled, a.PostPublicID.String(),
			events.New("moderation", events.TopicAppealFiled, row.FiledAt, AppealFiledData{
				AppealID: id.String(), ActionID: a.PublicID.String(), PostID: a.PostPublicID.String(),
				AppellantID: me.ID, ModeratorID: a.ActorID, FiledAt: row.FiledAt, DueAt: due,
			}))
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		problem.Write(w, r, http.StatusConflict, "already_appealed", i18n.MsgAlreadyAppealed)
	case err != nil:
		h.internal(w, r, "file appeal", err)
	default:
		httpjson.Write(w, http.StatusCreated, AppealResponse{AppealID: id.String(), State: "open", DueAt: due})
	}
}

// DecisionRequest is the body of POST /v1/appeals/{appeal_id}/decision.
type DecisionRequest struct {
	Decision string `json:"decision"` // uphold | overturn
	Notes    string `json:"notes"`
}

// DecisionResponse is returned by POST /v1/appeals/{appeal_id}/decision.
type DecisionResponse struct {
	AppealID  string `json:"appeal_id"`
	State     string `json:"state"`
	PostState string `json:"post_state"`
}

// Decide serves POST /v1/appeals/{appeal_id}/decision (T-3.1.3.4–T-3.1.3.7):
// the appeals committee upholds or overturns. Overturning restores the post
// to its state before the action, records that restore in the post's
// history and flags the original action (and so its moderator).
func (h *Handlers) Decide(w http.ResponseWriter, r *http.Request) {
	me, roles, ok := h.Members.Caller(w, r)
	if !ok {
		return
	}
	if !roles.Has(membership.RoleAppealsMember) {
		problem.Write(w, r, http.StatusForbidden, "insufficient_authority", i18n.MsgInsufficientAuthority)
		return
	}
	var req DecisionRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	if req.Decision != "uphold" && req.Decision != "overturn" {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_decision", i18n.MsgInvalidDecision,
			problem.FieldError{Field: "decision", Code: "invalid"})
		return
	}
	notes := strings.TrimSpace(req.Notes)
	if utf8.RuneCountInString(notes) > maxNotesRunes {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgNotesTooLong,
			problem.FieldError{Field: "notes", Code: "too_long"})
		return
	}
	appealID, err := uuid.Parse(chi.URLParam(r, "appeal_id"))
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "appeal_not_found", i18n.MsgAppealNotFound)
		return
	}

	var resp DecisionResponse
	var restored *post.Post
	err = h.tx(r.Context(), func(q *moderationdb.Queries, tx pgx.Tx) error {
		ap, err := q.GetAppeal(r.Context(), appealID) // locks the appeal row
		if err != nil {
			return err
		}
		if ap.State != appealOpen {
			return errAppealDecided
		}
		if ap.ActorID == me.ID {
			return errOwnDecision // nobody reviews their own decision
		}
		state, postState := int16(appealUpheld), ap.PostState
		if req.Decision == "overturn" {
			state = appealOverturned
			if err := q.MarkActionOverturned(r.Context(), ap.ActionID); err != nil {
				return err
			}
			// Restore only if nothing has changed the post since the action.
			if ap.PostState == ap.NewState && ap.PostState != ap.PreviousState {
				if _, err := q.InsertModerationAction(r.Context(), moderationdb.InsertModerationActionParams{
					PublicID: uuid.Must(uuid.NewV7()), PostID: ap.PostID, PostPublicID: ap.PostPublicID, ActorID: me.ID,
					Action: "restore", ReasonCode: ap.ReasonCode,
					Notes:      pgtype.Text{String: "Appeal " + ap.PublicID.String() + " overturned", Valid: true},
					ScopeLevel: ap.Level, PreviousState: ap.PostState, NewState: ap.PreviousState,
				}); err != nil {
					return err
				}
				if err := q.SetPostState(r.Context(), moderationdb.SetPostStateParams{State: ap.PreviousState, PostID: ap.PostID}); err != nil {
					return err
				}
				postState = ap.PreviousState
				restored = &post.Post{PublicID: ap.PostPublicID, Level: ap.Level, WardID: ap.WardID,
					ConstituencyID: ap.ConstituencyID, CountyID: ap.CountyID}
			}
		}
		if err := q.DecideAppeal(r.Context(), moderationdb.DecideAppealParams{
			ID: ap.ID, State: state, DecidedBy: pgtype.Int8{Int64: me.ID, Valid: true},
			DecisionNotes: pgtype.Text{String: notes, Valid: notes != ""},
		}); err != nil {
			return err
		}
		resp = DecisionResponse{AppealID: ap.PublicID.String(), State: appealStates[state], PostState: stateNames[postState]}
		now := h.Now().UTC()
		return outbox.Enqueue(r.Context(), tx, events.TopicAppealResolved, ap.PostPublicID.String(),
			events.New("moderation", events.TopicAppealResolved, now, AppealResolvedData{
				AppealID: ap.PublicID.String(), ActionID: ap.ActionPublicID.String(), PostID: ap.PostPublicID.String(),
				Decision: req.Decision, AuthorID: ap.AuthorID, ModeratorID: ap.ActorID, DecidedBy: me.ID,
				PostState: postState, DecidedAt: now,
			}))
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		problem.Write(w, r, http.StatusNotFound, "appeal_not_found", i18n.MsgAppealNotFound)
	case errors.Is(err, errAppealDecided):
		problem.Write(w, r, http.StatusConflict, "appeal_decided", i18n.MsgAppealDecided)
	case errors.Is(err, errOwnDecision):
		problem.Write(w, r, http.StatusForbidden, "insufficient_authority", i18n.MsgInsufficientAuthority)
	case err != nil:
		h.internal(w, r, "decide appeal", err)
	default:
		if restored != nil {
			h.invalidate(r, *restored)
		}
		httpjson.Write(w, http.StatusOK, resp)
	}
}

var (
	errAppealDecided = errors.New("moderation: appeal already decided")
	errOwnDecision   = errors.New("moderation: reviewer took the original action")
)

// AppealJSON is an open appeal in the committee's list.
type AppealJSON struct {
	AppealID   string    `json:"appeal_id"`
	Statement  string    `json:"statement"`
	FiledAt    time.Time `json:"filed_at"`
	DueAt      time.Time `json:"due_at"`
	ActionID   string    `json:"action_id"`
	PostID     string    `json:"post_id"`
	Action     string    `json:"action"`
	ReasonCode string    `json:"reason_code"`
	Level      int16     `json:"level"`
	Content    *string   `json:"content"`
	Moderator  string    `json:"moderator"`
}

// Open serves GET /v1/appeals — open appeals, soonest due first, for the
// appeals committee.
func (h *Handlers) Open(w http.ResponseWriter, r *http.Request) {
	_, roles, ok := h.Members.Caller(w, r)
	if !ok {
		return
	}
	if !roles.Has(membership.RoleAppealsMember) {
		problem.Write(w, r, http.StatusForbidden, "insufficient_authority", i18n.MsgInsufficientAuthority)
		return
	}
	rows, err := moderationdb.New(h.Pool).ListOpenAppeals(r.Context())
	if err != nil {
		h.internal(w, r, "open appeals", err)
		return
	}
	items := make([]AppealJSON, 0, len(rows))
	for _, row := range rows {
		var content *string
		if row.Content.Valid {
			content = &row.Content.String
		}
		items = append(items, AppealJSON{
			AppealID: row.PublicID.String(), Statement: row.Statement, FiledAt: row.FiledAt, DueAt: row.DueAt,
			ActionID: row.ActionPublicID.String(), PostID: row.PostPublicID.String(), Action: row.Action,
			ReasonCode: row.ReasonCode, Level: row.ScopeLevel, Content: content, Moderator: row.ModeratorDisplayName,
		})
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"items": items})
}
