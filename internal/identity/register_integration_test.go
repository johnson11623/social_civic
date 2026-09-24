package identity

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests run against a real, migrated PostgreSQL when
// TEST_DATABASE_URL is set (see `make test-integration`); otherwise they skip.
func integrationPool(t *testing.T) *pgxpool.Pool {
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
	if _, err := pool.Exec(context.Background(), "TRUNCATE users, consents, verification_attempts, outbox, otp_challenges, refresh_tokens, erasure_requests, erasure_steps RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("reset tables (is the database migrated?): %v", err)
	}
	return pool
}

func newIntegrationFixture(t *testing.T) (*fixture, *pgxpool.Pool) {
	t.Helper()
	pool := integrationPool(t)
	f := newFixture(t)
	store := NewPostgresStore(pool)
	f.handler.Store = store
	f.handler.Sessions = store
	return f, pool
}

func count(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRegisterIntegration_PersistsUserAndConsent(t *testing.T) {
	f, pool := newIntegrationFixture(t)

	rec := f.post(t, validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	var (
		hash                       []byte
		keyVersion, name, lang     string
		ward, constituency, county int
		state                      int
	)
	err := pool.QueryRow(context.Background(), `
		SELECT national_id_hash, national_id_key_version, display_name, preferred_lang,
		       ward_id, constituency_id, county_id, state
		FROM users`).Scan(&hash, &keyVersion, &name, &lang, &ward, &constituency, &county, &state)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 32 || keyVersion != "v7" || name != "Wanjiku M." || lang != "sw" || state != 1 {
		t.Errorf("user row: hash=%d bytes key=%s name=%s lang=%s state=%d", len(hash), keyVersion, name, lang, state)
	}
	if ward != 551 || constituency != 111 || county != 22 {
		t.Errorf("scope = %d/%d/%d", ward, constituency, county)
	}

	// The raw national ID appears nowhere in the identity tables.
	if n := count(t, pool, `SELECT count(*) FROM users u WHERE u::text LIKE '%12345678%'`); n != 0 {
		t.Error("raw national ID found in users")
	}
	if n := count(t, pool, `SELECT count(*) FROM consents c WHERE c::text LIKE '%12345678%'`); n != 0 {
		t.Error("raw national ID found in consents")
	}

	var version string
	var withdrawn *string
	var ipHash []byte
	err = pool.QueryRow(context.Background(),
		`SELECT version, withdrawn_at::text, ip_hash FROM consents`).Scan(&version, &withdrawn, &ipHash)
	if err != nil {
		t.Fatal(err)
	}
	if version != "2026-01" || withdrawn != nil || len(ipHash) != 32 {
		t.Errorf("consent: version=%s withdrawn=%v ip_hash=%d bytes", version, withdrawn, len(ipHash))
	}

	// user.registered was enqueued in the same transaction, keyed by user id.
	var topic, key, payload string
	var userID int64
	var published *string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(context.Background(),
		`SELECT topic, partition_key, payload::text, published_at::text FROM outbox`).Scan(&topic, &key, &payload, &published)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "user.registered" || key != strconv.FormatInt(userID, 10) || published != nil {
		t.Errorf("outbox row: topic=%s key=%s published=%v", topic, key, published)
	}
	for _, want := range []string{`"type": "user.registered"`, `"specversion": "1.0"`, `"ward_id": 551`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload missing %s: %s", want, payload)
		}
	}
	if strings.Contains(payload, "12345678") || strings.Contains(payload, "national_id") {
		t.Errorf("outbox payload leaks national ID: %s", payload)
	}
}

func TestRegisterIntegration_DuplicateIDReturns409(t *testing.T) {
	f, pool := newIntegrationFixture(t)

	if rec := f.post(t, validBody()); rec.Code != http.StatusCreated {
		t.Fatalf("first registration: %d %s", rec.Code, rec.Body)
	}
	second := validBody()
	second["display_name"] = "Someone Else"
	rec := f.post(t, second)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("id_already_registered")) {
		t.Fatalf("second registration: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, pool, "SELECT count(*) FROM users"); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM consents"); n != 1 {
		t.Errorf("consents = %d, want 1 (failed insert must roll back)", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM outbox"); n != 1 {
		t.Errorf("outbox = %d, want 1 (a rolled-back registration must not emit an event)", n)
	}
}

func TestRegisterIntegration_ConcurrentDuplicatesOnlyOneWins(t *testing.T) {
	f, pool := newIntegrationFixture(t)

	const n = 10
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() { codes <- f.post(t, validBody()).Code }()
	}
	created, conflicts := 0, 0
	for i := 0; i < n; i++ {
		switch <-codes {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		}
	}
	if created != 1 || conflicts != n-1 {
		t.Errorf("created=%d conflicts=%d, want 1 and %d", created, conflicts, n-1)
	}
	if got := count(t, pool, "SELECT count(*) FROM users"); got != 1 {
		t.Errorf("users = %d, want 1", got)
	}
	if got := count(t, pool, "SELECT count(*) FROM outbox"); got != 1 {
		t.Errorf("outbox = %d, want 1", got)
	}
}

func TestRegisterIntegration_RejectionsWriteNothing(t *testing.T) {
	f, pool := newIntegrationFixture(t)

	noConsent := validBody()
	noConsent["consent_granted"] = false
	badID := validBody()
	badID["national_id"] = "1234567"

	for _, body := range []map[string]any{noConsent, badID} {
		if rec := f.post(t, body); rec.Code < 400 {
			t.Fatalf("expected rejection, got %d", rec.Code)
		}
	}
	for _, table := range []string{"users", "consents", "verification_attempts", "outbox"} {
		if n := count(t, pool, "SELECT count(*) FROM "+table); n != 0 {
			t.Errorf("%s has %d rows after rejected requests", table, n)
		}
	}
}
