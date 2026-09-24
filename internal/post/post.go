package post

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/post/postdb"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// MaxContentRunes is the post length limit (composer counter, T-W1.4.2.3).
const MaxContentRunes = 500

// Post levels and states (posts.level, posts.state).
const (
	LevelWard         = 1
	LevelConstituency = 2
	LevelCounty       = 3
	LevelNational     = 4

	StateActive     = 1
	StateFrozen     = 2
	StateTombstoned = 3
	StatePurged     = 4
)

// Post is a stored post.
type Post struct {
	ID             int64
	PublicID       uuid.UUID
	ChannelID      uuid.UUID
	ChannelName    string
	AuthorID       uuid.UUID
	AuthorName     string
	Content        string
	Level          int16
	WardID         int32
	ConstituencyID int32
	CountyID       int32
	Score          float32
	State          int16
	CreatedAt      time.Time

	ChannelRowID int64
	RootID       int64 // 0 for top-level posts
	ParentID     int64
	ParentPublic uuid.UUID
	RootPublic   uuid.UUID
	Likes        int
	Replies      int
	Liked        *bool // for the caller, when known
	Sponsored    bool
	LabelEN      string
	LabelSW      string

	AuthorRowID int64 // internal: authorship checks (appeals), events
	Moderation  *Moderation
}

// Moderation is the decision behind a frozen or removed post.
type Moderation struct {
	ActionID    string     `json:"action_id"`
	Action      string     `json:"action"`
	ReasonCode  string     `json:"reason_code"`
	AppealDueAt *time.Time `json:"appeal_due_at,omitempty"`
}

// PostCreatedData is the payload of post.created. Content is left out: events
// feed scoring, notifications and audit, none of which need the text.
type PostCreatedData struct {
	PostID         string    `json:"post_id"`
	ChannelID      string    `json:"channel_id"`
	AuthorID       int64     `json:"author_id"`
	WardID         int32     `json:"ward_id"`
	ConstituencyID int32     `json:"constituency_id"`
	CountyID       int32     `json:"county_id"`
	Level          int16     `json:"level"`
	CreatedAt      time.Time `json:"created_at"`
}

// CanView reports whether a user may see a post at its current level: its
// ward (1), constituency (2), county (3) or the whole country (4).
func CanView(u ActiveUser, level int16, ward, constituency, county int32) bool {
	switch level {
	case LevelWard:
		return u.WardID == ward
	case LevelConstituency:
		return u.ConstituencyID == constituency
	case LevelCounty:
		return u.CountyID == county
	case LevelNational:
		return true
	}
	return false
}

// CreatePost inserts a ward-level post and enqueues post.created, atomically.
func (s *Store) CreatePost(ctx context.Context, author ActiveUser, channelID int64, p Post, event func(Post) events.Event) (Post, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := postdb.New(tx).InsertPost(ctx, postdb.InsertPostParams{
			PublicID:       p.PublicID,
			ChannelID:      channelID,
			AuthorID:       author.ID,
			Content:        pgtypeText(p.Content),
			WardID:         author.WardID,
			ConstituencyID: author.ConstituencyID,
			CountyID:       author.CountyID,
		})
		if err != nil {
			return err
		}
		p.ID, p.CreatedAt, p.Level, p.State = row.ID, row.CreatedAt, LevelWard, StateActive
		p.WardID, p.ConstituencyID, p.CountyID = author.WardID, author.ConstituencyID, author.CountyID
		return outbox.Enqueue(ctx, tx, events.TopicPostCreated, p.PublicID.String(), event(p))
	})
	return p, err
}

// PostByPublicID returns a post that has not been purged.
func (s *Store) PostByPublicID(ctx context.Context, id uuid.UUID) (Post, error) {
	r, err := postdb.New(s.pool).GetPostByPublicID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && r.State == StatePurged) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, err
	}
	p := Post{
		ID: r.ID, PublicID: r.PublicID, ChannelID: r.ChannelPublicID, ChannelName: r.ChannelName,
		AuthorID: r.AuthorPublicID, AuthorName: r.AuthorDisplayName, Content: r.Content.String,
		Level: r.Level, WardID: r.WardID, ConstituencyID: r.ConstituencyID, CountyID: r.CountyID,
		Score: r.Score, State: r.State, CreatedAt: r.CreatedAt,
		ChannelRowID: r.ChannelID, RootID: r.RootID.Int64, ParentID: r.ParentID.Int64,
		Likes: int(r.LikeCount), Replies: int(r.ReplyCount),
		Sponsored: r.Sponsored, LabelEN: r.LabelTextEn.String, LabelSW: r.LabelTextSw.String,
		AuthorRowID: r.AuthorID,
	}
	if r.ModerationActionID != "" {
		p.Moderation = &Moderation{ActionID: r.ModerationActionID, Action: r.ModerationAction,
			ReasonCode: r.ModerationReason, AppealDueAt: r.ModerationAppealDueAt}
	}
	q := postdb.New(s.pool)
	if p.RootID != 0 {
		if p.RootPublic, err = q.GetPostPublicIDByID(ctx, p.RootID); err != nil {
			return Post{}, err
		}
		if p.ParentPublic, err = q.GetPostPublicIDByID(ctx, p.ParentID); err != nil {
			return Post{}, err
		}
	}
	return p, nil
}

// ---- HTTP -------------------------------------------------------------------------

// PostJSON is a post in API responses.
type PostJSON struct {
	PostID    string     `json:"post_id"`
	ChannelID string     `json:"channel_id"`
	Channel   string     `json:"channel,omitempty"`
	Level     int16      `json:"level"`
	WardID    int32      `json:"ward_id"`
	Content   *string    `json:"content"` // null when removed
	Score     float32    `json:"score"`
	State     string     `json:"state"`
	Author    *AuthorRef `json:"author,omitempty"`
	Counts    Counts     `json:"counts"`
	Liked     *bool      `json:"liked,omitempty"`
	Sponsored bool       `json:"sponsored"`
	Label     *Label     `json:"sponsored_label,omitempty"` // both languages, always shown together
	// Frozen or removed: the decision, its harm and the appeal deadline.
	Moderation *Moderation `json:"moderation,omitempty"`
	RootID     string      `json:"root_id,omitempty"`   // replies: the thread's top-level post
	ParentID   string      `json:"parent_id,omitempty"` // replies: the post or reply answered
	CreatedAt  time.Time   `json:"created_at"`
}

// Label is a sponsored post's immutable bilingual disclosure (F-07).
type Label struct {
	EN string `json:"en"`
	SW string `json:"sw"`
}

// AuthorRef identifies a post's author publicly.
type AuthorRef struct {
	PublicID    string `json:"public_id"`
	DisplayName string `json:"display_name"`
}

// Counts are a post's interaction totals (filled by Feature 2.1.3).
type Counts struct {
	Likes   int `json:"likes"`
	Replies int `json:"replies"`
}

var stateNames = map[int16]string{StateActive: "active", StateFrozen: "frozen", StateTombstoned: "tombstoned"}

func postJSON(p Post) PostJSON {
	out := PostJSON{
		PostID: p.PublicID.String(), ChannelID: p.ChannelID.String(), Channel: p.ChannelName, Level: p.Level,
		WardID: p.WardID, Score: p.Score, State: stateNames[p.State], CreatedAt: p.CreatedAt,
		Counts: Counts{Likes: p.Likes, Replies: p.Replies}, Liked: p.Liked,
	}
	if p.RootID != 0 {
		out.RootID, out.ParentID = p.RootPublic.String(), p.ParentPublic.String()
	}
	if p.Sponsored {
		out.Sponsored, out.Label = true, &Label{EN: p.LabelEN, SW: p.LabelSW}
	}
	out.Moderation = p.Moderation
	if p.State == StateActive || p.State == StateFrozen {
		content := p.Content
		out.Content = &content
	}
	if p.AuthorName != "" {
		out.Author = &AuthorRef{PublicID: p.AuthorID.String(), DisplayName: p.AuthorName}
	}
	return out
}

// CreatePostRequest is the body of POST /v1/channels/{channel_id}/posts.
type CreatePostRequest struct {
	Content  string  `json:"content"`
	MediaURL *string `json:"media_url"`
}

// PostHandlers serves post endpoints. Routes must run behind authn.Middleware
// (and identity.RequireConsent for writes).
type PostHandlers struct {
	Store  *Store
	Cache  FeedCache
	Logger *slog.Logger
}

// Create serves POST /v1/channels/{channel_id}/posts (T-2.1.2.2–T-2.1.2.7).
func (h *PostHandlers) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	channelID, err := uuid.Parse(chi.URLParam(r, "channel_id"))
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgChannelNotFound)
		return
	}
	var req CreatePostRequest
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
	case req.MediaURL != nil && *req.MediaURL != "":
		// Media is entitlement-gated and uploads aren't built yet (T-W1.4.2.4).
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgMediaNotSupported,
			problem.FieldError{Field: "media_url", Code: "not_supported"})
		return
	}

	channel, err := h.Store.ChannelByPublicID(r.Context(), channelID)
	if errors.Is(err, ErrNotFound) {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgChannelNotFound)
		return
	}
	if err != nil {
		internal(w, r, h.Logger, "load channel", err)
		return
	}
	if channel.WardID != user.WardID {
		problem.Write(w, r, http.StatusForbidden, "not_member", i18n.MsgNotMember)
		return
	}
	if !channel.CanPost(user) {
		// Announcement channel: only its creator posts until moderator roles (EPIC 1.2).
		problem.Write(w, r, http.StatusForbidden, "read_only_channel", i18n.MsgReadOnlyChannel)
		return
	}
	channelRowID, err := h.Store.channelRowID(r.Context(), channelID)
	if err != nil {
		internal(w, r, h.Logger, "channel id", err)
		return
	}

	created, err := h.Store.CreatePost(r.Context(), user, channelRowID,
		Post{PublicID: uuid.Must(uuid.NewV7()), ChannelID: channel.PublicID, ChannelName: channel.Name, Content: content},
		func(p Post) events.Event {
			return events.New("post", events.TopicPostCreated, p.CreatedAt, PostCreatedData{
				PostID: p.PublicID.String(), ChannelID: p.ChannelID.String(), AuthorID: user.ID, WardID: p.WardID,
				ConstituencyID: p.ConstituencyID, CountyID: p.CountyID, Level: p.Level, CreatedAt: p.CreatedAt,
			})
		})
	if err != nil {
		internal(w, r, h.Logger, "create post", err)
		return
	}
	// The post is committed; a cache miss only delays it (feed TTL ≤ 30 s).
	if h.Cache != nil {
		if err := h.Cache.Bump(r.Context(), ScopeOf(created)); err != nil {
			h.Logger.WarnContext(r.Context(), "feed cache bump failed", "ward", created.WardID, "err", err)
		}
	}
	created.AuthorID, created.AuthorName = user.PublicID, user.DisplayName
	liked := false
	created.Liked = &liked
	httpjson.Write(w, http.StatusCreated, postJSON(created))
}

// Get serves GET /v1/posts/{post_id}: visible to the post's current audience.
func (h *PostHandlers) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "post_id"))
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgPostNotFound)
		return
	}
	p, err := h.Store.PostByPublicID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgPostNotFound)
		return
	}
	if err != nil {
		internal(w, r, h.Logger, "get post", err)
		return
	}
	if !CanView(user, p.Level, p.WardID, p.ConstituencyID, p.CountyID) {
		problem.Write(w, r, http.StatusForbidden, "out_of_scope", i18n.MsgOutOfScope)
		return
	}
	liked, err := h.Store.HasLiked(r.Context(), p.ID, user.ID)
	if err != nil {
		internal(w, r, h.Logger, "liked", err)
		return
	}
	p.Liked = &liked
	httpjson.Write(w, http.StatusOK, postJSON(p))
}

// RunPartitionMaintenance keeps monthly post partitions created 3 months
// ahead (LLD §13), checking at start and then daily. Rows never fail to
// insert without it (posts_default catches them), but default-partition
// rows make later partition creation slow, so the worker runs this.
func RunPartitionMaintenance(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, logger *slog.Logger) error {
	for {
		var created int
		if err := pool.QueryRow(ctx, "SELECT ensure_post_partitions(3)").Scan(&created); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "post partition maintenance failed", "err", err)
		} else if created > 0 {
			logger.InfoContext(ctx, "post partitions created", "count", created)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(24 * time.Hour):
		}
	}
}
