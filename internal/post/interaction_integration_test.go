package post

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

// Feature 2.1.3 — likes and replies against a real PostgreSQL.

func (e *env) getPost(t *testing.T, id, token string) PostJSON {
	t.Helper()
	rec := e.do(t, "GET", "/v1/posts/"+id, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get post: %d %s", rec.Code, rec.Body)
	}
	var p PostJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p
}

func TestLikes_OncePerUserAndUnlike(t *testing.T) {
	e := newEnv(t)
	channel, tok, _ := e.wardChannel(t, "water")
	p, _ := e.post(t, channel, tok, "Borehole needs repair")
	_, other := e.user(t, kiamwangi, gatunduSouth, kiambu)

	rec := e.do(t, "POST", "/v1/posts/"+p.PostID+"/likes", other, nil)
	var lr LikeResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &lr)
	if rec.Code != http.StatusCreated || lr.Likes != 1 || !lr.Liked {
		t.Fatalf("like: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, "POST", "/v1/posts/"+p.PostID+"/likes", other, nil); rec.Code != http.StatusConflict || code(t, rec) != "already_liked" {
		t.Errorf("second like: %d %s", rec.Code, rec.Body)
	}
	got := e.getPost(t, p.PostID, other)
	if got.Counts.Likes != 1 || got.Liked == nil || !*got.Liked {
		t.Errorf("post after like = %+v", got)
	}
	if got := e.getPost(t, p.PostID, tok); got.Liked == nil || *got.Liked {
		t.Errorf("author's liked = %v", got.Liked)
	}

	rec = e.do(t, "DELETE", "/v1/posts/"+p.PostID+"/likes", other, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &lr)
	if rec.Code != http.StatusOK || lr.Likes != 0 || lr.Liked {
		t.Fatalf("unlike: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, "DELETE", "/v1/posts/"+p.PostID+"/likes", other, nil); rec.Code != http.StatusNotFound || code(t, rec) != "like_not_found" {
		t.Errorf("second unlike: %d %s", rec.Code, rec.Body)
	}

	// Liking again after unliking is fine; the user is one actor throughout.
	if rec := e.do(t, "POST", "/v1/posts/"+p.PostID+"/likes", other, nil); rec.Code != http.StatusCreated {
		t.Errorf("re-like: %d", rec.Code)
	}
	if n := count(t, e.pool, `SELECT unique_actors FROM post_counters c JOIN posts p ON p.id = c.post_id WHERE p.public_id = $1`, p.PostID); n != 1 {
		t.Errorf("unique_actors = %d", n)
	}
	// T-2.1.3.6 — like, unlike, like recorded; the first like is a new actor.
	if n := count(t, e.pool, `SELECT count(*) FROM outbox WHERE topic = 'interaction.recorded'`); n != 3 {
		t.Errorf("interaction events = %d", n)
	}
	if n := count(t, e.pool, `SELECT count(*) FROM outbox WHERE topic = 'interaction.recorded' AND payload->'data'->>'new_actor' = 'true'`); n != 1 {
		t.Errorf("new-actor events = %d", n)
	}
	if len(e.cache.bumped) != 4 { // post + 3 interactions
		t.Errorf("cache bumps = %v", e.cache.bumped)
	}
}

func TestLikes_ConcurrentLikesCountOnce(t *testing.T) {
	e := newEnv(t)
	channel, tok, _ := e.wardChannel(t, "water")
	p, _ := e.post(t, channel, tok, "Borehole needs repair")
	_, other := e.user(t, kiamwangi, gatunduSouth, kiambu)

	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for range 10 {
		wg.Go(func() { codes <- e.do(t, "POST", "/v1/posts/"+p.PostID+"/likes", other, nil).Code })
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		if c == http.StatusCreated {
			created++
		} else if c != http.StatusConflict {
			t.Errorf("unexpected status %d", c)
		}
	}
	if created != 1 || e.getPost(t, p.PostID, other).Counts.Likes != 1 {
		t.Errorf("created = %d", created)
	}
}

func TestLikes_ScopeAndState(t *testing.T) {
	e := newEnv(t)
	channel, tok, _ := e.wardChannel(t, "water")
	p, _ := e.post(t, channel, tok, "Borehole needs repair")
	_, elsewhere := e.user(t, 1400, 280, 47)

	if rec := e.do(t, "POST", "/v1/posts/"+p.PostID+"/likes", elsewhere, nil); rec.Code != http.StatusForbidden || code(t, rec) != "out_of_scope" {
		t.Errorf("out-of-scope like: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, "POST", "/v1/posts/0190a0a0-0000-7000-8000-000000000000/likes", tok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing post: %d", rec.Code)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE posts SET state = 2 WHERE public_id = $1`, p.PostID); err != nil {
		t.Fatal(err)
	}
	if rec := e.do(t, "POST", "/v1/posts/"+p.PostID+"/likes", tok, nil); rec.Code != http.StatusConflict || code(t, rec) != "post_not_active" {
		t.Errorf("frozen like: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, "POST", "/v1/posts/"+p.PostID+"/replies", tok, map[string]any{"content": "hi"}); rec.Code != http.StatusConflict {
		t.Errorf("frozen reply: %d %s", rec.Code, rec.Body)
	}
}

func TestReplies_ThreadInheritsScope(t *testing.T) {
	e := newEnv(t)
	channel, tok, _ := e.wardChannel(t, "water")
	root, _ := e.post(t, channel, tok, "Borehole needs repair")
	_, neighbour := e.user(t, kiamwangi, gatunduSouth, kiambu)

	reply := func(parent, token, content string) PostJSON {
		t.Helper()
		rec := e.do(t, "POST", "/v1/posts/"+parent+"/replies", token, map[string]any{"content": content})
		if rec.Code != http.StatusCreated {
			t.Fatalf("reply: %d %s", rec.Code, rec.Body)
		}
		var p PostJSON
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return p
	}
	r1 := reply(root.PostID, neighbour, "I saw it too")
	r2 := reply(r1.PostID, tok, "Reported to the MCA")
	if r1.RootID != root.PostID || r1.ParentID != root.PostID || r2.RootID != root.PostID || r2.ParentID != r1.PostID ||
		r1.Level != LevelWard || r1.WardID != kiamwangi || r1.ChannelID != channel {
		t.Errorf("replies = %+v / %+v", r1, r2)
	}

	got := e.getPost(t, root.PostID, tok)
	if got.Counts.Replies != 2 {
		t.Errorf("root replies = %d", got.Counts.Replies)
	}
	if got := e.getPost(t, r1.PostID, tok); got.Counts.Replies != 1 || got.RootID != root.PostID {
		t.Errorf("r1 = %+v", got)
	}
	// Author and neighbour, each counted once.
	if n := count(t, e.pool, `SELECT unique_actors FROM post_counters c JOIN posts p ON p.id = c.post_id WHERE p.public_id = $1`, root.PostID); n != 2 {
		t.Errorf("unique_actors = %d", n)
	}

	// Paged, oldest first; asking via a reply returns the same thread.
	rec := e.do(t, "GET", "/v1/posts/"+r2.PostID+"/replies?limit=1", tok, nil)
	var page struct {
		PostID     string     `json:"post_id"`
		Items      []PostJSON `json:"items"`
		HasMore    bool       `json:"has_more"`
		NextCursor string     `json:"next_cursor"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if rec.Code != http.StatusOK || page.PostID != root.PostID || len(page.Items) != 1 || page.Items[0].PostID != r1.PostID || !page.HasMore {
		t.Fatalf("page 1: %d %s", rec.Code, rec.Body)
	}
	if l := page.Items[0].Liked; l == nil || *l {
		t.Errorf("reply liked = %v", l)
	}
	rec = e.do(t, "GET", "/v1/posts/"+root.PostID+"/replies?limit=1&cursor="+page.NextCursor, tok, nil)
	page.NextCursor = ""
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if len(page.Items) != 1 || page.Items[0].PostID != r2.PostID || page.Items[0].ParentID != r1.PostID || page.HasMore {
		t.Errorf("page 2: %s", rec.Body)
	}
	if rec := e.do(t, "GET", "/v1/posts/"+root.PostID+"/replies?cursor=!!", tok, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad cursor: %d", rec.Code)
	}

	// Replies stay out of scope for other wards, and out of the feed indexes.
	_, elsewhere := e.user(t, 1400, 280, 47)
	if rec := e.do(t, "GET", "/v1/posts/"+root.PostID+"/replies", elsewhere, nil); rec.Code != http.StatusForbidden {
		t.Errorf("out-of-scope thread: %d", rec.Code)
	}
	if rec := e.do(t, "POST", "/v1/posts/"+root.PostID+"/replies", tok, map[string]any{"content": " "}); code(t, rec) != "content_empty" {
		t.Errorf("empty reply: %s", rec.Body)
	}
	if n := count(t, e.pool, `SELECT count(*) FROM outbox WHERE topic = 'interaction.recorded' AND payload->'data'->>'type' = 'reply'`); n != 2 {
		t.Errorf("reply events = %d", n)
	}
}
