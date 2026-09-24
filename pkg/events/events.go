// Package events defines the CloudEvents 1.0 envelope for domain events.
// Events are published through the transactional outbox (pkg/outbox).
package events

import (
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
