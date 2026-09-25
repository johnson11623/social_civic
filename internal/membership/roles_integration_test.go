package membership

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/platform/authn"
)

// Feature 1.2.2 against a real, migrated PostgreSQL (TEST_DATABASE_URL).

var seedOnce sync.Once

type env struct {
	pool   *pgxpool.Pool
	router http.Handler
	tokens *identity.TokenIssuer
	store  *Store
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
	if _, err := pool.Exec(ctx, "TRUNCATE users, consents, role_assignments, outbox RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("reset (is the database migrated?): %v", err)
	}
	tokens, err := identity.NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	h := &Handlers{Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
	r := chi.NewRouter()
	auth := authn.Middleware(tokens, identity.Unauthenticated)
	r.With(auth).Get("/v1/users/me/roles", h.MyRoles)
	r.With(auth).Post("/v1/moderators", h.Appoint)
	return &env{pool: pool, router: r, tokens: tokens, store: store}
}

var seq = 20000000

func (e *env) user(t *testing.T, ward, constituency, county int) (Member, string) {
	t.Helper()
	seq++
	publicID := uuid.Must(uuid.NewV7())
	hash := append(bytes.Repeat([]byte{byte(seq % 251)}, 31), byte(seq/251))
	var id int64
	if err := e.pool.QueryRow(context.Background(), `
		INSERT INTO users (public_id, national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
		VALUES ($1, $2, 'v1', 'Test', $3, $4, $5) RETURNING id`, publicID, hash, ward, constituency, county).Scan(&id); err != nil {
		t.Fatal(err)
	}
	pair, err := e.tokens.Issue(publicID.String(), identity.ScopeClaim{Ward: ward, Constituency: constituency, County: county})
	if err != nil {
		t.Fatal(err)
	}
	return Member{ID: id, PublicID: publicID, Ward: int32(ward), Constituency: int32(constituency), County: int32(county)}, pair.Access
}

func (e *env) sysadmin(t *testing.T) string {
	t.Helper()
	m, tok := e.user(t, 1366, 274, 47)
	if _, err := e.store.Appoint(context.Background(), m, RoleSysadmin, 0, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	return tok
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

const kiamwangi, gatunduSouth, kiambu = 551, 111, 22

func TestAppoint_SysadminOnly(t *testing.T) {
	e := newEnv(t)
	resident, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	rec := e.do(t, "POST", "/v1/moderators", tok, map[string]any{"user_id": resident.PublicID, "role": RoleWardMod, "unit_code": kiamwangi})
	if rec.Code != http.StatusForbidden || code(rec) != "insufficient_authority" {
		t.Errorf("non-sysadmin: %d %s", rec.Code, rec.Body)
	}
}

func TestAppoint_WardModeratorWithTerm(t *testing.T) {
	e := newEnv(t)
	admin := e.sysadmin(t)
	resident, residentTok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	term := time.Now().Add(365 * 24 * time.Hour).UTC().Truncate(time.Second)

	rec := e.do(t, "POST", "/v1/moderators", admin, map[string]any{
		"user_id": resident.PublicID, "role": RoleWardMod, "unit_code": kiamwangi, "term_end": term,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("appoint: %d %s", rec.Code, rec.Body)
	}
	// T-1.2.2.3/5 — moderator.appointed, carrying the term.
	var payload string
	if err := e.pool.QueryRow(context.Background(), `SELECT payload::text FROM outbox WHERE topic = 'moderator.appointed' AND payload->'data'->>'role' = 'ward_mod'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"unit_code": 551`) || !strings.Contains(payload, `"term_end": "`+term.Format(time.RFC3339)) {
		t.Errorf("event = %s", payload)
	}

	var mine struct{ Items []AssignmentJSON }
	_ = json.Unmarshal(e.do(t, "GET", "/v1/users/me/roles", residentTok, nil).Body.Bytes(), &mine)
	if len(mine.Items) != 1 || mine.Items[0].Role != RoleWardMod || mine.Items[0].Level != 1 || mine.Items[0].UnitCode != kiamwangi {
		t.Errorf("roles = %+v", mine.Items)
	}

	// T-1.2.2.4 — one active role per (user, group), whatever the role.
	if rec := e.do(t, "POST", "/v1/moderators", admin, map[string]any{"user_id": resident.PublicID, "role": RoleWardMod, "unit_code": kiamwangi}); rec.Code != http.StatusConflict || code(rec) != "already_assigned" {
		t.Errorf("second appointment: %d %s", rec.Code, rec.Body)
	}
	// A different group is fine: the same person can moderate their county too.
	if rec := e.do(t, "POST", "/v1/moderators", admin, map[string]any{"user_id": resident.PublicID, "role": RoleCountyMod, "unit_code": kiambu}); rec.Code != http.StatusCreated {
		t.Errorf("county role: %d %s", rec.Code, rec.Body)
	}
}

func TestAppoint_Validation(t *testing.T) {
	e := newEnv(t)
	admin := e.sysadmin(t)
	resident, _ := e.user(t, kiamwangi, gatunduSouth, kiambu)
	cases := []struct {
		body   map[string]any
		status int
		code   string
	}{
		{map[string]any{"user_id": resident.PublicID, "role": "king", "unit_code": kiamwangi}, 422, "invalid_role"},
		{map[string]any{"user_id": resident.PublicID, "role": RoleDPO}, 422, "invalid_role"}, // platform roles: CLI only
		{map[string]any{"user_id": resident.PublicID, "role": RoleWardMod, "unit_code": 552}, 422, "not_in_group"},
		{map[string]any{"user_id": resident.PublicID, "role": RoleConstMod, "unit_code": 274}, 422, "not_in_group"},
		{map[string]any{"user_id": uuid.NewString(), "role": RoleWardMod, "unit_code": kiamwangi}, 404, "user_not_found"},
		{map[string]any{"user_id": resident.PublicID, "role": RoleWardMod, "unit_code": kiamwangi, "term_end": time.Now().Add(-time.Hour)}, 422, "invalid_term"},
	}
	for _, c := range cases {
		if rec := e.do(t, "POST", "/v1/moderators", admin, c.body); rec.Code != c.status || code(rec) != c.code {
			t.Errorf("%v: %d %s", c.body, rec.Code, rec.Body)
		}
	}
	// National moderators need no unit.
	if rec := e.do(t, "POST", "/v1/moderators", admin, map[string]any{"user_id": resident.PublicID, "role": RoleNatMod}); rec.Code != http.StatusCreated {
		t.Errorf("nat_mod: %d %s", rec.Code, rec.Body)
	}
}

func TestRoles_ExpiredTermsAndErasure(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m, _ := e.user(t, kiamwangi, gatunduSouth, kiambu)
	if _, err := e.store.Appoint(ctx, m, RoleWardMod, 1, kiamwangi, 0, nil); err != nil {
		t.Fatal(err)
	}
	soon := time.Now().Add(time.Hour)
	if _, err := e.store.Appoint(ctx, m, RoleCountyMod, 3, kiambu, 0, &soon); err != nil {
		t.Fatal(err)
	}
	// Terms end on their own.
	if _, err := e.pool.Exec(ctx, `UPDATE role_assignments SET appointed_at = now() - interval '2 days', term_end = now() - interval '1 day' WHERE role_code = 'county_mod'`); err != nil {
		t.Fatal(err)
	}
	roles, err := e.store.Roles(ctx, m.ID)
	if err != nil || len(roles) != 1 || roles[0].Role != RoleWardMod {
		t.Fatalf("roles = %+v, %v", roles, err)
	}
	// The erasure step ends every role.
	if err := pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error { return RevokeRolesStep(ctx, tx, m.ID) }); err != nil {
		t.Fatal(err)
	}
	if roles, _ := e.store.Roles(ctx, m.ID); len(roles) != 0 {
		t.Errorf("after erasure: %+v", roles)
	}
}
