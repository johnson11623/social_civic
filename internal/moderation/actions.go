package moderation

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// Feature 3.1.2 — harm-based moderation actions.

// AppealWindow is how long an author has to appeal (T-3.1.2.6).
const AppealWindow = 14 * 24 * time.Hour

// transitions maps each action to the post states it applies to and the
// state it leaves. Hide and delete both remove the post (tombstone) and keep
// its content until the appeal window closes, so an appeal can restore it;
// delete also records the removal for purging. Freeze keeps a post visible
// but closed to interaction while it is reviewed. Restore undoes either.
var transitions = map[string]struct {
	from []int16
	to   int16
}{
	"hide":    {[]int16{post.StateActive, post.StateFrozen}, post.StateTombstoned},
	"delete":  {[]int16{post.StateActive, post.StateFrozen}, post.StateTombstoned},
	"freeze":  {[]int16{post.StateActive}, post.StateFrozen},
	"restore": {[]int16{post.StateFrozen, post.StateTombstoned}, post.StateActive},
}

var stateNames = map[int16]string{post.StateActive: "active", post.StateFrozen: "frozen", post.StateTombstoned: "tombstoned"}

// ModerationActionData is the payload of moderation.action_recorded
// (T-3.1.2.4). Notification tells the author (T-3.1.2.5) from it, in
// their language; audit keeps it (T-3.1.2.7).
type ModerationActionData struct {
	ActionID      string     `json:"action_id"`
	PostID        string     `json:"post_id"`
	AuthorID      int64      `json:"author_id"`
	ActorID       int64      `json:"actor_id"`
	Action        string     `json:"action"`
	ReasonCode    string     `json:"reason_code"`
	ScopeLevel    int16      `json:"scope_level"`
	PreviousState int16      `json:"previous_state"`
	NewState      int16      `json:"new_state"`
	AppealDueAt   *time.Time `json:"appeal_due_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ActionRequest is the body of POST /v1/moderation/actions.
type ActionRequest struct {
	PostID     string `json:"post_id"`
	Action     string `json:"action"`
	ReasonCode string `json:"reason_code"`
	Notes      string `json:"notes"`
}

// ActionResponse is returned by POST /v1/moderation/actions.
type ActionResponse struct {
	ActionID          string     `json:"action_id"`
	PostState         string     `json:"post_state"`
	AppealWindowHours int        `json:"appeal_window_hours,omitempty"`
	AppealDueAt       *time.Time `json:"appeal_due_at,omitempty"`
}

// Act serves POST /v1/moderation/actions (T-3.1.2.2–T-3.1.2.8).
//
// F-08 asks for MFA on moderator actions; TOTP enrolment isn't built yet.
func (h *Handlers) Act(w http.ResponseWriter, r *http.Request) {
	me, roles, ok := h.Members.Caller(w, r)
	if !ok {
		return
	}
	var req ActionRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	move, ok := transitions[req.Action]
	if !ok {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_action", i18n.MsgInvalidAction,
			problem.FieldError{Field: "action", Code: "invalid"})
		return
	}
	if !ValidReason(req.ReasonCode) {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_reason", i18n.MsgInvalidReason,
			problem.FieldError{Field: "reason_code", Code: "invalid"})
		return
	}
	notes := strings.TrimSpace(req.Notes)
	if utf8.RuneCountInString(notes) > maxNotesRunes {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgNotesTooLong,
			problem.FieldError{Field: "notes", Code: "too_long"})
		return
	}
	p, ok := h.loadPost(w, r, req.PostID)
	if !ok {
		return
	}
	// T-3.1.2.3 — tiered authority at the post's current level.
	if !roles.CanModerate(p.Level, p.WardID, p.ConstituencyID, p.CountyID) {
		h.Logger.WarnContext(r.Context(), "moderation.denied", "actor", me.ID, "post", p.PublicID, "level", p.Level)
		problem.Write(w, r, http.StatusForbidden, "insufficient_authority", i18n.MsgInsufficientAuthority)
		return
	}
	allowed := false
	for _, s := range move.from {
		allowed = allowed || s == p.State
	}
	if !allowed {
		problem.Write(w, r, http.StatusConflict, "already_moderated", i18n.MsgAlreadyModerated)
		return
	}

	now := h.Now().UTC()
	var due *time.Time
	if req.Action != "restore" {
		d := now.Add(AppealWindow)
		due = &d
	}
	id := uuid.Must(uuid.NewV7())
	reportState := int16(reportActioned)
	if req.Action == "restore" {
		reportState = reportDismissed
	}
	err := h.tx(r.Context(), func(q *moderationdb.Queries, tx pgx.Tx) error {
		row, err := q.InsertModerationAction(r.Context(), moderationdb.InsertModerationActionParams{
			PublicID: id, PostID: p.ID, PostPublicID: p.PublicID, ActorID: me.ID, Action: req.Action,
			ReasonCode: req.ReasonCode, Notes: pgtype.Text{String: notes, Valid: notes != ""}, ScopeLevel: p.Level,
			PreviousState: p.State, NewState: move.to, AppealDueAt: due,
		})
		if err != nil {
			return err
		}
		if err := q.SetPostState(r.Context(), moderationdb.SetPostStateParams{State: move.to, PostID: p.ID}); err != nil {
			return err
		}
		if _, err := q.ResolveOpenReports(r.Context(), moderationdb.ResolveOpenReportsParams{State: reportState, PostID: p.ID}); err != nil {
			return err
		}
		return outbox.Enqueue(r.Context(), tx, events.TopicModerationAction, p.PublicID.String(),
			events.New("moderation", events.TopicModerationAction, row.CreatedAt, ModerationActionData{
				ActionID: id.String(), PostID: p.PublicID.String(), AuthorID: p.AuthorRowID, ActorID: me.ID,
				Action: req.Action, ReasonCode: req.ReasonCode, ScopeLevel: p.Level, PreviousState: p.State,
				NewState: move.to, AppealDueAt: due, CreatedAt: row.CreatedAt,
			}))
	})
	if err != nil {
		h.internal(w, r, "act", err)
		return
	}
	h.invalidate(r, p)
	resp := ActionResponse{ActionID: id.String(), PostState: stateNames[move.to], AppealDueAt: due}
	if due != nil {
		resp.AppealWindowHours = int(AppealWindow.Hours())
	}
	httpjson.Write(w, http.StatusCreated, resp)
}

// invalidate drops the feeds that showed the post (T-3.1.2.8); a failure
// only delays it by the feed TTL.
func (h *Handlers) invalidate(r *http.Request, p post.Post) {
	if h.Cache == nil {
		return
	}
	if err := post.InvalidatePost(r.Context(), h.Cache, p, p.Level); err != nil {
		h.Logger.WarnContext(r.Context(), "feed cache bump failed", "post", p.PublicID, "err", err)
	}
}

// ---- Queue ----------------------------------------------------------------------------

// QueueItem is a reported post in a moderator's queue.
type QueueItem struct {
	PostID          string         `json:"post_id"`
	Level           int16          `json:"level"`
	WardID          int32          `json:"ward_id"`
	Content         *string        `json:"content"`
	State           string         `json:"state"`
	Author          post.AuthorRef `json:"author"`
	ReportCount     int            `json:"report_count"`
	Reasons         []string       `json:"reasons"`
	FirstReportedAt time.Time      `json:"first_reported_at"`
	PostedAt        time.Time      `json:"posted_at"`
}

var queueLevels = map[string]int16{"all": 0, "ward": 1, "constituency": 2, "county": 3, "national": 4}

// Queue serves GET /v1/moderation/queue?level=&filter=open|triage|all&sort=age|reports:
// reported posts within the caller's authority. "open" is reported posts
// still live; "triage" is frozen posts awaiting a decision.
func (h *Handlers) Queue(w http.ResponseWriter, r *http.Request) {
	_, roles, ok := h.Members.Caller(w, r)
	if !ok {
		return
	}
	scope := authorityOf(roles)
	if !scope.any {
		problem.Write(w, r, http.StatusForbidden, "insufficient_authority", i18n.MsgInsufficientAuthority)
		return
	}
	q := r.URL.Query()
	level, ok := queueLevels[cmpOr(q.Get("level"), "all")]
	filter := map[string][]int16{
		"open": {post.StateActive}, "triage": {post.StateFrozen}, "all": {post.StateActive, post.StateFrozen},
	}[cmpOr(q.Get("filter"), "open")]
	sort := cmpOr(q.Get("sort"), "age")
	if !ok || filter == nil || (sort != "age" && sort != "reports") {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed)
		return
	}
	rows, err := moderationdb.New(h.Pool).ModerationQueue(r.Context(), moderationdb.ModerationQueueParams{
		PostStates: filter, Wards: scope.wards, Constituencies: scope.constituencies, Counties: scope.counties,
		National: scope.national, Level: level, ByReports: sort == "reports",
	})
	if err != nil {
		h.internal(w, r, "queue", err)
		return
	}
	items := make([]QueueItem, 0, len(rows))
	for _, row := range rows {
		var content *string
		if row.Content.Valid {
			content = &row.Content.String
		}
		items = append(items, QueueItem{
			PostID: row.PublicID.String(), Level: row.Level, WardID: row.WardID, Content: content,
			State: stateNames[row.State], ReportCount: int(row.ReportCount), Reasons: row.Reasons,
			Author:          post.AuthorRef{PublicID: row.AuthorPublicID.String(), DisplayName: row.AuthorDisplayName},
			FirstReportedAt: row.FirstReportedAt, PostedAt: row.CreatedAt,
		})
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"items": items})
}

type authority struct {
	wards, constituencies, counties []int32
	national, any                   bool
}

// authorityOf flattens moderator roles into the units they govern.
func authorityOf(roles membership.Roles) authority {
	a := authority{wards: []int32{}, constituencies: []int32{}, counties: []int32{}}
	for _, r := range roles {
		switch membership.ModeratorLevels[r.Role] {
		case 1:
			a.wards, a.any = append(a.wards, r.UnitCode), true
		case 2:
			a.constituencies, a.any = append(a.constituencies, r.UnitCode), true
		case 3:
			a.counties, a.any = append(a.counties, r.UnitCode), true
		case 4:
			a.national, a.any = true, true
		}
	}
	return a
}

func cmpOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// ---- History --------------------------------------------------------------------------

// ActionJSON is a moderation action in a post's history.
type ActionJSON struct {
	ActionID    string          `json:"action_id"`
	Action      string          `json:"action"`
	ReasonCode  string          `json:"reason_code"`
	Level       int16           `json:"level"`
	PostState   string          `json:"post_state"`
	AppealDueAt *time.Time      `json:"appeal_due_at,omitempty"`
	Overturned  bool            `json:"overturned"`
	CreatedAt   time.Time       `json:"created_at"`
	Notes       string          `json:"notes,omitempty"`     // moderators only
	Moderator   *post.AuthorRef `json:"moderator,omitempty"` // moderators only
}

// History serves GET /v1/posts/{post_id}/moderation: what was done to a
// post and why. Moderators with authority see notes and who acted; the
// author sees the decisions and appeal deadlines.
func (h *Handlers) History(w http.ResponseWriter, r *http.Request) {
	me, roles, ok := h.Members.Caller(w, r)
	if !ok {
		return
	}
	p, ok := h.loadPost(w, r, chi.URLParam(r, "post_id"))
	if !ok {
		return
	}
	moderator := roles.CanModerate(p.Level, p.WardID, p.ConstituencyID, p.CountyID)
	if !moderator && p.AuthorRowID != me.ID {
		problem.Write(w, r, http.StatusForbidden, "insufficient_authority", i18n.MsgInsufficientAuthority)
		return
	}
	rows, err := moderationdb.New(h.Pool).ListPostActions(r.Context(), p.ID)
	if err != nil {
		h.internal(w, r, "history", err)
		return
	}
	items := make([]ActionJSON, 0, len(rows))
	for _, row := range rows {
		a := ActionJSON{
			ActionID: row.PublicID.String(), Action: row.Action, ReasonCode: row.ReasonCode, Level: row.ScopeLevel,
			PostState: stateNames[row.NewState], AppealDueAt: row.AppealDueAt, Overturned: row.OverturnedAt != nil,
			CreatedAt: row.CreatedAt,
		}
		if moderator {
			a.Notes = row.Notes.String
			a.Moderator = &post.AuthorRef{PublicID: row.ActorPublicID.String(), DisplayName: row.ActorDisplayName}
		}
		items = append(items, a)
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"post_id": p.PublicID.String(), "items": items})
}
