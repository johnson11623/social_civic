package post

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/johnson11623/social_civic/internal/media"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/internal/post/postdb"
)

// Feature 2.1.4 — the feed merges the top posts of the caller's four scopes
// (ward, constituency, county, national) by score.

// MaxFeedLimit is the largest page, and the size of each cached scope page.
const MaxFeedLimit = 50

// Scope is one feed audience: a level and the unit at that level (0 for national).
type Scope struct {
	Level int16
	ID    int32
}

// Name is the scope's segment in cache keys, e.g. "ward:551" or "national".
func (s Scope) Name() string {
	switch s.Level {
	case LevelWard:
		return "ward:" + strconv.Itoa(int(s.ID))
	case LevelConstituency:
		return "const:" + strconv.Itoa(int(s.ID))
	case LevelCounty:
		return "county:" + strconv.Itoa(int(s.ID))
	default:
		return "national"
	}
}

// ScopeAt is the audience of a post from origin (ward, constituency, county) at level.
func ScopeAt(level int16, ward, constituency, county int32) Scope {
	switch level {
	case LevelWard:
		return Scope{LevelWard, ward}
	case LevelConstituency:
		return Scope{LevelConstituency, constituency}
	case LevelCounty:
		return Scope{LevelCounty, county}
	default:
		return Scope{LevelNational, 0}
	}
}

// ScopeOf is the feed a post currently appears in.
func ScopeOf(p Post) Scope { return ScopeAt(p.Level, p.WardID, p.ConstituencyID, p.CountyID) }

// scopesFor lists the feeds a user reads, ward first (T-2.1.4.3: never
// another ward's, constituency's or county's).
func scopesFor(u ActiveUser) []Scope {
	return []Scope{
		{LevelWard, u.WardID}, {LevelConstituency, u.ConstituencyID},
		{LevelCounty, u.CountyID}, {LevelNational, 0},
	}
}

// ---- Cache (T-2.1.4.2, T-2.1.2.6) ------------------------------------------------

// FeedCache tracks a version per feed scope. Feed keys embed the version
// (LLD §7), so bumping it invalidates every cached page of that feed at once
// without races; stale keys expire by TTL.
type FeedCache interface {
	Bump(ctx context.Context, s Scope) error
}

// InvalidatePost bumps the feeds affected when a post changes level or
// state (T-2.1.4.5): the scope it left, when elevated, and the one it is in.
// Elevation and moderation call it after committing.
func InvalidatePost(ctx context.Context, c FeedCache, p Post, fromLevel int16) error {
	var errs []error
	if fromLevel != p.Level {
		errs = append(errs, c.Bump(ctx, ScopeAt(fromLevel, p.WardID, p.ConstituencyID, p.CountyID)))
	}
	errs = append(errs, c.Bump(ctx, ScopeOf(p)))
	return errors.Join(errs...)
}

// RedisFeedCache implements FeedCache and stores each scope's top page.
//
// F-06: pages are HMAC-signed together with their key, so a value written
// by anything without the key, or copied between keys, is rejected and the
// feed falls back to the database.
type RedisFeedCache struct {
	Client *redis.Client
	Key    []byte // at least 32 bytes
}

// VersionKey is the Redis key holding a scope's feed version.
func VersionKey(s Scope) string { return "feed:v:" + s.Name() }

func pageKey(s Scope, version int64) string {
	return "feed:" + s.Name() + ":v" + strconv.FormatInt(version, 10) + ":top50"
}

// pageTTL follows LLD §7: ward feeds churn most.
func pageTTL(s Scope) time.Duration {
	if s.Level == LevelWard {
		return 30 * time.Second
	}
	return 60 * time.Second
}

// Bump implements FeedCache.
func (c RedisFeedCache) Bump(ctx context.Context, s Scope) error {
	return c.Client.Incr(ctx, VersionKey(s)).Err()
}

func (c RedisFeedCache) mac(key string, body []byte) []byte {
	m := hmac.New(sha256.New, c.Key)
	m.Write([]byte(key))
	m.Write([]byte{0})
	m.Write(body)
	return m.Sum(nil)
}

// pageKeys resolves the current page key of each scope.
func (c RedisFeedCache) pageKeys(ctx context.Context, scopes []Scope) ([]string, error) {
	vkeys := make([]string, len(scopes))
	for i, s := range scopes {
		vkeys[i] = VersionKey(s)
	}
	vals, err := c.Client.MGet(ctx, vkeys...).Result()
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(scopes))
	for i, v := range vals {
		var n int64 // a scope nobody wrote to yet is at version 0
		if str, ok := v.(string); ok {
			n, _ = strconv.ParseInt(str, 10, 64)
		}
		keys[i] = pageKey(scopes[i], n)
	}
	return keys, nil
}

// load returns each key's verified page, or nil where missing or rejected.
func (c RedisFeedCache) load(ctx context.Context, logger *slog.Logger, keys []string) ([][]feedEntry, error) {
	vals, err := c.Client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	pages := make([][]feedEntry, len(keys))
	for i, v := range vals {
		raw, ok := v.(string)
		if !ok {
			continue
		}
		if len(raw) < sha256.Size || !hmac.Equal([]byte(raw[:sha256.Size]), c.mac(keys[i], []byte(raw[sha256.Size:]))) {
			logger.WarnContext(ctx, "cache_key_signature_mismatch", "key", keys[i])
			continue
		}
		page := []feedEntry{}
		if err := json.Unmarshal([]byte(raw[sha256.Size:]), &page); err != nil {
			logger.WarnContext(ctx, "feed cache page unreadable", "key", keys[i], "err", err)
			continue
		}
		pages[i] = page
	}
	return pages, nil
}

func (c RedisFeedCache) store(ctx context.Context, key string, s Scope, page []feedEntry) error {
	body, err := json.Marshal(page)
	if err != nil {
		return err
	}
	return c.Client.Set(ctx, key, append(c.mac(key, body), body...), pageTTL(s)).Err()
}

// ---- Store ------------------------------------------------------------------------

// feedEntry is a post as cached: its public JSON plus the internal id that
// orders ties and looks up the caller's likes. The caller-specific liked
// flag is never cached.
type feedEntry struct {
	ID   int64    `json:"i"`
	Post PostJSON `json:"p"`
}

// feedAfter is a keyset position: entries strictly below (score, id) follow.
type feedAfter struct {
	Score float32 `json:"s"`
	ID    int64   `json:"i"`
}

var feedStart = feedAfter{Score: math.MaxFloat32, ID: math.MaxInt64}

// ScopeFeed returns up to limit of a scope's organic posts after the cursor.
func (s *Store) ScopeFeed(ctx context.Context, sc Scope, after feedAfter, limit int) ([]feedEntry, error) {
	q := postdb.New(s.pool)
	var rows []postdb.FeedWardRow
	var err error
	switch sc.Level {
	case LevelWard:
		rows, err = q.FeedWard(ctx, postdb.FeedWardParams{ScopeID: sc.ID, AfterScore: after.Score, AfterID: after.ID, MaxRows: int32(limit)})
	case LevelConstituency:
		var rs []postdb.FeedConstituencyRow
		rs, err = q.FeedConstituency(ctx, postdb.FeedConstituencyParams{ScopeID: sc.ID, AfterScore: after.Score, AfterID: after.ID, MaxRows: int32(limit)})
		for _, r := range rs {
			rows = append(rows, postdb.FeedWardRow(r))
		}
	case LevelCounty:
		var rs []postdb.FeedCountyRow
		rs, err = q.FeedCounty(ctx, postdb.FeedCountyParams{ScopeID: sc.ID, AfterScore: after.Score, AfterID: after.ID, MaxRows: int32(limit)})
		for _, r := range rs {
			rows = append(rows, postdb.FeedWardRow(r))
		}
	default:
		var rs []postdb.FeedNationalRow
		rs, err = q.FeedNational(ctx, postdb.FeedNationalParams{AfterScore: after.Score, AfterID: after.ID, MaxRows: int32(limit)})
		for _, r := range rs {
			rows = append(rows, postdb.FeedWardRow(r))
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]feedEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, feedEntry{ID: r.ID, Post: postJSON(Post{
			PublicID: r.PublicID, ChannelID: r.ChannelPublicID, ChannelName: r.ChannelName,
			AuthorID: r.AuthorPublicID, AuthorName: r.AuthorDisplayName,
			AuthorAvatar: media.URL(s.MediaCDN, r.AuthorAvatar), Content: r.Content.String,
			Level: r.Level, WardID: r.WardID, Score: r.Score, State: StateActive, CreatedAt: r.CreatedAt,
			Likes: int(r.LikeCount), Replies: int(r.ReplyCount), Media: media.Embedded(s.MediaCDN, r.Media),
		})})
	}
	return out, nil
}

// LikedAmong returns which of ids the user likes.
func (s *Store) LikedAmong(ctx context.Context, userID int64, ids []int64) (map[int64]bool, error) {
	liked, err := postdb.New(s.pool).LikedAmong(ctx, postdb.LikedAmongParams{UserID: userID, PostIds: ids})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(liked))
	for _, id := range liked {
		out[id] = true
	}
	return out, nil
}

// ---- HTTP -------------------------------------------------------------------------

// FeedHandlers serves GET /v1/feed behind authn.Middleware.
type FeedHandlers struct {
	Store  *Store
	Cache  RedisFeedCache
	Logger *slog.Logger
}

// FeedResponse is the body of GET /v1/feed.
type FeedResponse struct {
	Items      []PostJSON `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
	HasMore    bool       `json:"has_more"`
}

var feedLevels = map[string]int16{"ward": LevelWard, "constituency": LevelConstituency, "county": LevelCounty, "national": LevelNational}

// Feed serves GET /v1/feed?level=all|ward|constituency|county|national&limit=&cursor=
// (T-2.1.4.1–T-2.1.4.4). Items are the caller's scopes merged by score,
// highest first; replies and sponsored posts are not ranked (T-2.1.4.6).
// X-Feed-Cache reports hit, miss or partial for the first page.
func (h *FeedHandlers) Feed(w http.ResponseWriter, r *http.Request) {
	user, ok := caller(w, r, h.Store, h.Logger)
	if !ok {
		return
	}
	scopes := scopesFor(user)
	if lv := r.URL.Query().Get("level"); lv != "" && lv != "all" {
		level, ok := feedLevels[lv]
		if !ok {
			problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed,
				problem.FieldError{Field: "level", Code: "invalid"})
			return
		}
		scopes = []Scope{scopes[level-1]}
	}
	limit := MaxFeedLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > MaxFeedLimit {
			problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed,
				problem.FieldError{Field: "limit", Code: "out_of_range"})
			return
		}
		limit = n
	}
	after := feedStart
	if v := r.URL.Query().Get("cursor"); v != "" {
		var ok bool
		if after, ok = h.decodeCursor(v); !ok {
			problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgValidationFailed,
				problem.FieldError{Field: "cursor", Code: "invalid"})
			return
		}
	}

	pages, status, err := h.scopePages(r.Context(), scopes, after, limit)
	if err != nil {
		internal(w, r, h.Logger, "feed", err)
		return
	}
	if status != "" {
		w.Header().Set("X-Feed-Cache", status)
	}
	var merged []feedEntry
	for _, p := range pages {
		for _, e := range p {
			if e.Post.Score < after.Score || (e.Post.Score == after.Score && e.ID < after.ID) {
				merged = append(merged, e)
			}
		}
	}
	slices.SortFunc(merged, func(a, b feedEntry) int {
		return cmp.Or(cmp.Compare(b.Post.Score, a.Post.Score), cmp.Compare(b.ID, a.ID))
	})
	resp := FeedResponse{Items: []PostJSON{}}
	if len(merged) > limit {
		merged, resp.HasMore = merged[:limit], true
		last := merged[limit-1]
		resp.NextCursor = h.encodeCursor(feedAfter{Score: last.Post.Score, ID: last.ID})
	}

	ids := make([]int64, len(merged))
	for i, e := range merged {
		ids[i] = e.ID
	}
	liked, err := h.Store.LikedAmong(r.Context(), user.ID, ids)
	if err != nil {
		internal(w, r, h.Logger, "feed liked", err)
		return
	}
	for _, e := range merged {
		l := liked[e.ID]
		e.Post.Liked = &l
		resp.Items = append(resp.Items, e.Post)
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// scopePages returns, per scope, enough entries after the cursor to fill a
// page and tell whether more follow. First pages come from the cache;
// Redis failures degrade to the database. status is hit, miss or partial
// for cached first pages, empty otherwise.
func (h *FeedHandlers) scopePages(ctx context.Context, scopes []Scope, after feedAfter, limit int) (pages [][]feedEntry, status string, err error) {
	pages = make([][]feedEntry, len(scopes))
	var keys []string
	if after == feedStart {
		if keys, err = h.Cache.pageKeys(ctx, scopes); err == nil {
			pages, err = h.Cache.load(ctx, h.Logger, keys)
		}
		if err != nil {
			h.Logger.WarnContext(ctx, "feed cache unavailable", "err", err)
			keys, pages = nil, make([][]feedEntry, len(scopes))
		}
	}
	hits := 0
	for i, s := range scopes {
		if pages[i] != nil {
			hits++
			continue
		}
		// A cached page holds MaxFeedLimit+1 so any first page knows has_more.
		n := limit + 1
		if keys != nil {
			n = MaxFeedLimit + 1
		}
		page, err := h.Store.ScopeFeed(ctx, s, after, n)
		if err != nil {
			return nil, "", err
		}
		pages[i] = page
		if keys != nil {
			if err := h.Cache.store(ctx, keys[i], s, page); err != nil {
				h.Logger.WarnContext(ctx, "feed cache write failed", "key", keys[i], "err", err)
			}
		}
	}
	switch {
	case keys == nil:
	case hits == len(scopes):
		status = "hit"
	case hits == 0:
		status = "miss"
	default:
		status = "partial"
	}
	return pages, status, nil
}

// Cursors are signed (API spec §1.5) so clients can't forge positions.
func (h *FeedHandlers) encodeCursor(a feedAfter) string {
	body, _ := json.Marshal(a)
	m := hmac.New(sha256.New, h.Cache.Key)
	m.Write([]byte("feed-cursor\x00"))
	m.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

func (h *FeedHandlers) decodeCursor(s string) (feedAfter, bool) {
	body64, sig64, ok := strings.Cut(s, ".")
	if !ok {
		return feedAfter{}, false
	}
	body, err1 := base64.RawURLEncoding.DecodeString(body64)
	sig, err2 := base64.RawURLEncoding.DecodeString(sig64)
	if err1 != nil || err2 != nil {
		return feedAfter{}, false
	}
	m := hmac.New(sha256.New, h.Cache.Key)
	m.Write([]byte("feed-cursor\x00"))
	m.Write(body)
	var a feedAfter
	if !hmac.Equal(sig, m.Sum(nil)[:16]) || json.Unmarshal(body, &a) != nil {
		return feedAfter{}, false
	}
	return a, true
}
