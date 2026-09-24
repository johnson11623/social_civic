package post

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/post/postdb"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Interaction errors.
var (
	ErrAlreadyLiked = errors.New("post: already liked")
	ErrLikeNotFound = errors.New("post: like not found")
)

// Interaction types in interaction.recorded (scoring consumes them).
const (
	InteractionLike   = "like"
	InteractionUnlike = "unlike"
	InteractionReply  = "reply"
)

// InteractionData is the payload of interaction.recorded (T-2.1.3.6).
type InteractionData struct {
	PostID    string    `json:"post_id"` // the thread's top-level post for replies
	TargetID  string    `json:"target_id,omitempty"`
	ReplyID   string    `json:"reply_id,omitempty"`
	UserID    int64     `json:"user_id"`
	Type      string    `json:"type"`
	NewActor  bool      `json:"new_actor"`
	CreatedAt time.Time `json:"created_at"`
}

func interactionEvent(d InteractionData) events.Event {
	return events.New("post", events.TopicInteraction, d.CreatedAt, d)
}

// HasLiked reports whether user likes post.
func (s *Store) HasLiked(ctx context.Context, postID, userID int64) (bool, error) {
	return postdb.New(s.pool).HasLiked(ctx, postdb.HasLikedParams{PostID: postID, UserID: userID})
}

// Like records one like per user per post, updates counters and enqueues
// interaction.recorded, atomically. Returns the new like count.
func (s *Store) Like(ctx context.Context, user ActiveUser, p Post, now time.Time) (int, error) {
	var likes int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := postdb.New(tx)
		n, err := q.InsertLike(ctx, postdb.InsertLikeParams{PostID: p.ID, UserID: user.ID})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrAlreadyLiked
		}
		newActor, err := q.InsertActor(ctx, postdb.InsertActorParams{PostID: p.ID, UserID: user.ID})
		if err != nil {
			return err
		}
		c, err := q.AddToCounters(ctx, postdb.AddToCountersParams{PostID: p.ID, Likes: 1, Actors: int32(newActor)})
		if err != nil {
			return err
		}
		likes = int(c.LikeCount)
		return outbox.Enqueue(ctx, tx, events.TopicInteraction, p.PublicID.String(), interactionEvent(InteractionData{
			PostID: threadRoot(p).String(), TargetID: p.PublicID.String(), UserID: user.ID,
			Type: InteractionLike, NewActor: newActor == 1, CreatedAt: now,
		}))
	})
	return likes, err
}

// Unlike removes the caller's like. The user stays counted as an actor.
func (s *Store) Unlike(ctx context.Context, user ActiveUser, p Post, now time.Time) (int, error) {
	var likes int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := postdb.New(tx)
		n, err := q.DeleteLike(ctx, postdb.DeleteLikeParams{PostID: p.ID, UserID: user.ID})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrLikeNotFound
		}
		c, err := q.AddToCounters(ctx, postdb.AddToCountersParams{PostID: p.ID, Likes: -1})
		if err != nil {
			return err
		}
		likes = int(c.LikeCount)
		return outbox.Enqueue(ctx, tx, events.TopicInteraction, p.PublicID.String(), interactionEvent(InteractionData{
			PostID: threadRoot(p).String(), TargetID: p.PublicID.String(), UserID: user.ID,
			Type: InteractionUnlike, CreatedAt: now,
		}))
	})
	return likes, err
}

// threadRoot is the public id of a post's top-level post (itself if top-level).
func threadRoot(p Post) uuid.UUID {
	if p.RootID != 0 {
		return p.RootPublic
	}
	return p.PublicID
}

// Reply stores a reply to parent. It inherits the thread's level, audience
// and channel, so whoever can see the thread sees the reply (T-2.1.3.5).
func (s *Store) Reply(ctx context.Context, user ActiveUser, parent Post, content string, now time.Time) (Post, error) {
	root := parent.ID
	rootPublic := parent.PublicID
	if parent.RootID != 0 {
		root, rootPublic = parent.RootID, parent.RootPublic
	}
	reply := Post{
		PublicID: uuid.Must(uuid.NewV7()), ChannelID: parent.ChannelID, Content: content,
		Level: parent.Level, WardID: parent.WardID, ConstituencyID: parent.ConstituencyID, CountyID: parent.CountyID,
		State: StateActive, RootID: root, ParentID: parent.ID, RootPublic: rootPublic, ParentPublic: parent.PublicID,
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := postdb.New(tx)
		row, err := q.InsertReply(ctx, postdb.InsertReplyParams{
			PublicID: reply.PublicID, ChannelID: parent.ChannelRowID, AuthorID: user.ID, Content: pgtypeText(content),
			Level: reply.Level, WardID: reply.WardID, ConstituencyID: reply.ConstituencyID, CountyID: reply.CountyID,
			RootID: pgtype.Int8{Int64: root, Valid: true}, ParentID: pgtype.Int8{Int64: parent.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		reply.ID, reply.CreatedAt = row.ID, row.CreatedAt
		// The thread's reply count is on the root; a nested reply also counts
		// on the reply it answers.
		if _, err := q.AddToCounters(ctx, postdb.AddToCountersParams{PostID: parent.ID, Replies: 1}); err != nil {
			return err
		}
		newActor, err := q.InsertActor(ctx, postdb.InsertActorParams{PostID: root, UserID: user.ID})
		if err != nil {
			return err
		}
		if root != parent.ID || newActor == 1 {
			rootReplies := int32(0)
			if root != parent.ID {
				rootReplies = 1
			}
			if _, err := q.AddToCounters(ctx, postdb.AddToCountersParams{PostID: root, Replies: rootReplies, Actors: int32(newActor)}); err != nil {
				return err
			}
		}
		return outbox.Enqueue(ctx, tx, events.TopicInteraction, rootPublic.String(), interactionEvent(InteractionData{
			PostID: rootPublic.String(), TargetID: parent.PublicID.String(), ReplyID: reply.PublicID.String(),
			UserID: user.ID, Type: InteractionReply, NewActor: newActor == 1, CreatedAt: reply.CreatedAt,
		}))
	})
	return reply, err
}

// ThreadPage is one page of a thread's replies.
type ThreadPage struct {
	Replies []Post
	Next    *threadCursor
}

type threadCursor struct {
	T  time.Time `json:"t"`
	ID int64     `json:"i"`
}

func (c threadCursor) encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (threadCursor, bool) {
	if s == "" {
		return threadCursor{T: time.Unix(0, 0).UTC()}, true
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	var c threadCursor
	if err != nil || json.Unmarshal(b, &c) != nil || c.ID < 0 {
		return threadCursor{}, false
	}
	return c, true
}

// Thread lists replies under a top-level post in conversation order.
func (s *Store) Thread(ctx context.Context, rootID int64, after threadCursor, limit int) (ThreadPage, error) {
	q := postdb.New(s.pool)
	rows, err := q.ListThread(ctx, postdb.ListThreadParams{
		RootID: pgtype.Int8{Int64: rootID, Valid: true}, AfterTime: after.T, AfterID: after.ID, MaxRows: int32(limit + 1),
	})
	if err != nil {
		return ThreadPage{}, err
	}
	page := ThreadPage{}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		page.Next = &threadCursor{T: last.CreatedAt, ID: last.ID}
	}
	parents := make([]int64, 0, len(rows))
	for _, r := range rows {
		parents = append(parents, r.ParentID.Int64)
	}
	ids, err := q.PublicIDsByIDs(ctx, parents)
	if err != nil {
		return ThreadPage{}, err
	}
	public := make(map[int64]uuid.UUID, len(ids))
	for _, r := range ids {
		public[r.ID] = r.PublicID
	}
	for _, r := range rows {
		page.Replies = append(page.Replies, Post{
			ID: r.ID, PublicID: r.PublicID, Content: r.Content.String, State: r.State, CreatedAt: r.CreatedAt,
			AuthorID: r.AuthorPublicID, AuthorName: r.AuthorDisplayName, RootID: rootID, ParentID: r.ParentID.Int64,
			ParentPublic: public[r.ParentID.Int64], Likes: int(r.LikeCount), Replies: int(r.ReplyCount),
		})
	}
	return page, nil
}

// ---- HTTP -------------------------------------------------------------------------

// visiblePost loads the path's post and checks the caller can see it.
func (h *PostHandlers) visiblePost(w http.ResponseWriter, r *http.Request, user ActiveUser) (Post, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "post_id"))
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgPostNotFound)
		return Post{}, false
	}
	p, err := h.Store.PostByPublicID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgPostNotFound)
		return Post{}, false
	}
	if err != nil {
		internal(w, r, h.Logger, "load post", err)
		return Post{}, false
	}
	if !CanView(user, p.Level, p.WardID, p.ConstituencyID, p.CountyID) {
		problem.Write(w, r, http.StatusForbidden, "out_of_scope", i18n.MsgOutOfScope)
		return Post{}, false
	}
	return p, true
}

// interactable additionally requires an active post: frozen (under
// moderation) and removed posts take no new likes or replies.
func (h *PostHandlers) interactable(w http.ResponseWriter, r *http.Request, user ActiveUser) (Post, bool) {
	p, ok := h.visiblePost(w, r, user)
	if ok && p.State != StateActive {
		problem.Write(w, r, http.StatusConflict, "post_not_active", i18n.MsgPostNotActive)
		return Post{}, false
	}
	return p, ok
}

// LikeResponse is returned by like and unlike.
type LikeResponse struct {
	PostID string `json:"post_id"`
	Likes  int    `json:"likes"`
	Liked  bool   `json:"liked"`
}

// Like serves POST /v1/posts/{post_id}/likes (T-2.1.3.2, T-2.1.3.3).
func (h *PostHandlers) Like(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	p, ok := h.interactable(w, r, user)
	if !ok {
		return
	}
	likes, err := h.Store.Like(r.Context(), user, p, time.Now().UTC())
	switch {
	case errors.Is(err, ErrAlreadyLiked):
		problem.Write(w, r, http.StatusConflict, "already_liked", i18n.MsgAlreadyLiked)
	case err != nil:
		internal(w, r, h.Logger, "like", err)
	default:
		h.bump(r, p)
		httpjson.Write(w, http.StatusCreated, LikeResponse{PostID: p.PublicID.String(), Likes: likes, Liked: true})
	}
}

// Unlike serves DELETE /v1/posts/{post_id}/likes (T-2.1.3.4).
func (h *PostHandlers) Unlike(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	p, ok := h.visiblePost(w, r, user)
	if !ok {
		return
	}
	likes, err := h.Store.Unlike(r.Context(), user, p, time.Now().UTC())
	switch {
	case errors.Is(err, ErrLikeNotFound):
		problem.Write(w, r, http.StatusNotFound, "like_not_found", i18n.MsgLikeNotFound)
	case err != nil:
		internal(w, r, h.Logger, "unlike", err)
	default:
		h.bump(r, p)
		httpjson.Write(w, http.StatusOK, LikeResponse{PostID: p.PublicID.String(), Likes: likes, Liked: false})
	}
}

// ReplyRequest is the body of POST /v1/posts/{post_id}/replies.
type ReplyRequest struct {
	Content string `json:"content"`
}

// Reply serves POST /v1/posts/{post_id}/replies (T-2.1.3.5).
func (h *PostHandlers) Reply(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	var req ReplyRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	content := strings.TrimSpace(req.Content)
	switch {
	case content == "":
		problem.Write(w, r, http.StatusUnprocessableEntity, "content_empty", i18n.MsgContentEmpty,
			problem.FieldError{Field: "content", Code: "required"})
		return
	case utf8.RuneCountInString(content) > MaxContentRunes:
		problem.Write(w, r, http.StatusUnprocessableEntity, "content_too_long", i18n.MsgContentTooLong,
			problem.FieldError{Field: "content", Code: "too_long"})
		return
	}
	parent, ok := h.interactable(w, r, user)
	if !ok {
		return
	}
	reply, err := h.Store.Reply(r.Context(), user, parent, content, time.Now().UTC())
	if err != nil {
		internal(w, r, h.Logger, "reply", err)
		return
	}
	h.bump(r, parent)
	reply.AuthorID, reply.AuthorName = user.PublicID, user.DisplayName
	httpjson.Write(w, http.StatusCreated, postJSON(reply))
}

// Replies serves GET /v1/posts/{post_id}/replies?cursor=&limit= — the whole
// thread under a top-level post, oldest first, with parent ids for nesting.
func (h *PostHandlers) Replies(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	p, ok := h.visiblePost(w, r, user)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed,
				problem.FieldError{Field: "limit", Code: "out_of_range"})
			return
		}
		limit = n
	}
	after, ok := decodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed,
			problem.FieldError{Field: "cursor", Code: "invalid"})
		return
	}
	root := p.ID
	if p.RootID != 0 {
		root = p.RootID
	}
	page, err := h.Store.Thread(r.Context(), root, after, limit)
	if err != nil {
		internal(w, r, h.Logger, "thread", err)
		return
	}
	ids := make([]int64, len(page.Replies))
	for i, reply := range page.Replies {
		ids[i] = reply.ID
	}
	liked, err := h.Store.LikedAmong(r.Context(), user.ID, ids)
	if err != nil {
		internal(w, r, h.Logger, "thread liked", err)
		return
	}
	items := make([]PostJSON, 0, len(page.Replies))
	for _, reply := range page.Replies {
		l := liked[reply.ID]
		reply.Liked = &l
		reply.ChannelID, reply.Level, reply.WardID, reply.RootPublic = p.ChannelID, p.Level, p.WardID, threadRoot(p)
		items = append(items, postJSON(reply))
	}
	resp := map[string]any{"post_id": threadRoot(p).String(), "items": items, "has_more": page.Next != nil}
	if page.Next != nil {
		resp["next_cursor"] = page.Next.encode()
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// bump invalidates the feed that shows the post (its counts changed);
// failures only delay freshness.
func (h *PostHandlers) bump(r *http.Request, p Post) {
	if h.Cache == nil {
		return
	}
	if err := h.Cache.Bump(r.Context(), ScopeOf(p)); err != nil {
		h.Logger.WarnContext(r.Context(), "feed cache bump failed", "scope", ScopeOf(p).Name(), "err", err)
	}
}
