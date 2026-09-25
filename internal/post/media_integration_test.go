package post

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Posts carrying media (docs/media): ready media of the author's only.

// readyMedia inserts processed media for userID, as the media worker leaves it.
func (e *env) readyMedia(t *testing.T, userID int64, kind int16, state int16) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	variants := `{"images":[{"name":"thumbnail","width":320,"height":240,"webp":"` + id.String() + `/thumbnail.webp","jpeg":"` + id.String() + `/thumbnail.jpg"}]}`
	if kind == 2 {
		variants = `{"poster":{"name":"poster","width":640,"height":360,"webp":"` + id.String() + `/poster.webp","jpeg":"` + id.String() + `/poster.jpg"},"hls":"` + id.String() + `/master.m3u8","renditions":["240p"]}`
	}
	var v any = variants
	if state != 3 {
		v = nil
	}
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO media (public_id, owner_id, kind, mime_type, declared_size, alt_text, state, original_key, width, height, placeholder, variants)
		VALUES ($1, $2, $3, 'image/png', 100, 'A dry tap at the market', $4, 'k', 640, 480, 'data:image/webp;base64,AA', $5::jsonb)`,
		id, userID, kind, state, v); err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func TestPosts_WithImageAndNoText(t *testing.T) {
	e := newEnv(t)
	channel, tok, userID := e.wardChannel(t, "water")
	mediaID := e.readyMedia(t, userID, 1, 3)

	rec := e.do(t, "POST", "/v1/channels/"+channel+"/posts", tok, map[string]any{"media_id": mediaID})
	var p PostJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusCreated || p.Content == nil || *p.Content != "" || p.Media == nil ||
		p.Media.MediaID != mediaID || p.Media.AltText != "A dry tap at the market" ||
		p.Media.Images[0].WebP != "http://cdn.test/media/variants/"+mediaID+"/thumbnail.webp" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	// Every read path carries it.
	got := e.getPost(t, p.PostID, tok)
	if got.Media == nil || got.Media.Placeholder == "" || got.Media.Width != 640 {
		t.Errorf("get = %+v", got.Media)
	}
	var page ChannelPostsResponse
	_ = json.Unmarshal(e.do(t, "GET", "/v1/channels/"+channel+"/posts", tok, nil).Body.Bytes(), &page)
	if len(page.Items) != 1 || page.Items[0].Media == nil {
		t.Errorf("channel posts = %+v", page.Items)
	}
	// Removed posts hide their media with their text.
	if _, err := e.pool.Exec(context.Background(), `UPDATE posts SET state = 3 WHERE public_id = $1`, p.PostID); err != nil {
		t.Fatal(err)
	}
	if got := e.getPost(t, p.PostID, tok); got.Media != nil || got.Content != nil {
		t.Errorf("removed post still shows media: %+v", got)
	}
}

func TestPosts_VideoWithText(t *testing.T) {
	e := newEnv(t)
	channel, tok, userID := e.wardChannel(t, "water")
	mediaID := e.readyMedia(t, userID, 2, 3)
	p, status := e.post(t, channel, tok, "") // helper sends content only
	if status != http.StatusUnprocessableEntity {
		t.Errorf("empty without media: %d", status)
	}
	rec := e.do(t, "POST", "/v1/channels/"+channel+"/posts", tok, map[string]any{"content": "Road flooded near the school", "media_id": mediaID})
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusCreated || p.Media == nil || p.Media.Kind != "video" ||
		p.Media.HLSURL != "http://cdn.test/media/variants/"+mediaID+"/master.m3u8" || p.Media.Poster == nil {
		t.Fatalf("video post: %d %s", rec.Code, rec.Body)
	}
}

func TestPosts_MediaMustBeReadyAndOwn(t *testing.T) {
	e := newEnv(t)
	channel, tok, userID := e.wardChannel(t, "water")
	otherID, _ := e.user(t, kiamwangi, gatunduSouth, kiambu)
	for name, id := range map[string]string{
		"still processing": e.readyMedia(t, userID, 1, 2),
		"someone else's":   e.readyMedia(t, otherID, 1, 3),
		"unknown":          uuid.NewString(),
	} {
		rec := e.do(t, "POST", "/v1/channels/"+channel+"/posts", tok, map[string]any{"content": "x", "media_id": id})
		if rec.Code != 422 || code(t, rec) != "media_not_ready" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}
