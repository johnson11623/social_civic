// Package outbox implements the transactional outbox: services Enqueue events
// inside their own database transaction, and a Relay publishes committed rows
// to the event bus. An event is published if and only if its transaction
// committed, and at least once (consumers dedupe on the CloudEvents id).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/pkg/events"
)

// ContentType is the CloudEvents structured-mode content type for Kafka.
const ContentType = "application/cloudevents+json"

// Message is one record to publish.
type Message struct {
	Topic   string
	Key     string
	Value   []byte
	Headers map[string]string
}

// Producer publishes a batch synchronously, returning only after the broker
// has acknowledged every message.
type Producer interface {
	Produce(ctx context.Context, msgs []Message) error
}

// Enqueue stores event e for topic in the caller's transaction.
func Enqueue(ctx context.Context, tx pgx.Tx, topic, key string, e events.Event) error {
	if topic == "" || key == "" {
		return errors.New("outbox: topic and key are required")
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("outbox: encode event: %w", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO outbox (topic, partition_key, event_id, payload) VALUES ($1, $2, $3, $4)`,
		topic, key, e.ID, payload)
	if err != nil {
		return fmt.Errorf("outbox: enqueue: %w", err)
	}
	return nil
}

// Relay moves committed outbox rows to the event bus in id order.
//
// Run a single relay per database for now: FOR UPDATE SKIP LOCKED makes
// several relays safe from double-publishing, but they could reorder events
// for the same key.
type Relay struct {
	Pool      *pgxpool.Pool
	Producer  Producer
	Logger    *slog.Logger
	BatchSize int           // default 100
	Interval  time.Duration // idle poll interval, default 100ms
	MaxDelay  time.Duration // cap on retry backoff, default 30s
}

// RunOnce publishes up to BatchSize pending rows and returns how many were
// published. On a publish failure it records the attempt and returns the error;
// the rows stay pending and are retried in the same order.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	batch := r.BatchSize
	if batch <= 0 {
		batch = 100
	}
	var published int
	var publishErr error
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, topic, partition_key, payload
			FROM outbox
			WHERE published_at IS NULL
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED`, batch)
		if err != nil {
			return err
		}
		var ids []int64
		var msgs []Message
		for rows.Next() {
			var id int64
			var m Message
			if err := rows.Scan(&id, &m.Topic, &m.Key, &m.Value); err != nil {
				rows.Close()
				return err
			}
			m.Headers = map[string]string{"content-type": ContentType}
			ids = append(ids, id)
			msgs = append(msgs, m)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		if publishErr = r.Producer.Produce(ctx, msgs); publishErr != nil {
			_, err := tx.Exec(ctx,
				`UPDATE outbox SET attempts = attempts + 1, last_error = $2 WHERE id = ANY($1)`,
				ids, truncate(publishErr.Error(), 1000))
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE outbox SET published_at = now(), attempts = attempts + 1, last_error = NULL WHERE id = ANY($1)`,
			ids); err != nil {
			return err
		}
		published = len(ids)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("outbox: relay: %w", err)
	}
	if publishErr != nil {
		return 0, fmt.Errorf("outbox: publish: %w", publishErr)
	}
	return published, nil
}

// Run relays until ctx is cancelled, backing off exponentially while the bus
// or database is failing.
func (r *Relay) Run(ctx context.Context) error {
	interval := r.Interval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	maxDelay := r.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	batch := r.BatchSize
	if batch <= 0 {
		batch = 100
	}
	delay := interval
	for {
		n, err := r.RunOnce(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			r.Logger.ErrorContext(ctx, "outbox relay failed; retrying", "err", err, "retry_in", delay)
			delay = min(delay*2, maxDelay)
		case n == batch:
			delay = interval
			continue // more rows are waiting
		default:
			if n > 0 {
				r.Logger.DebugContext(ctx, "outbox relayed", "events", n)
			}
			delay = interval
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
