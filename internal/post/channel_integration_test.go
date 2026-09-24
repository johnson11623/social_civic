package post

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/platform/authn"
)

// Channels against a real, migrated PostgreSQL (TEST_DATABASE_URL), through
// the same middleware chain as cmd/api: authn → consent → handler.

var seedOnce sync.Once

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make test-integration`")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx := context.Background()
	seedOnce.Do(func() {
		units, err := boundary.LoadIEBCCSVFile("../../db/seed/iebc_2022_wards.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := boundary.Seed(ctx, pool, boundary.IEBC2022, units, boundary.ExpectedIEBC2022); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := pool.Exec(ctx, "TRUNCATE users, consents, channels, posts, post_likes, post_actors, post_counters, outbox RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("reset (is the database migrated?): %v", err)
	}
	return pool
}

type env struct {
	pool   *pgxpool.Pool
	router http.Handler
	tokens *identity.TokenIssuer
	cache  *fakeCache
}

type fakeCache struct {
	mu     sync.Mutex
	bumped []int32
	err    error
}

func (c *fakeCache) BumpWard(_ context.Context, ward int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bumped = append(c.bumped, ward)
	return c.err
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testPool(t)
	tokens, err := identity.NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &ChannelHandlers{Store: NewStore(pool), Logger: logger}
	cache := &fakeCache{}
	ph := &PostHandlers{Store: NewStore(pool), Cache: cache, Logger: logger}
	requireAuth := authn.Middleware(tokens, identity.Unauthenticated)
	requireConsent := identity.RequireConsent(identity.NewPostgresStore(pool), logger)
	r := chi.NewRouter()
	r.With(requireAuth).Get("/v1/channels", h.List)
	r.With(requireAuth).Get("/v1/channels/{channel_id}", h.Get)
	r.With(requireAuth, requireConsent).Post("/v1/channels", h.Create)
	r.With(requireAuth, requireConsent).Post("/v1/channels/{channel_id}/posts", ph.Create)
	r.With(requireAuth).Get("/v1/posts/{post_id}", ph.Get)
	r.With(requireAuth).Get("/v1/posts/{post_id}/replies", ph.Replies)
	r.With(requireAuth, requireConsent).Post("/v1/posts/{post_id}/likes", ph.Like)
	r.With(requireAuth, requireConsent).Delete("/v1/posts/{post_id}/likes", ph.Unlike)
	r.With(requireAuth, requireConsent).Post("/v1/posts/{post_id}/replies", ph.Reply)
	return &env{pool: pool, router: r, tokens: tokens, cache: cache}
}

var idSeq = 10000000

// user inserts an active, consenting user in ward and returns an access token.
func (e *env) user(t *testing.T, ward, constituency, county int) (int64, string) {
	t.Helper()
	idSeq++
	publicID := uuid.Must(uuid.NewV7())
	var id int64
	hash := bytes.Repeat([]byte{byte(idSeq % 251)}, 31)
	hash = append(hash, byte(idSeq/251))
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO users (public_id, national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
		VALUES ($1, $2, 'v1', 'Test', $3, $4, $5) RETURNING id`, publicID, hash, ward, constituency, county).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO consents (user_id, version, granted_at) VALUES ($1, $2, now())`, id, identity.CurrentConsentVersion); err != nil {
		t.Fatal(err)
	}
	pair, err := e.tokens.Issue(publicID.String(), identity.ScopeClaim{Ward: ward, Constituency: constituency, County: county})
	if err != nil {
		t.Fatal(err)
	}
	return id, pair.Access
}

func (e *env) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Accept-Language", "en")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct{ Code string }
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Code
}

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Kiamwangi (551), Gatundu South (111), Kiambu (22); Kitisuru is elsewhere.
const kiamwangi, gatunduSouth, kiambu = 551, 111, 22

func TestChannels_CreateInOwnWard(t *testing.T) {
	e := newEnv(t)
	userID, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)

	rec := e.do(t, "POST", "/v1/channels", tok, map[string]any{
		"name": "water-points", "description": "Focused discussion on water access in Kiamwangi", "category": "services",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var ch ChannelJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)
	if ch.WardID != kiamwangi || ch.Name != "water-points" || ch.Category != "services" || ch.State != "active" || ch.ReadOnly {
		t.Errorf("channel = %+v", ch)
	}

	// T-2.1.1.5 / T-2.1.1.6 — channel.created enqueued with the channel (feeds the audit log).
	var topic, payload string
	if err := e.pool.QueryRow(context.Background(), `SELECT topic, payload::text FROM outbox`).Scan(&topic, &payload); err != nil {
		t.Fatal(err)
	}
	if topic != "channel.created" || !strings.Contains(payload, `"ward_id": 551`) || !strings.Contains(payload, `"category": "services"`) {
		t.Errorf("event %s %s", topic, payload)
	}
	var creator int64
	_ = e.pool.QueryRow(context.Background(), `SELECT creator_id FROM channels WHERE name = 'water-points'`).Scan(&creator)
	if creator != userID {
		t.Errorf("creator_id = %d, want %d", creator, userID)
	}
}

func TestChannels_Rejections(t *testing.T) {
	e := newEnv(t)
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)

	// T-2.1.1.3 — not in another ward.
	if rec := e.do(t, "POST", "/v1/channels", tok, map[string]any{"name": "roads", "ward_id": 1400}); rec.Code != 403 || code(t, rec) != "not_member" {
		t.Errorf("other ward: %d %s", rec.Code, rec.Body)
	}
	// T-2.1.1.4 — duplicate name in ward.
	e.do(t, "POST", "/v1/channels", tok, map[string]any{"name": "youth-jobs"})
	if rec := e.do(t, "POST", "/v1/channels", tok, map[string]any{"name": "youth-jobs"}); rec.Code != 409 || code(t, rec) != "name_taken" {
		t.Errorf("duplicate: %d %s", rec.Code, rec.Body)
	}
	for name, body := range map[string]map[string]any{
		"uppercase":        {"name": "Water-Points"},
		"spaces":           {"name": "water points"},
		"double hyphen":    {"name": "water--points"},
		"trailing hyphen":  {"name": "water-"},
		"too short":        {"name": "w"},
		"too long":         {"name": strings.Repeat("a", 41)},
		"reserved":         {"name": "announcements"},
		"reserved general": {"name": "general"},
		"bad category":     {"name": "events", "category": "politics"},
		"long description": {"name": "events", "description": strings.Repeat("x", 141)},
	} {
		if rec := e.do(t, "POST", "/v1/channels", tok, body); rec.Code != 422 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := e.do(t, "POST", "/v1/channels", tok, map[string]any{"name": "x", "private": true}); rec.Code != 400 {
		t.Errorf("unknown field (no private channels): %d", rec.Code)
	}
	if n := count(t, e.pool, "SELECT count(*) FROM channels"); n != 1 {
		t.Errorf("channels = %d, want 1 (only youth-jobs)", n)
	}
	if n := count(t, e.pool, "SELECT count(*) FROM outbox"); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}
}

func TestChannels_AuthAndConsent(t *testing.T) {
	e := newEnv(t)
	userID, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)

	if rec := e.do(t, "POST", "/v1/channels", "", map[string]any{"name": "roads"}); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
	_, _ = e.pool.Exec(context.Background(), "UPDATE consents SET withdrawn_at = now() WHERE user_id = $1", userID)
	if rec := e.do(t, "POST", "/v1/channels", tok, map[string]any{"name": "roads"}); rec.Code != 403 || code(t, rec) != "consent_required" {
		t.Errorf("withdrawn consent: %d %s", rec.Code, rec.Body)
	}
	_, _ = e.pool.Exec(context.Background(), "UPDATE users SET state = 3 WHERE id = $1", userID)
	if rec := e.do(t, "GET", "/v1/channels", tok, nil); rec.Code != 401 {
		t.Errorf("erased user listing: %d", rec.Code)
	}
}

func TestChannels_ListAndGet(t *testing.T) {
	e := newEnv(t)
	if _, err := EnsureGeneralChannels(context.Background(), e.pool); err != nil {
		t.Fatal(err)
	}
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	e.user(t, kiamwangi, gatunduSouth, kiambu) // second ward member
	_, other := e.user(t, 1400, 280, 47)       // Kitisuru, Westlands, Nairobi City

	for _, n := range []string{"youth-jobs", "health", "water-points"} {
		e.do(t, "POST", "/v1/channels", tok, map[string]any{"name": n})
	}
	rec := e.do(t, "GET", "/v1/channels", tok, nil)
	var list struct {
		WardID int32         `json:"ward_id"`
		Items  []ChannelJSON `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	var names []string
	for _, c := range list.Items {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "general,health,water-points,youth-jobs" || list.WardID != kiamwangi {
		t.Errorf("ward channels = %v (ward %d)", names, list.WardID)
	}

	// Get with member count; another ward's member is refused.
	id := list.Items[1].ChannelID
	rec = e.do(t, "GET", "/v1/channels/"+id, tok, nil)
	var got ChannelJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got.Name != "health" || got.MemberCount == nil || *got.MemberCount != 2 {
		t.Errorf("get: %d %+v", rec.Code, got)
	}
	if rec := e.do(t, "GET", "/v1/channels/"+id, other, nil); rec.Code != 403 || code(t, rec) != "not_member" {
		t.Errorf("other ward get: %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{"not-a-uuid", uuid.NewString()} {
		if rec := e.do(t, "GET", "/v1/channels/"+bad, tok, nil); rec.Code != 404 {
			t.Errorf("get %s: %d", bad, rec.Code)
		}
	}
	// The other ward sees only its own #general.
	rec = e.do(t, "GET", "/v1/channels", other, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].Name != "general" {
		t.Errorf("other ward channels = %+v", list.Items)
	}
}

func TestEnsureGeneralChannels_OnePerWardIdempotent(t *testing.T) {
	e := newEnv(t)
	first, err := EnsureGeneralChannels(context.Background(), e.pool)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := EnsureGeneralChannels(context.Background(), e.pool)
	if first != 1450 || second != 0 {
		t.Errorf("created %d then %d, want 1450 then 0", first, second)
	}
	if n := count(t, e.pool, "SELECT count(*) FROM channels WHERE name = 'general' AND creator_id IS NULL"); n != 1450 {
		t.Errorf("#general channels = %d", n)
	}
}
