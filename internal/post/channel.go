// Package post owns ward channels, posts and interactions (Channel & Post
// service, EPIC 2.1).
package post

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/post/postdb"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Channel categories (Web App Design v2.0 §3.3), stored as 1–5.
var categories = []string{"general", "services", "opportunities", "safety", "culture"}

func categoryCode(name string) (int16, bool) {
	for i, c := range categories {
		if c == name {
			return int16(i + 1), true
		}
	}
	return 0, false
}

func categoryName(code int16) string {
	if code >= 1 && int(code) <= len(categories) {
		return categories[code-1]
	}
	return categories[0]
}

var channelName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedNames guard against impersonation (US-04) and platform channels.
var reservedNames = map[string]bool{
	"general": true, "announcements": true, "admin": true, "administrator": true, "moderator": true,
	"moderators": true, "official": true, "officials": true, "platform": true, "system": true, "support": true,
	"help": true, "iebc": true, "government": true, "serikali": true, "county": true, "kaunti": true,
}

const maxDescriptionRunes = 140

// Errors.
var (
	ErrChannelNameTaken = errors.New("post: channel name taken")
	ErrNotFound         = errors.New("post: not found")
	ErrNoActiveUser     = errors.New("post: no active user")
)

// ActiveUser is the caller as the post service sees them.
type ActiveUser struct {
	ID             int64
	PublicID       uuid.UUID
	DisplayName    string
	WardID         int32
	ConstituencyID int32
	CountyID       int32
}

// Channel is a ward channel.
type Channel struct {
	PublicID    uuid.UUID
	WardID      int32
	Name        string
	Description string
	Category    int16
	ReadOnly    bool
	CreatedAt   time.Time
}

// ChannelCreatedData is the payload of channel.created.
type ChannelCreatedData struct {
	ChannelID string `json:"channel_id"`
	WardID    int32  `json:"ward_id"`
	CreatorID int64  `json:"creator_id"`
	Name      string `json:"name"`
	Category  string `json:"category"`
}

// Store persists channels.
type Store struct{ pool *pgxpool.Pool }

// NewStore wraps a connection pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ActiveUser resolves the caller; suspended and erased users have none.
func (s *Store) ActiveUser(ctx context.Context, publicID uuid.UUID) (ActiveUser, error) {
	u, err := postdb.New(s.pool).GetActiveUserByPublicID(ctx, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActiveUser{}, ErrNoActiveUser
	}
	return ActiveUser{ID: u.ID, PublicID: u.PublicID, DisplayName: u.DisplayName,
		WardID: u.WardID, ConstituencyID: u.ConstituencyID, CountyID: u.CountyID}, err
}

// CreateChannel inserts the channel and enqueues channel.created, atomically.
func (s *Store) CreateChannel(ctx context.Context, creator ActiveUser, c Channel, event func(Channel) events.Event) (Channel, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := postdb.New(tx).InsertChannel(ctx, postdb.InsertChannelParams{
			PublicID:    c.PublicID,
			WardID:      creator.WardID,
			CreatorID:   pgtype.Int8{Int64: creator.ID, Valid: true},
			Name:        c.Name,
			Description: pgtype.Text{String: c.Description, Valid: c.Description != ""},
			Category:    c.Category,
		})
		if err != nil {
			return err
		}
		c.WardID, c.CreatedAt = creator.WardID, row.CreatedAt
		return outbox.Enqueue(ctx, tx, events.TopicChannelCreated, c.PublicID.String(), event(c))
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "channels_name_unique_in_ward" {
		return Channel{}, ErrChannelNameTaken
	}
	return c, err
}

// WardChannels lists a ward's active channels, #general first.
func (s *Store) WardChannels(ctx context.Context, wardID int32) ([]Channel, error) {
	rows, err := postdb.New(s.pool).ListWardChannels(ctx, wardID)
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(rows))
	for _, r := range rows {
		out = append(out, Channel{PublicID: r.PublicID, WardID: r.WardID, Name: r.Name, Description: r.Description.String,
			Category: r.Category, ReadOnly: r.ReadOnly, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// ChannelByPublicID returns an active channel.
func (s *Store) ChannelByPublicID(ctx context.Context, id uuid.UUID) (Channel, error) {
	r, err := postdb.New(s.pool).GetChannelByPublicID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && r.State != 1) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	return Channel{PublicID: r.PublicID, WardID: r.WardID, Name: r.Name, Description: r.Description.String,
		Category: r.Category, ReadOnly: r.ReadOnly, CreatedAt: r.CreatedAt}, nil
}

// channelRowID maps a channel's public id to its row id.
func (s *Store) channelRowID(ctx context.Context, id uuid.UUID) (int64, error) {
	r, err := postdb.New(s.pool).GetChannelByPublicID(ctx, id)
	return r.ID, err
}

// WardMemberCount counts a ward's active members.
func (s *Store) WardMemberCount(ctx context.Context, wardID int32) (int, error) {
	n, err := postdb.New(s.pool).CountActiveWardMembers(ctx, wardID)
	return int(n), err
}

// EnsureGeneralChannels gives every ward its platform-owned #general channel.
// Idempotent; returns how many were created.
func EnsureGeneralChannels(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return postdb.New(pool).EnsureGeneralChannels(ctx)
}

// ---- HTTP -------------------------------------------------------------------------

// ChannelJSON is a channel in API responses.
type ChannelJSON struct {
	ChannelID   string    `json:"channel_id"`
	WardID      int32     `json:"ward_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Category    string    `json:"category"`
	ReadOnly    bool      `json:"read_only"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	MemberCount *int      `json:"member_count,omitempty"`
}

func toJSON(c Channel) ChannelJSON {
	return ChannelJSON{ChannelID: c.PublicID.String(), WardID: c.WardID, Name: c.Name, Description: c.Description,
		Category: categoryName(c.Category), ReadOnly: c.ReadOnly, State: "active", CreatedAt: c.CreatedAt}
}

// CreateChannelRequest is the body of POST /v1/channels.
type CreateChannelRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	// Optional; channels are always created in the caller's own ward, and a
	// different ward is refused (T-2.1.1.3).
	WardID *int32 `json:"ward_id"`
}

// ChannelHandlers serves /v1/channels. Routes must run behind authn.Middleware
// (and identity.RequireConsent for writes).
type ChannelHandlers struct {
	Store  *Store
	Logger *slog.Logger
}

// caller resolves the authenticated, active user or writes 401.
func caller(w http.ResponseWriter, r *http.Request, store *Store, logger *slog.Logger) (ActiveUser, bool) {
	p, ok := authn.FromContext(r.Context())
	id, err := uuid.Parse(p.Subject)
	if !ok || err != nil {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return ActiveUser{}, false
	}
	u, err := store.ActiveUser(r.Context(), id)
	if errors.Is(err, ErrNoActiveUser) {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return ActiveUser{}, false
	}
	if err != nil {
		internal(w, r, logger, "resolve user", err)
		return ActiveUser{}, false
	}
	return u, true
}

func internal(w http.ResponseWriter, r *http.Request, logger *slog.Logger, op string, err error) {
	logger.ErrorContext(r.Context(), "post service failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
}

// Create serves POST /v1/channels (T-2.1.1.2–T-2.1.1.6).
func (h *ChannelHandlers) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	var req CreateChannelRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	if req.WardID != nil && *req.WardID != user.WardID {
		problem.Write(w, r, http.StatusForbidden, "not_member", i18n.MsgNotMember)
		return
	}

	name := strings.TrimSpace(req.Name)
	switch {
	case !channelName.MatchString(name) || len(name) < 2 || len(name) > 40:
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgInvalidChannelName,
			problem.FieldError{Field: "name", Code: "invalid_format"})
		return
	case reservedNames[name]:
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgChannelNameReserved,
			problem.FieldError{Field: "name", Code: "reserved"})
		return
	}
	description := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(description) > maxDescriptionRunes {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgDescriptionTooLong,
			problem.FieldError{Field: "description", Code: "too_long"})
		return
	}
	category := int16(1)
	if req.Category != "" {
		c, ok := categoryCode(req.Category)
		if !ok {
			problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgInvalidCategory,
				problem.FieldError{Field: "category", Code: "invalid"})
			return
		}
		category = c
	}

	channel, err := h.Store.CreateChannel(r.Context(), user,
		Channel{PublicID: uuid.Must(uuid.NewV7()), Name: name, Description: description, Category: category},
		func(c Channel) events.Event {
			return events.New("post", events.TopicChannelCreated, c.CreatedAt, ChannelCreatedData{
				ChannelID: c.PublicID.String(), WardID: c.WardID, CreatorID: user.ID, Name: c.Name,
				Category: categoryName(c.Category),
			})
		})
	switch {
	case errors.Is(err, ErrChannelNameTaken):
		problem.Write(w, r, http.StatusConflict, "name_taken", i18n.MsgChannelNameTaken)
	case err != nil:
		internal(w, r, h.Logger, "create channel", err)
	default:
		httpjson.Write(w, http.StatusCreated, toJSON(channel))
	}
}

// List serves GET /v1/channels: the caller's ward channels, #general first.
func (h *ChannelHandlers) List(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	channels, err := h.Store.WardChannels(r.Context(), user.WardID)
	if err != nil {
		internal(w, r, h.Logger, "list channels", err)
		return
	}
	items := make([]ChannelJSON, 0, len(channels))
	for _, c := range channels {
		items = append(items, toJSON(c))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"ward_id": user.WardID, "items": items})
}

// Get serves GET /v1/channels/{channel_id}; channels are visible to their ward only.
func (h *ChannelHandlers) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "channel_id"))
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgChannelNotFound)
		return
	}
	channel, err := h.Store.ChannelByPublicID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgChannelNotFound)
		return
	}
	if err != nil {
		internal(w, r, h.Logger, "get channel", err)
		return
	}
	if channel.WardID != user.WardID {
		problem.Write(w, r, http.StatusForbidden, "not_member", i18n.MsgNotMember)
		return
	}
	members, err := h.Store.WardMemberCount(r.Context(), channel.WardID)
	if err != nil {
		internal(w, r, h.Logger, "count members", err)
		return
	}
	out := toJSON(channel)
	out.MemberCount = &members
	httpjson.Write(w, http.StatusOK, out)
}

// KeyByUser keys rate limits by the authenticated user (run after authn).
func KeyByUser(r *http.Request) string {
	p, _ := authn.FromContext(r.Context())
	return "u:" + p.Subject
}

func pgtypeText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
