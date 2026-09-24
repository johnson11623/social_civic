package post

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Feature 2.1.2 — posts against a real PostgreSQL.

// A channel in Kiamwangi and a token for a Kiamwangi resident.
func (e *env) wardChannel(t *testing.T, name string) (channelID, token string, userID int64) {
	t.Helper()
	userID, token = e.user(t, kiamwangi, gatunduSouth, kiambu)
	rec := e.do(t, "POST", "/v1/channels", token, map[string]any{"name": name})
	var ch ChannelJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)
	if ch.ChannelID == "" {
		t.Fatalf("create channel: %d %s", rec.Code, rec.Body)
	}
	return ch.ChannelID, token, userID
}

func (e *env) post(t *testing.T, channelID, token, content string) (PostJSON, int) {
	t.Helper()
	rec := e.do(t, "POST", "/v1/channels/"+channelID+"/posts", token, map[string]any{"content": content})
	var p PostJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p, rec.Code
}

func TestPosts_CreateAtWardLevel(t *testing.T) {
	e := newEnv(t)
	channel, tok, userID := e.wardChannel(t, "water-points")

	p, status := e.post(t, channel, tok, "  Water point broken at Kiamwangi market  ")
	if status != http.StatusCreated {
		t.Fatalf("status %d", status)
	}
	if p.Level != LevelWard || p.WardID != kiamwangi || p.State != "active" || p.Score != 0 ||
		p.Content == nil || *p.Content != "Water point broken at Kiamwangi market" || p.ChannelID != channel {
		t.Errorf("post = %+v", p)
	}

	// Origin ward and ancestors copied; row lands in this month's partition.
	var ward, constituency, county int32
	var author int64
	var partition string
	err := e.pool.QueryRow(context.Background(), `
		SELECT ward_id, constituency_id, county_id, author_id, tableoid::regclass::text FROM posts WHERE public_id = $1`,
		p.PostID).Scan(&ward, &constituency, &county, &author, &partition)
	if err != nil {
		t.Fatal(err)
	}
	if ward != kiamwangi || constituency != gatunduSouth || county != kiambu || author != userID {
		t.Errorf("scope %d/%d/%d author %d", ward, constituency, county, author)
	}
	if want := "posts_" + time.Now().UTC().Format("2006_01"); partition != want {
		t.Errorf("partition = %s, want %s", partition, want)
	}

	// T-2.1.2.5 — post.created enqueued, without the post's text.
	var payload string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT payload::text FROM outbox WHERE topic = 'post.created'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"level": 1`) || !strings.Contains(payload, `"ward_id": 551`) || strings.Contains(payload, "Water point") {
		t.Errorf("event payload = %s", payload)
	}
	// T-2.1.2.6 — the ward feed cache version was bumped.
	if len(e.cache.bumped) != 1 || e.cache.bumped[0] != kiamwangi {
		t.Errorf("cache bumps = %v", e.cache.bumped)
	}
}

func TestPosts_Validation(t *testing.T) {
	e := newEnv(t)
	channel, tok, _ := e.wardChannel(t, "roads")

	for name, content := range map[string]string{"empty": "", "whitespace": "   \n\t "} {
		if _, status := e.post(t, channel, tok, content); status != 422 {
			t.Errorf("%s: %d", name, status)
		}
	}
	if _, status := e.post(t, channel, tok, strings.Repeat("ñ", 501)); status != 422 {
		t.Errorf("501 chars: %d", status)
	}
	if _, status := e.post(t, channel, tok, strings.Repeat("ñ", 500)); status != 201 {
		t.Errorf("500 chars: %d", status)
	}
	rec := e.do(t, "POST", "/v1/channels/"+channel+"/posts", tok, map[string]any{"content": "photo", "media_url": "https://x/y.jpg"})
	if rec.Code != 422 {
		t.Errorf("media: %d", rec.Code)
	}
	if n := count(t, e.pool, "SELECT count(*) FROM posts"); n != 1 {
		t.Errorf("posts = %d, want 1", n)
	}
}

func TestPosts_WhereYouCanPost(t *testing.T) {
	e := newEnv(t)
	channel, _, _ := e.wardChannel(t, "health")

	// Another ward's resident can't post in a Kiamwangi channel.
	_, kiganjo := e.user(t, 552, gatunduSouth, kiambu)
	if rec := e.do(t, "POST", "/v1/channels/"+channel+"/posts", kiganjo, map[string]any{"content": "hi"}); rec.Code != 403 || code(t, rec) != "not_member" {
		t.Errorf("other ward: %d %s", rec.Code, rec.Body)
	}
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	for _, bad := range []string{"nope", "01900000-0000-7000-8000-000000000000"} {
		if rec := e.do(t, "POST", "/v1/channels/"+bad+"/posts", tok, map[string]any{"content": "hi"}); rec.Code != 404 {
			t.Errorf("channel %s: %d", bad, rec.Code)
		}
	}
	// Read-only channels (announcements) refuse ordinary members.
	_, _ = e.pool.Exec(context.Background(), "UPDATE channels SET read_only = TRUE WHERE name = 'health'")
	if rec := e.do(t, "POST", "/v1/channels/"+channel+"/posts", tok, map[string]any{"content": "hi"}); rec.Code != 403 || code(t, rec) != "read_only_channel" {
		t.Errorf("read-only: %d %s", rec.Code, rec.Body)
	}
}

func TestPosts_ConsentRequired(t *testing.T) {
	e := newEnv(t)
	channel, tok, userID := e.wardChannel(t, "jobs")
	_, _ = e.pool.Exec(context.Background(), "UPDATE consents SET withdrawn_at = now() WHERE user_id = $1", userID)
	if _, status := e.post(t, channel, tok, "hello"); status != 403 {
		t.Errorf("withdrawn consent: %d", status)
	}
}

func TestPosts_VisibilityFollowsLevel(t *testing.T) {
	e := newEnv(t)
	channel, author, _ := e.wardChannel(t, "water")
	p, _ := e.post(t, channel, author, "Water point broken")

	_, sameWard := e.user(t, kiamwangi, gatunduSouth, kiambu)
	_, sameConstituency := e.user(t, 552, gatunduSouth, kiambu) // Kiganjo
	_, sameCounty := e.user(t, 555, 112, kiambu)                // Gituamba, Gatundu North
	_, elsewhere := e.user(t, 1400, 280, 47)                    // Kitisuru, Nairobi City

	visible := func(tok string) int { return e.do(t, "GET", "/v1/posts/"+p.PostID, tok, nil).Code }
	setLevel := func(level int) {
		_, _ = e.pool.Exec(context.Background(), "UPDATE posts SET level = $1 WHERE public_id = $2", level, p.PostID)
	}
	check := func(name string, want map[string]int) {
		t.Helper()
		for who, tok := range map[string]string{"ward": sameWard, "constituency": sameConstituency, "county": sameCounty, "elsewhere": elsewhere} {
			if got := visible(tok); got != want[who] {
				t.Errorf("%s: %s sees %d, want %d", name, who, got, want[who])
			}
		}
	}
	check("ward", map[string]int{"ward": 200, "constituency": 403, "county": 403, "elsewhere": 403})
	setLevel(LevelConstituency)
	check("constituency", map[string]int{"ward": 200, "constituency": 200, "county": 403, "elsewhere": 403})
	setLevel(LevelCounty)
	check("county", map[string]int{"ward": 200, "constituency": 200, "county": 200, "elsewhere": 403})
	setLevel(LevelNational)
	check("national", map[string]int{"ward": 200, "constituency": 200, "county": 200, "elsewhere": 200})

	rec := e.do(t, "GET", "/v1/posts/"+p.PostID, sameWard, nil)
	var got PostJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Author == nil || got.Author.DisplayName != "Test" || got.Channel != "water" {
		t.Errorf("post detail = %+v", got)
	}

	// Removed content is never returned; purged posts are gone.
	_, _ = e.pool.Exec(context.Background(), "UPDATE posts SET state = 3, content = NULL WHERE public_id = $1", p.PostID)
	rec = e.do(t, "GET", "/v1/posts/"+p.PostID, sameWard, nil)
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if raw["state"] != "tombstoned" || raw["content"] != nil {
		t.Errorf("tombstoned = %v", raw)
	}
	_, _ = e.pool.Exec(context.Background(), "UPDATE posts SET state = 4 WHERE public_id = $1", p.PostID)
	if code := visible(sameWard); code != 404 {
		t.Errorf("purged: %d", code)
	}
}

func TestPosts_CacheFailureDoesNotFailThePost(t *testing.T) {
	e := newEnv(t)
	channel, tok, _ := e.wardChannel(t, "events")
	e.cache.err = errors.New("redis down")
	if _, status := e.post(t, channel, tok, "still posted"); status != 201 {
		t.Errorf("status %d", status)
	}
}

func TestPostPartitions(t *testing.T) {
	e := newEnv(t)
	var created int
	if err := e.pool.QueryRow(context.Background(), "SELECT ensure_post_partitions(3)").Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Errorf("re-running created %d partitions, want 0 (idempotent)", created)
	}
	var partitions int
	_ = e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_inherits WHERE inhparent = 'posts'::regclass`).Scan(&partitions)
	if partitions < 5 { // this month + 3 ahead + default
		t.Errorf("partitions = %d", partitions)
	}
}

func TestCanView(t *testing.T) {
	u := ActiveUser{WardID: 551, ConstituencyID: 111, CountyID: 22}
	cases := []struct {
		level          int16
		ward, con, cty int32
		want           bool
	}{
		{LevelWard, 551, 111, 22, true}, {LevelWard, 552, 111, 22, false},
		{LevelConstituency, 552, 111, 22, true}, {LevelConstituency, 555, 112, 22, false},
		{LevelCounty, 555, 112, 22, true}, {LevelCounty, 1400, 280, 47, false},
		{LevelNational, 1400, 280, 47, true}, {9, 551, 111, 22, false},
	}
	for _, c := range cases {
		if got := CanView(u, c.level, c.ward, c.con, c.cty); got != c.want {
			t.Errorf("CanView(level %d, %d/%d/%d) = %v", c.level, c.ward, c.con, c.cty, got)
		}
	}
}
