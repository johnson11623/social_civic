package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/membership"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/post"
)

// EPIC 3.1 against a real, migrated PostgreSQL (TEST_DATABASE_URL).

const kiamwangi, gatunduSouth, kiambu = 551, 111, 22

var seedOnce sync.Once

type fakeCache struct {
	mu     sync.Mutex
	bumped []post.Scope
}

func (c *fakeCache) Bump(_ context.Context, s post.Scope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bumped = append(c.bumped, s)
	return nil
}

type env struct {
	pool    *pgxpool.Pool
	router  http.Handler
	tokens  *identity.TokenIssuer
	members *membership.Store
	cache   *fakeCache
	now     time.Time
}

func newEnv(t *testing.T) *env {
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
	if _, err := pool.Exec(ctx, `TRUNCATE users, consents, channels, posts, post_counters, reports, moderation_actions,
		appeals, role_assignments, outbox RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset (is the database migrated?): %v", err)
	}
	tokens, err := identity.NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &env{pool: pool, tokens: tokens, members: membership.NewStore(pool), cache: &fakeCache{}, now: time.Now()}
	members := &membership.Handlers{Store: e.members, Logger: logger, Now: time.Now}
	h := &Handlers{Pool: pool, Posts: post.NewStore(pool), Members: members, Cache: e.cache, Logger: logger,
		Now: func() time.Time { return e.now }}
	r := chi.NewRouter()
	auth := authn.Middleware(tokens, identity.Unauthenticated)
	registerRoutes(r, auth, h)
	e.router = r
	return e
}

var seq = 30000000

type person struct {
	membership.Member
	token string
}

func (e *env) user(t *testing.T, ward, constituency, county int) person {
	t.Helper()
	seq++
	publicID := uuid.Must(uuid.NewV7())
	hash := append(bytes.Repeat([]byte{byte(seq % 251)}, 31), byte(seq/251))
	var id int64
	if err := e.pool.QueryRow(context.Background(), `
		INSERT INTO users (public_id, national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
		VALUES ($1, $2, 'v1', $3, $4, $5, $6) RETURNING id`, publicID, hash, fmt.Sprintf("User %d", seq), ward, constituency, county).Scan(&id); err != nil {
		t.Fatal(err)
	}
	pair, err := e.tokens.Issue(publicID.String(), identity.ScopeClaim{Ward: ward, Constituency: constituency, County: county})
	if err != nil {
		t.Fatal(err)
	}
	return person{membership.Member{ID: id, PublicID: publicID, Ward: int32(ward), Constituency: int32(constituency), County: int32(county)}, pair.Access}
}

func (e *env) moderator(t *testing.T, role string, level int16, unit int32, ward, constituency, county int) person {
	t.Helper()
	p := e.user(t, ward, constituency, county)
	if _, err := e.members.Appoint(context.Background(), p.Member, role, level, unit, 0, nil); err != nil {
		t.Fatal(err)
	}
	return p
}

var channelSeq int

// post writes a post by author at level, straight to the database.
func (e *env) post(t *testing.T, author person, level int16) string {
	t.Helper()
	ctx := context.Background()
	channelSeq++
	var channelID int64
	if err := e.pool.QueryRow(ctx, `INSERT INTO channels (public_id, ward_id, name) VALUES ($1, $2, $3) RETURNING id`,
		uuid.Must(uuid.NewV7()), author.Ward, fmt.Sprintf("c%d", channelSeq)).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := e.pool.Exec(ctx, `
		INSERT INTO posts (public_id, channel_id, author_id, content, level, ward_id, constituency_id, county_id)
		VALUES ($1, $2, $3, 'Some post', $4, $5, $6, $7)`,
		id, channelID, author.ID, level, author.Ward, author.Constituency, author.County); err != nil {
		t.Fatal(err)
	}
	return id.String()
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

func (e *env) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// ---- Feature 3.1.1 ----------------------------------------------------------------

func TestReports_QueuedToLevelModerators(t *testing.T) {
	e := newEnv(t)
	author := e.user(t, kiamwangi, gatunduSouth, kiambu)
	reporter := e.user(t, kiamwangi, gatunduSouth, kiambu)
	wardPost := e.post(t, author, post.LevelWard)
	countyPost := e.post(t, author, post.LevelCounty)

	rec := e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": wardPost, "reason_code": "incitement", "details": "Calls for violence at the market"})
	var res ReportResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusCreated || res.State != "open" || res.Queue != "ward_mod" || res.ReportID == "" {
		t.Fatalf("report: %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": countyPost, "reason_code": "privacy"}).Body.Bytes(), &res)
	if res.Queue != "county_mod" {
		t.Errorf("county queue = %q", res.Queue)
	}
	// T-3.1.1.5 — post.reported on the bus.
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE topic = 'post.reported' AND payload->'data'->>'queue' = 'ward_mod'`); n != 1 {
		t.Errorf("post.reported events = %d", n)
	}
}

func TestReports_Validation(t *testing.T) {
	e := newEnv(t)
	author := e.user(t, kiamwangi, gatunduSouth, kiambu)
	reporter := e.user(t, kiamwangi, gatunduSouth, kiambu)
	elsewhere := e.user(t, 1366, 274, 47)
	p := e.post(t, author, post.LevelWard)

	// T-3.1.1.3 — only harms in the policy.
	for _, reason := range []string{"spam", "i_disagree", ""} {
		if rec := e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": p, "reason_code": reason}); rec.Code != 422 || code(rec) != "invalid_reason" {
			t.Errorf("reason %q: %d %s", reason, rec.Code, rec.Body)
		}
	}
	// T-3.1.1.4 — once per user.
	e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": p, "reason_code": "hate_speech"})
	if rec := e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": p, "reason_code": "privacy"}); rec.Code != 409 || code(rec) != "already_reported" {
		t.Errorf("second report: %d %s", rec.Code, rec.Body)
	}
	if n := e.count(t, "SELECT count(*) FROM reports"); n != 1 {
		t.Errorf("reports = %d", n)
	}
	// Only posts you can see.
	if rec := e.do(t, "POST", "/v1/reports", elsewhere.token, map[string]any{"post_id": p, "reason_code": "hate_speech"}); rec.Code != 403 {
		t.Errorf("out of scope: %d", rec.Code)
	}
	if rec := e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": uuid.NewString(), "reason_code": "hate_speech"}); rec.Code != 404 {
		t.Errorf("unknown post: %d", rec.Code)
	}
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'x'
	}
	other := e.user(t, kiamwangi, gatunduSouth, kiambu)
	if rec := e.do(t, "POST", "/v1/reports", other.token, map[string]any{"post_id": p, "reason_code": "privacy", "details": string(long)}); rec.Code != 422 {
		t.Errorf("long details: %d", rec.Code)
	}
}
