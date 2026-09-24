// Package events defines the CloudEvents 1.0 envelope and publisher used for
// domain events. The Kafka publisher arrives in T-1.1.1.7.
package events

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event is a CloudEvents 1.0 envelope with a JSON payload.
type Event struct {
	SpecVersion     string    `json:"specversion"`
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	Type            string    `json:"type"`
	Time            time.Time `json:"time"`
	DataContentType string    `json:"datacontenttype"`
	Data            any       `json:"data"`
}

// New builds an event with a time-ordered UUIDv7 id.
func New(source, eventType string, at time.Time, data any) Event {
	return Event{
		SpecVersion:     "1.0",
		ID:              uuid.Must(uuid.NewV7()).String(),
		Source:          source,
		Type:            eventType,
		Time:            at.UTC(),
		DataContentType: "application/json",
		Data:            data,
	}
}

// Publisher delivers domain events to the event bus.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// LogPublisher writes events to a logger. Development only.
type LogPublisher struct {
	Logger *slog.Logger
}

// Publish implements Publisher.
func (p LogPublisher) Publish(ctx context.Context, e Event) error {
	p.Logger.InfoContext(ctx, "event published", "type", e.Type, "id", e.ID, "source", e.Source)
	return nil
}

// Recorder keeps published events in memory. Intended for tests.
type Recorder struct {
	mu     sync.Mutex
	events []Event
	Err    error // returned from Publish when set
}

// Publish implements Publisher.
func (r *Recorder) Publish(_ context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	r.events = append(r.events, e)
	return nil
}

// Events returns a copy of the recorded events.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}
