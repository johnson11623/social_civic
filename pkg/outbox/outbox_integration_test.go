package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/pkg/events"
)

// Runs against a real, migrated PostgreSQL when TEST_DATABASE_URL is set.
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
	if _, err := pool.Exec(context.Background(), "TRUNCATE outbox RESTART IDENTITY"); err != nil {
		t.Fatalf("reset outbox (is the database migrated?): %v", err)
	}
	return pool
}

// fakeProducer records batches; fail makes the next calls return an error.
type fakeProducer struct {
	mu      sync.Mutex
	batches [][]Message
	fail    int
}

func (p *fakeProducer) Produce(_ context.Context, msgs []Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail > 0 {
		p.fail--
		return errors.New("broker unavailable")
	}
	p.batches = append(p.batches, append([]Message(nil), msgs...))
	return nil
}

func (p *fakeProducer) all() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Message
	for _, b := range p.batches {
		out = append(out, b...)
	}
	return out
}

func enqueue(t *testing.T, pool *pgxpool.Pool, n int, commit bool) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for i := 0; i < n; i++ {
		e := events.New("test", "test.happened", time.Now(), map[string]int{"seq": i})
		if err := Enqueue(ctx, tx, "test.topic", "key-"+strconv.Itoa(i%3), e); err != nil {
			t.Fatal(err)
		}
	}
	if commit {
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func newRelay(pool *pgxpool.Pool, p Producer) *Relay {
	return &Relay{Pool: pool, Producer: p, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), BatchSize: 4, Interval: 10 * time.Millisecond}
}

func pending(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox WHERE published_at IS NULL").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRelayPublishesCommittedEventsInOrder(t *testing.T) {
	pool := testPool(t)
	enqueue(t, pool, 10, true)
	p := &fakeProducer{}
	r := newRelay(pool, p)

	total := 0
	for {
		n, err := r.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		if n > r.BatchSize {
			t.Fatalf("batch of %d exceeds BatchSize %d", n, r.BatchSize)
		}
		total += n
	}
	if total != 10 || pending(t, pool) != 0 {
		t.Fatalf("published %d, pending %d", total, pending(t, pool))
	}

	msgs := p.all()
	for i, m := range msgs {
		var e events.Event
		if err := json.Unmarshal(m.Value, &e); err != nil {
			t.Fatal(err)
		}
		if seq := int(e.Data.(map[string]any)["seq"].(float64)); seq != i {
			t.Errorf("message %d has seq %d: order not preserved", i, seq)
		}
		if m.Topic != "test.topic" || m.Key != "key-"+strconv.Itoa(i%3) || m.Headers["content-type"] != ContentType {
			t.Errorf("message %d = topic %s key %s headers %v", i, m.Topic, m.Key, m.Headers)
		}
	}
}

func TestRelayIgnoresUncommittedEvents(t *testing.T) {
	pool := testPool(t)
	enqueue(t, pool, 3, false) // rolled back
	p := &fakeProducer{}
	if n, err := newRelay(pool, p).RunOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
	if len(p.all()) != 0 {
		t.Error("rolled-back events were published")
	}
}

func TestRelayRetriesAfterPublishFailure(t *testing.T) {
	pool := testPool(t)
	enqueue(t, pool, 2, true)
	p := &fakeProducer{fail: 2}
	r := newRelay(pool, p)

	for i := 0; i < 2; i++ {
		if _, err := r.RunOnce(context.Background()); err == nil {
			t.Fatal("expected publish error")
		}
	}
	var attempts int
	var lastErr string
	if err := pool.QueryRow(context.Background(), "SELECT min(attempts), min(last_error) FROM outbox").Scan(&attempts, &lastErr); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || lastErr != "broker unavailable" || pending(t, pool) != 2 {
		t.Fatalf("after failures: attempts=%d last_error=%q pending=%d", attempts, lastErr, pending(t, pool))
	}

	if n, err := r.RunOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("recovery RunOnce = %d, %v", n, err)
	}
	var cleared int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox WHERE last_error IS NULL AND attempts = 3").Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if cleared != 2 || pending(t, pool) != 0 {
		t.Errorf("after recovery: cleared=%d pending=%d", cleared, pending(t, pool))
	}
}

func TestConcurrentRelaysNeverDoublePublish(t *testing.T) {
	pool := testPool(t)
	enqueue(t, pool, 200, true)
	p := &fakeProducer{}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := newRelay(pool, p)
			for {
				n, err := r.RunOnce(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				if n == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for _, m := range p.all() {
		var e events.Event
		_ = json.Unmarshal(m.Value, &e)
		if seen[e.ID] {
			t.Fatalf("event %s published twice", e.ID)
		}
		seen[e.ID] = true
	}
	if len(seen) != 200 || pending(t, pool) != 0 {
		t.Errorf("published %d unique, pending %d", len(seen), pending(t, pool))
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- newRelay(pool, &fakeProducer{}).Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestEnqueueRejectsMissingTopicOrKey(t *testing.T) {
	pool := testPool(t)
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		e := events.New("test", "x", time.Now(), nil)
		if Enqueue(context.Background(), tx, "", "k", e) == nil || Enqueue(context.Background(), tx, "t", "", e) == nil {
			t.Error("expected errors for empty topic/key")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
