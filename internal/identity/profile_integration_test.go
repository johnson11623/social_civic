package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/platform/authn"
)

// The account page's profile against a real PostgreSQL.

type profileFixture struct {
	pool   *pgxpool.Pool
	router http.Handler
	access string
	userID int64
}

func newProfileFixture(t *testing.T) *profileFixture {
	t.Helper()
	pool := integrationPool(t)
	tokens, err := NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	f := &profileFixture{pool: pool}
	publicID := uuid.Must(uuid.NewV7())
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO users (public_id, national_id_hash, national_id_key_version, display_name, preferred_lang, ward_id, constituency_id, county_id)
		VALUES ($1, $2, 'v1', 'Wanjiku M.', 'sw', 551, 111, 22) RETURNING id`, publicID, bytes.Repeat([]byte{5}, 32)).Scan(&f.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO consents (user_id, version, granted_at) VALUES ($1, $2, now())`, f.userID, CurrentConsentVersion); err != nil {
		t.Fatal(err)
	}
	pair, err := tokens.Issue(publicID.String(), ScopeClaim{Ward: 551, Constituency: 111, County: 22})
	if err != nil {
		t.Fatal(err)
	}
	f.access = pair.Access
	h := &ProfileHandlers{Pool: pool, Boundary: testTree(t), MediaCDN: "http://cdn.test/media/variants",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r := chi.NewRouter()
	auth := authn.Middleware(tokens, Unauthenticated)
	r.With(auth).Get("/v1/users/me", h.Get)
	r.With(auth).Patch("/v1/users/me", h.Update)
	r.With(auth).Put("/v1/users/me/avatar", h.SetAvatar)
	r.With(auth).Delete("/v1/users/me/avatar", h.RemoveAvatar)
	f.router = r
	return f
}

func (f *profileFixture) call(t *testing.T, method string, body any) (*httptest.ResponseRecorder, Profile) {
	t.Helper()
	return f.callPath(t, method, "/v1/users/me", body)
}

func (f *profileFixture) callPath(t *testing.T, method, path string, body any) (*httptest.ResponseRecorder, Profile) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+f.access)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	var p Profile
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return rec, p
}

func TestProfile_GetAndUpdate(t *testing.T) {
	f := newProfileFixture(t)
	rec, p := f.call(t, http.MethodGet, nil)
	if rec.Code != http.StatusOK || p.DisplayName != "Wanjiku M." || p.PreferredLang != "sw" || p.Ward == nil ||
		p.Ward.Name != "Kiamwangi" || p.Ward.County != "Kiambu" || !p.Consent.Active || p.MFAEnabled || p.Erasure != nil {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}

	rec, p = f.call(t, http.MethodPatch, map[string]any{"display_name": "  Wanjiku Mwangi ", "preferred_lang": "en"})
	if rec.Code != http.StatusOK || p.DisplayName != "Wanjiku Mwangi" || p.PreferredLang != "en" {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	// Omitted fields stay as they are.
	if _, p = f.call(t, http.MethodPatch, map[string]any{"preferred_lang": "sw"}); p.DisplayName != "Wanjiku Mwangi" || p.PreferredLang != "sw" {
		t.Errorf("partial update = %+v", p)
	}
	for _, body := range []map[string]any{{"display_name": "   "}, {"preferred_lang": "fr"}} {
		if rec, _ := f.call(t, http.MethodPatch, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%v: %d", body, rec.Code)
		}
	}
	if rec, _ := f.call(t, http.MethodPatch, map[string]any{"ward_id": 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", rec.Code)
	}
}

func TestProfile_ShowsConsentMFAAndErasure(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE consents SET withdrawn_at = now() WHERE user_id = $1`, f.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO user_mfa (user_id, secret_enc, key_version, enabled_at) VALUES ($1, '\x00', 'v', now())`, f.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO erasure_requests (public_id, user_id, requested_at, ack_by, completion_by)
		VALUES (gen_random_uuid(), $1, now(), now() + interval '48 hours', now() + interval '30 days')`, f.userID); err != nil {
		t.Fatal(err)
	}
	_, p := f.call(t, http.MethodGet, nil)
	if p.Consent.Active || p.Consent.WithdrawnAt == nil || !p.MFAEnabled || p.Erasure == nil || p.Erasure.State != "pending" {
		t.Errorf("profile = %+v", p)
	}
}

// insertImage stores a media row for owner in the given state (3 = ready).
func insertImage(t *testing.T, pool *pgxpool.Pool, owner int64, kind, state int16) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	v := `{"images":[{"name":"thumbnail","width":320,"height":320,"jpeg":"` + id.String() + `/thumbnail.jpg"}]}`
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO media (public_id, owner_id, kind, mime_type, declared_size, state, original_key, variants)
		VALUES ($1, $2, $3, 'image/png', 100, $4, 'k', $5::jsonb)`, id, owner, kind, state, v); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProfile_Avatar(t *testing.T) {
	f := newProfileFixture(t)
	if _, p := f.call(t, http.MethodGet, nil); p.Avatar != nil {
		t.Fatalf("new user has an avatar: %+v", p.Avatar)
	}
	photo := insertImage(t, f.pool, f.userID, 1, 3)
	rec, p := f.callPath(t, http.MethodPut, "/v1/users/me/avatar", map[string]any{"media_id": photo.String()})
	want := "http://cdn.test/media/variants/" + photo.String() + "/thumbnail.jpg"
	if rec.Code != http.StatusOK || p.Avatar == nil || p.Avatar.URL != want || p.Avatar.MediaID != photo.String() {
		t.Fatalf("set: %d %s", rec.Code, rec.Body)
	}
	if _, p = f.call(t, http.MethodGet, nil); p.Avatar == nil || p.Avatar.URL != want {
		t.Fatalf("get after set: %+v", p.Avatar)
	}

	// Only the caller's own photos, once processed.
	var other int64
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO users (public_id, national_id_hash, national_id_key_version, display_name, preferred_lang, ward_id, constituency_id, county_id)
		VALUES ($1, $2, 'v1', 'Otieno', 'en', 551, 111, 22) RETURNING id`, uuid.Must(uuid.NewV7()), bytes.Repeat([]byte{6}, 32)).Scan(&other); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{
		"someone else's": insertImage(t, f.pool, other, 1, 3).String(),
		"processing":     insertImage(t, f.pool, f.userID, 1, 2).String(),
		"a video":        insertImage(t, f.pool, f.userID, 2, 3).String(),
		"not an id":      "nope",
	} {
		if rec, _ := f.callPath(t, http.MethodPut, "/v1/users/me/avatar", map[string]any{"media_id": id}); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if _, p = f.call(t, http.MethodGet, nil); p.Avatar == nil || p.Avatar.URL != want {
		t.Fatalf("refused changes kept the photo? %+v", p.Avatar)
	}

	if rec, p = f.callPath(t, http.MethodDelete, "/v1/users/me/avatar", nil); rec.Code != http.StatusOK || p.Avatar != nil {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
}
