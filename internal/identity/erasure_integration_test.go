package identity

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/johnson11623/social_civic/internal/membership"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/johnson11623/social_civic/internal/platform/authn"
)

// Story 1.1.3.2 against a real PostgreSQL.

type erasureFixture struct {
	*consentFixture
	processor *ErasureProcessor
}

func newErasureFixture(t *testing.T) *erasureFixture {
	t.Helper()
	c := newConsentFixture(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	erasure := &ErasureHandlers{Store: NewPostgresStore(c.pool), Logger: logger, Now: c.clock}

	r := chi.NewRouter()
	r.Mount("/", c.router)
	r.With(authn.Middleware(c.auth.Tokens, Unauthenticated)).Post("/v1/users/me/erasure", erasure.Request)
	c.router = r
	return &erasureFixture{
		consentFixture: c,
		processor:      &ErasureProcessor{Pool: c.pool, Logger: logger, Now: c.clock, Steps: workerSteps()},
	}
}

func (e *erasureFixture) request(t *testing.T) (ErasureResponse, int) {
	t.Helper()
	rec := e.call(t, "/v1/users/me/erasure", e.access, map[string]string{"reason": "No longer wish to participate"})
	var resp ErasureResponse
	_ = jsonDecode(rec.Body.Bytes(), &resp)
	return resp, rec.Code
}

func (e *erasureFixture) drain(t *testing.T) {
	t.Helper()
	for i := 0; i < 20; i++ {
		found, err := e.processor.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			return
		}
	}
	t.Fatal("processor did not drain")
}

func TestErasureIntegration_RequestAndComplete(t *testing.T) {
	e := newErasureFixture(t)
	ctx := context.Background()

	// T-1.1.3.5: 202 with ack_by (48h) and completion_by (30d).
	resp, code := e.request(t)
	if code != http.StatusAccepted || resp.State != "received" {
		t.Fatalf("request: %d %+v", code, resp)
	}
	if !resp.AckBy.Equal(fixedNow.Add(48*time.Hour)) || !resp.CompletionBy.Equal(fixedNow.Add(30*24*time.Hour)) {
		t.Errorf("deadlines: ack_by %s completion_by %s", resp.AckBy, resp.CompletionBy)
	}
	if len(resp.Retained) == 0 {
		t.Error("the user must be told what is retained")
	}
	// Sessions end immediately; T-1.1.3.6: erasure.requested enqueued.
	if n := count(t, e.pool, "SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL"); n != 0 {
		t.Errorf("%d sessions still active after the request", n)
	}
	if n := count(t, e.pool, "SELECT count(*) FROM outbox WHERE topic = 'erasure.requested'"); n != 1 {
		t.Errorf("erasure.requested events = %d", n)
	}
	// Second request → 409 erasure_in_progress.
	if _, code := e.request(t); code != http.StatusConflict {
		t.Errorf("second request: %d, want 409", code)
	}

	// T-1.1.3.7: the saga anonymizes the user.
	var oldHash []byte
	_ = e.pool.QueryRow(ctx, "SELECT national_id_hash FROM users").Scan(&oldHash)
	_, _ = e.pool.Exec(ctx, "INSERT INTO verification_attempts (id_hash, outcome) VALUES ($1, 1)", oldHash)
	e.drain(t)

	var (
		name, keyVersion string
		hash, phone      []byte
		state            int
		ward             int
	)
	_ = e.pool.QueryRow(ctx, "SELECT display_name, national_id_hash, national_id_key_version, msisdn_ciphertext, state, ward_id FROM users").
		Scan(&name, &hash, &keyVersion, &phone, &state, &ward)
	if name != "[deleted user]" || phone != nil || state != UserErased || keyVersion != "erased" || string(hash) == string(oldHash) {
		t.Errorf("user after erasure: name=%q phone=%v state=%d key=%s hashChanged=%v", name, phone, state, keyVersion, string(hash) != string(oldHash))
	}
	if ward != 551 {
		t.Error("administrative scope is retained for aggregates")
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM verification_attempts":                       0,
		"SELECT count(*) FROM otp_challenges":                              0,
		"SELECT count(*) FROM consents WHERE ip_hash IS NOT NULL":          0,
		"SELECT count(*) FROM consents":                                    1, // lawful-basis evidence kept
		"SELECT count(*) FROM erasure_requests WHERE state = 2":            1,
		"SELECT count(*) FROM dpo_incomplete_erasures":                     0,
		"SELECT count(*) FROM outbox WHERE topic = 'user.erased'":          1,
		"SELECT count(*) FROM users WHERE msisdn_hash IS NOT NULL":         0,
		"SELECT count(*) FROM erasure_steps WHERE state = 2":               len(ErasureSteps),
		"SELECT count(*) FROM erasure_requests WHERE completed_at IS NULL": 0,
	} {
		if n := count(t, e.pool, q); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}
	// T-1.1.3.8: completion records what was retained.
	var retained []string
	var payload string
	_ = e.pool.QueryRow(ctx, "SELECT retained_fields FROM erasure_requests").Scan(&retained)
	_ = e.pool.QueryRow(ctx, "SELECT payload::text FROM outbox WHERE topic = 'user.erased'").Scan(&payload)
	if len(retained) != len(RetainedAfterErasure) || !strings.Contains(payload, "retained_fields") || strings.Contains(payload, "+254") {
		t.Errorf("retained=%v payload=%s", retained, payload)
	}

	// After erasure: consent-gated features are closed, no login code is sent,
	// and the same national ID can register afresh.
	if code := e.post(t); code != http.StatusForbidden {
		t.Errorf("erased user posting = %d", code)
	}
	before := len(e.sms.Sent())
	e.do(t, e.auth.RequestOTP, map[string]string{"national_id": "12345678"})
	if len(e.sms.Sent()) != before {
		t.Error("login code sent for an erased account")
	}
	if rec := e.authFixture.post(t, validBody()); rec.Code != http.StatusCreated {
		t.Errorf("re-registration after erasure: %d %s", rec.Code, rec.Body)
	}
}

func TestErasureIntegration_FailedStepShowsOnDPOView(t *testing.T) {
	e := newErasureFixture(t)
	ctx := context.Background()
	if _, code := e.request(t); code != http.StatusAccepted {
		t.Fatal(code)
	}
	// T-1.1.3.9: simulate a partial failure. The step writes, then fails:
	// its writes must roll back (savepoint), and after the retry limit the
	// request must surface on the DPO view.
	e.processor.Steps = workerSteps()
	e.processor.Steps[StepIdentityAnonymize] = func(ctx context.Context, tx pgx.Tx, userID int64) error {
		if _, err := tx.Exec(ctx, "UPDATE users SET display_name = 'half-done' WHERE id = $1", userID); err != nil {
			return err
		}
		return errors.New("membership service unavailable")
	}
	e.drain(t)

	if n := count(t, e.pool, "SELECT count(*) FROM users WHERE display_name = 'half-done'"); n != 0 {
		t.Error("a failed step's writes were committed")
	}
	var attempts int
	_ = e.pool.QueryRow(ctx, "SELECT attempts FROM erasure_steps WHERE step = $1", StepIdentityAnonymize).Scan(&attempts)
	if attempts != ErasureStepMaxAttempts {
		t.Errorf("attempts = %d, want %d", attempts, ErasureStepMaxAttempts)
	}

	// "Overdue" is judged by the database clock; align the request with it.
	_, _ = e.pool.Exec(ctx, "UPDATE erasure_requests SET requested_at = now(), ack_by = now() + interval '48 hours', completion_by = now() + interval '30 days'")
	rows, err := NewPostgresStore(e.pool).ListIncompleteErasures(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("DPO view = %+v, %v", rows, err)
	}
	r := rows[0]
	if r.State != 3 || r.Overdue || len(r.FailedSteps) != 1 || r.FailedSteps[0] != StepIdentityAnonymize ||
		!strings.Contains(r.LastError, "membership service unavailable") {
		t.Errorf("DPO row = %+v", r)
	}
	// Past its 30-day deadline it is flagged overdue.
	_, _ = e.pool.Exec(ctx, "UPDATE erasure_requests SET requested_at = now() - interval '40 days', ack_by = now() - interval '38 days', completion_by = now() - interval '10 days'")
	rows, _ = NewPostgresStore(e.pool).ListIncompleteErasures(ctx)
	if !rows[0].Overdue {
		t.Error("request past completion_by not flagged overdue")
	}
	// A failed request still blocks a duplicate request.
	if _, code := e.request(t); code != http.StatusConflict {
		t.Errorf("request while failed: %d, want 409", code)
	}
}

func TestErasureIntegration_UnregisteredStepFails(t *testing.T) {
	e := newErasureFixture(t)
	e.request(t)
	e.processor.Steps = map[string]StepFunc{} // step exists in DB, no implementation
	e.drain(t)
	rows, _ := NewPostgresStore(e.pool).ListIncompleteErasures(context.Background())
	if len(rows) != 1 || !strings.Contains(rows[0].LastError, "no implementation") {
		t.Errorf("DPO view = %+v", rows)
	}
}

func TestErasureIntegration_ConcurrentProcessorsRunStepOnce(t *testing.T) {
	e := newErasureFixture(t)
	e.request(t)
	var runs atomic.Int32
	e.processor.Steps = workerSteps()
	e.processor.Steps[StepIdentityAnonymize] = func(ctx context.Context, tx pgx.Tx, userID int64) error {
		runs.Add(1)
		time.Sleep(50 * time.Millisecond)
		return anonymizeUser(ctx, tx, userID)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = e.processor.RunOnce(context.Background()) }()
	}
	wg.Wait()
	e.drain(t) // whatever steps the racing processors left
	if n := runs.Load(); n != 1 {
		t.Errorf("step ran %d times, want 1", n)
	}
	if n := count(t, e.pool, "SELECT count(*) FROM outbox WHERE topic = 'user.erased'"); n != 1 {
		t.Errorf("user.erased events = %d", n)
	}
}

func TestErasureIntegration_ReasonTooLong(t *testing.T) {
	e := newErasureFixture(t)
	rec := e.call(t, "/v1/users/me/erasure", e.access, map[string]string{"reason": strings.Repeat("x", 501)})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d", rec.Code)
	}
	if n := count(t, e.pool, "SELECT count(*) FROM erasure_requests"); n != 0 {
		t.Error("rejected request was stored")
	}
}

func jsonDecode(b []byte, v any) error { return json.Unmarshal(b, v) }

// workerSteps mirrors cmd/worker: identity's steps plus membership's.
func workerSteps() map[string]StepFunc {
	steps := DefaultErasureSteps()
	steps[StepMembershipRevoke] = membership.RevokeRolesStep
	return steps
}
