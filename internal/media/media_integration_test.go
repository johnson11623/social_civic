package media

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/platform/authn"
)

// The upload API (and, where ffmpeg allows, the worker) against a real
// PostgreSQL and S3 store: TEST_DATABASE_URL and TEST_S3_ENDPOINT.

type env struct {
	pool    *pgxpool.Pool
	storage *Storage
	router  http.Handler
	token   string
	other   string
	cdn     string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dbURL, s3 := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_S3_ENDPOINT")
	if dbURL == "" || s3 == "" {
		t.Skip("TEST_DATABASE_URL and TEST_S3_ENDPOINT not set; run `make test-media`")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "TRUNCATE users, media, outbox RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("reset (is the database migrated?): %v", err)
	}
	storage, err := NewStorage(StorageConfig{Endpoint: s3, AccessKey: "civicdev", SecretKey: "civicdev-secret-change-me",
		Originals: "test-originals", Public: "test-media"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.EnsureBuckets(ctx); err != nil {
		t.Fatal(err)
	}
	tokens, _ := identity.NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), time.Now)
	token := func() string {
		id := uuid.Must(uuid.NewV7())
		hash := append(id[:], id[:]...)
		if _, err := pool.Exec(ctx, `INSERT INTO users (public_id, national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
			VALUES ($1, $2, 'v1', 'U', 551, 111, 22)`, id, hash); err != nil {
			t.Fatal(err)
		}
		pair, _ := tokens.Issue(id.String(), identity.ScopeClaim{Ward: 551, Constituency: 111, County: 22})
		return pair.Access
	}
	e := &env{pool: pool, storage: storage, token: token(), other: token(), cdn: "http://cdn.test/media/variants"}
	h := &Handlers{Pool: pool, Storage: storage, CDNBase: e.cdn, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
	r := chi.NewRouter()
	auth := authn.Middleware(tokens, identity.Unauthenticated)
	r.With(auth).Post("/v1/media/uploads", h.CreateUpload)
	r.With(auth).Post("/v1/media/{media_id}/complete", h.Complete)
	r.With(auth).Get("/v1/media/{media_id}", h.Get)
	e.router = r
	return e
}

func (e *env) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func code(rec *httptest.ResponseRecorder) string {
	var p struct{ Code string }
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Code
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// upload asks for an upload, PUTs body to the signed URL and completes it.
func (e *env) upload(t *testing.T, mime string, body []byte) (string, *httptest.ResponseRecorder) {
	t.Helper()
	rec := e.do(t, "POST", "/v1/media/uploads", e.token, map[string]any{"mime_type": mime, "size_bytes": len(body), "alt_text": "A water point at Kiamwangi market"})
	var up UploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	if rec.Code != http.StatusCreated || up.Upload.Method != "PUT" || up.Upload.Headers["Content-Type"] != mime {
		t.Fatalf("create upload: %d %s", rec.Code, rec.Body)
	}
	req, _ := http.NewRequest(http.MethodPut, up.Upload.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", mime)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("PUT to signed URL: %v %v", res, err)
	}
	return up.MediaID, e.do(t, "POST", "/v1/media/"+up.MediaID+"/complete", e.token, nil)
}

func TestUpload_SignedPutCompleteAndQueue(t *testing.T) {
	e := newEnv(t)
	id, rec := e.upload(t, "image/png", pngBytes(t, 640, 480))
	var m MediaJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != http.StatusOK || m.State != "processing" || m.Kind != "image" || m.AltText == "" {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE topic = 'media.uploaded' AND payload->'data'->>'media_id' = $1`, id).Scan(&n)
	if n != 1 {
		t.Errorf("media.uploaded events = %d", n)
	}
	if rec := e.do(t, "POST", "/v1/media/"+id+"/complete", e.token, nil); rec.Code != http.StatusOK {
		t.Errorf("second complete: %d", rec.Code)
	}
	if rec := e.do(t, "GET", "/v1/media/"+id, e.other, nil); rec.Code != http.StatusNotFound {
		t.Errorf("other user: %d", rec.Code)
	}
}

func TestUpload_Validation(t *testing.T) {
	e := newEnv(t)
	// The description is optional.
	if rec := e.do(t, "POST", "/v1/media/uploads", e.token, map[string]any{"mime_type": "image/png", "size_bytes": 10}); rec.Code != http.StatusCreated {
		t.Errorf("without alt text: %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"mime_type": "image/gif", "size_bytes": 10, "alt_text": "x"}, "unsupported_type"},
		{map[string]any{"mime_type": "image/png", "size_bytes": 15 << 20, "alt_text": "x"}, "too_large"},
		{map[string]any{"mime_type": "video/mp4", "size_bytes": 101 << 20, "alt_text": "x"}, "too_large"},
	} {
		if rec := e.do(t, "POST", "/v1/media/uploads", e.token, c.body); rec.Code != 422 || code(rec) != c.code {
			t.Errorf("%v: %d %s", c.body, rec.Code, rec.Body)
		}
	}
	rec := e.do(t, "POST", "/v1/media/uploads", e.token, map[string]any{"mime_type": "image/png", "size_bytes": 10, "alt_text": "x"})
	var up UploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	if rec := e.do(t, "POST", "/v1/media/"+up.MediaID+"/complete", e.token, nil); rec.Code != 409 || code(rec) != "upload_missing" {
		t.Errorf("missing: %d %s", rec.Code, rec.Body)
	}
}

func TestUpload_RejectsContentThatLies(t *testing.T) {
	e := newEnv(t)
	id, rec := e.upload(t, "image/png", []byte("<html><script>alert(1)</script></html>"))
	if rec.Code != 422 || code(rec) != "upload_rejected" {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	var m MediaJSON
	_ = json.Unmarshal(e.do(t, "GET", "/v1/media/"+id, e.token, nil).Body.Bytes(), &m)
	if m.State != "failed" {
		t.Errorf("state = %s", m.State)
	}
	if _, _, err := e.storage.StatOriginal(context.Background(), id+"/original"); err != ErrNotFound {
		t.Errorf("rejected original kept: %v", err)
	}
}

func TestWorker_ImageEndToEnd(t *testing.T) {
	requireFFmpeg(t)
	e := newEnv(t)
	id, _ := e.upload(t, "image/png", pngBytes(t, 1200, 800))
	w := &Worker{Pool: e.pool, Storage: e.storage, Processor: &Processor{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
	if err := w.Handle(context.Background(), uuid.MustParse(id)); err != nil {
		t.Fatal(err)
	}
	var m MediaJSON
	_ = json.Unmarshal(e.do(t, "GET", "/v1/media/"+id, e.token, nil).Body.Bytes(), &m)
	if m.State != "ready" || m.Width != 1200 || len(m.Images) != 3 || !strings.HasPrefix(m.Placeholder, "data:image/") {
		t.Fatalf("media = %+v", m)
	}
	if want := e.cdn + "/" + id + "/thumbnail.jpg"; m.Images[0].JPEG != want {
		t.Errorf("url = %s, want %s", m.Images[0].JPEG, want)
	}
	res, err := http.Get("http://" + os.Getenv("TEST_S3_ENDPOINT") + "/test-media/" + id + "/medium.jpg")
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("public variant: %v %v", res, err)
	}
	if err := w.Handle(context.Background(), uuid.MustParse(id)); err != nil {
		t.Errorf("redelivery: %v", err)
	}
	var events int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE topic = 'media.processed'`).Scan(&events)
	if events != 1 {
		t.Errorf("media.processed events = %d", events)
	}
}
