package post

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/platform/authn"
)

// Feature 2.1.4 — the feed against a real PostgreSQL and Redis
// (TEST_DATABASE_URL and TEST_REDIS_URL, as `make test-integration` sets).

// Wards around Kiamwangi (551, Gatundu South 111, Kiambu 22).
const (
	kiganjo       = 552      // same constituency
	murera, juja  = 559, 113 // same county
	kitisuru      = 1366     // Westlands 274, Nairobi City 47
	westlands     = 274
	nairobiCounty = 47
)

type feedEnv struct {
	*env
	cache RedisFeedCache
}

func newFeedEnv(t *testing.T) *feedEnv {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set; run `make test-integration`")
	}
	e := newEnv(t)
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	cache := RedisFeedCache{Client: rdb, Key: []byte("test-feed-signing-key-0123456789abcdef")}
	h := &FeedHandlers{Store: NewStore(e.pool), Cache: cache, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r := chi.NewRouter()
	r.With(authn.Middleware(e.tokens, identity.Unauthenticated)).Get("/v1/feed", h.Feed)
	r.Mount("/", e.router)
	e.router = r
	return &feedEnv{env: e, cache: cache}
}

var channelSeq int

// seedPost writes a top-level post straight to the database at any level
// and score, with its origin ward's channel and an author from that ward.
func (e *feedEnv) seedPost(t *testing.T, level int16, ward, constituency, county int32, score float32) string {
	t.Helper()
	ctx := context.Background()
	channelSeq++
	authorID, _ := e.user(t, int(ward), int(constituency), int(county))
	var channelID int64
	if err := e.pool.QueryRow(ctx, `INSERT INTO channels (public_id, ward_id, name) VALUES ($1, $2, $3) RETURNING id`,
		uuid.Must(uuid.NewV7()), ward, fmt.Sprintf("c%d", channelSeq)).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := e.pool.Exec(ctx, `
		INSERT INTO posts (public_id, channel_id, author_id, content, level, ward_id, constituency_id, county_id, score)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, channelID, authorID, fmt.Sprintf("post %d at level %d", channelSeq, level), level, ward, constituency, county, score); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func (e *feedEnv) feed(t *testing.T, token, query string) (FeedResponse, *http.Response) {
	t.Helper()
	rec := e.do(t, "GET", "/v1/feed"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("feed%s: %d %s", query, rec.Code, rec.Body)
	}
	var f FeedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	return f, rec.Result()
}

func ids(f FeedResponse) []string {
	out := make([]string, len(f.Items))
	for i, p := range f.Items {
		out[i] = p.PostID
	}
	return out
}

func TestFeed_MergesFourLevelsByScoreWithinScope(t *testing.T) {
	e := newFeedEnv(t)
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)

	ward := e.seedPost(t, LevelWard, kiamwangi, gatunduSouth, kiambu, 10)
	national := e.seedPost(t, LevelNational, kitisuru, westlands, nairobiCounty, 400)
	constituency := e.seedPost(t, LevelConstituency, kiganjo, gatunduSouth, kiambu, 120)
	county := e.seedPost(t, LevelCounty, murera, juja, kiambu, 250)
	// T-2.1.4.3 — none of these are the caller's scopes.
	e.seedPost(t, LevelWard, kiganjo, gatunduSouth, kiambu, 999)                // neighbouring ward
	e.seedPost(t, LevelConstituency, murera, juja, kiambu, 999)                 // other constituency
	e.seedPost(t, LevelCounty, kitisuru, westlands, nairobiCounty, 999)         // other county
	e.seedPost(t, LevelWard, kitisuru, westlands, nairobiCounty, 999)           // other ward elsewhere
	hidden := e.seedPost(t, LevelWard, kiamwangi, gatunduSouth, kiambu, 999)    // frozen
	sponsored := e.seedPost(t, LevelWard, kiamwangi, gatunduSouth, kiambu, 999) // not organic (T-2.1.4.6)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `UPDATE posts SET state = 2 WHERE public_id = $1`, hidden); err != nil {
		t.Fatal(err)
	}
	// The sponsored flag is fixed at insert, so re-insert that row as sponsored.
	if _, err := e.pool.Exec(ctx, `
		WITH old AS (DELETE FROM posts WHERE public_id = $1 RETURNING *)
		INSERT INTO posts (public_id, channel_id, author_id, content, level, ward_id, constituency_id, county_id, score,
		                   sponsored, label_text_en, label_text_sw)
		SELECT public_id, channel_id, author_id, content, level, ward_id, constituency_id, county_id, score,
		       TRUE, 'Sponsored civic message', 'Ujumbe wa kiraia uliofadhiliwa' FROM old`, sponsored); err != nil {
		t.Fatal(err)
	}
	// A reply never ranks on its own.
	if rec := e.do(t, "POST", "/v1/posts/"+ward+"/replies", tok, map[string]any{"content": "Asante"}); rec.Code != http.StatusCreated {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body)
	}

	f, _ := e.feed(t, tok, "")
	want := []string{national, county, constituency, ward}
	if got := ids(f); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("feed = %v, want %v", got, want)
	}
	if f.HasMore || f.NextCursor != "" {
		t.Errorf("has_more %v cursor %q", f.HasMore, f.NextCursor)
	}
	for i, level := range []int16{4, 3, 2, 1} {
		if p := f.Items[i]; p.Level != level || p.Sponsored || p.Liked == nil || *p.Liked || p.Author == nil || p.Content == nil {
			t.Errorf("item %d = %+v", i, p)
		}
	}
	if f.Items[3].Counts.Replies != 1 {
		t.Errorf("ward post replies = %d", f.Items[3].Counts.Replies)
	}

	// One level at a time.
	f, _ = e.feed(t, tok, "?level=constituency")
	if got := ids(f); len(got) != 1 || got[0] != constituency {
		t.Errorf("constituency feed = %v", got)
	}
	if rec := e.do(t, "GET", "/v1/feed?level=planet", tok, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad level: %d", rec.Code)
	}

	// Sponsored posts are labelled in both languages wherever they are shown.
	if p := e.getPost(t, sponsored, tok); !p.Sponsored || p.Label == nil || p.Label.SW != "Ujumbe wa kiraia uliofadhiliwa" {
		t.Errorf("sponsored post = %+v", p)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE posts SET label_text_en = 'x' WHERE public_id = $1`, sponsored); err == nil {
		t.Error("sponsored label changed after insert")
	}
}

func TestFeed_CachedFirstPageAndInvalidation(t *testing.T) {
	e := newFeedEnv(t)
	ctx := context.Background()
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	first := e.seedPost(t, LevelWard, kiamwangi, gatunduSouth, kiambu, 5)

	if _, res := e.feed(t, tok, ""); res.Header.Get("X-Feed-Cache") != "miss" {
		t.Errorf("first request cache = %q", res.Header.Get("X-Feed-Cache"))
	}
	// T-2.1.4.2 — the second request is served from Redis, even though the
	// database has moved on (a post written behind the API's back).
	behind := e.seedPost(t, LevelWard, kiamwangi, gatunduSouth, kiambu, 50)
	f, res := e.feed(t, tok, "")
	if res.Header.Get("X-Feed-Cache") != "hit" || len(f.Items) != 1 || f.Items[0].PostID != first {
		t.Fatalf("cached feed = %v (%s)", ids(f), res.Header.Get("X-Feed-Cache"))
	}
	// Liked is per caller, never cached.
	if rec := e.do(t, "POST", "/v1/posts/"+first+"/likes", tok, nil); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	if f, _ := e.feed(t, tok, ""); f.Items[0].Liked == nil || !*f.Items[0].Liked {
		t.Errorf("liked not reflected: %+v", f.Items[0])
	}

	// Bumping the ward version drops every cached page of that feed.
	if err := e.cache.Bump(ctx, Scope{LevelWard, kiamwangi}); err != nil {
		t.Fatal(err)
	}
	f, res = e.feed(t, tok, "")
	if got := ids(f); res.Header.Get("X-Feed-Cache") != "partial" || strings.Join(got, ",") != behind+","+first {
		t.Fatalf("after bump = %v (%s)", got, res.Header.Get("X-Feed-Cache"))
	}

	// T-2.1.4.5 — elevation moves the post; both affected feeds are bumped.
	if _, err := e.pool.Exec(ctx, `UPDATE posts SET level = 2 WHERE public_id = $1`, behind); err != nil {
		t.Fatal(err)
	}
	p, err := NewStore(e.pool).PostByPublicID(ctx, uuid.MustParse(behind))
	if err != nil {
		t.Fatal(err)
	}
	if err := InvalidatePost(ctx, e.cache, p, LevelWard); err != nil {
		t.Fatal(err)
	}
	_, neighbour := e.user(t, kiganjo, gatunduSouth, kiambu)
	if f, _ := e.feed(t, neighbour, ""); len(f.Items) != 1 || f.Items[0].PostID != behind || f.Items[0].Level != 2 {
		t.Errorf("neighbour after elevation = %+v", f.Items)
	}
	if f, _ := e.feed(t, tok, "?level=ward"); len(f.Items) != 1 || f.Items[0].PostID != first {
		t.Errorf("ward feed after elevation = %v", ids(f))
	}

	// …and removal (moderation) drops it from its current feed.
	if _, err := e.pool.Exec(ctx, `UPDATE posts SET state = 3, content = NULL WHERE public_id = $1`, behind); err != nil {
		t.Fatal(err)
	}
	if err := InvalidatePost(ctx, e.cache, p, p.Level); err != nil {
		t.Fatal(err)
	}
	if f, _ := e.feed(t, neighbour, ""); len(f.Items) != 0 {
		t.Errorf("removed post still in feed: %v", ids(f))
	}
}

func TestFeed_TamperedCacheFallsBackToDatabase(t *testing.T) {
	e := newFeedEnv(t)
	ctx := context.Background()
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	real := e.seedPost(t, LevelWard, kiamwangi, gatunduSouth, kiambu, 5)
	e.feed(t, tok, "")

	// F-06 — overwrite the cached ward page with forged content.
	key := pageKey(Scope{LevelWard, kiamwangi}, 0)
	forged, _ := json.Marshal([]feedEntry{{ID: 1, Post: PostJSON{PostID: uuid.NewString(), Score: 1000}}})
	if err := e.cache.Client.Set(ctx, key, append(make([]byte, 32), forged...), 0).Err(); err != nil {
		t.Fatal(err)
	}
	f, res := e.feed(t, tok, "")
	if got := ids(f); len(got) != 1 || got[0] != real || res.Header.Get("X-Feed-Cache") == "hit" {
		t.Errorf("feed with forged cache = %v (%s)", got, res.Header.Get("X-Feed-Cache"))
	}
}

func TestFeed_CursorPagination(t *testing.T) {
	e := newFeedEnv(t)
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	var all []string
	// Ties in score are ordered by id, so paging never skips or repeats.
	for i := range 7 {
		all = append(all, e.seedPost(t, LevelWard, kiamwangi, gatunduSouth, kiambu, float32(100-10*(i/2))))
		all = append(all, e.seedPost(t, LevelCounty, murera, juja, kiambu, float32(95-10*(i/2))))
	}

	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		q := "?limit=4"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		f, _ := e.feed(t, tok, q)
		got = append(got, ids(f)...)
		if !f.HasMore {
			if pages != 3 || f.NextCursor != "" {
				t.Errorf("pages = %d, last cursor %q", pages, f.NextCursor)
			}
			break
		}
		cursor = f.NextCursor
	}
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Errorf("duplicate %s", id)
		}
		seen[id] = true
	}
	if len(got) != len(all) {
		t.Errorf("paged %d of %d posts", len(got), len(all))
	}
	full, _ := e.feed(t, tok, "")
	if strings.Join(ids(full), ",") != strings.Join(got, ",") {
		t.Errorf("paged order differs from one page:\n%v\n%v", got, ids(full))
	}

	// Cursors are signed.
	body, _, _ := strings.Cut(cursor, ".")
	for _, bad := range []string{"nope", body + ".AAAA", body} {
		if rec := e.do(t, "GET", "/v1/feed?cursor="+bad, tok, nil); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("cursor %q: %d", bad, rec.Code)
		}
	}
}
