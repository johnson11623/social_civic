// Package sms sends text messages. The carrier adapter (Africa's Talking /
// Safaricom bulk SMS, zero-rated) arrives with EPIC 4.5; until then the dev
// sender writes messages to the log.
package sms

import (
	"context"
	"log/slog"
	"sync"
)

// Sender delivers one SMS to an E.164 number.
type Sender interface {
	Send(ctx context.Context, to, text string) error
}

// DevLogSender logs messages instead of sending them. It prints login codes,
// so it must never run outside local development.
type DevLogSender struct {
	Logger *slog.Logger
}

// Send implements Sender.
func (s DevLogSender) Send(ctx context.Context, to, text string) error {
	masked := to
	if len(to) > 7 {
		masked = to[:7] + "***" + to[len(to)-2:]
	}
	s.Logger.WarnContext(ctx, "DEV SMS (not sent)", "to", masked, "text", text)
	return nil
}

// Recorder keeps sent messages in memory. Intended for tests.
type Recorder struct {
	mu   sync.Mutex
	sent []Message
	Err  error
}

// Message is a recorded SMS.
type Message struct{ To, Text string }

// Send implements Sender.
func (r *Recorder) Send(_ context.Context, to, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	r.sent = append(r.sent, Message{To: to, Text: text})
	return nil
}

// Sent returns a copy of the recorded messages.
func (r *Recorder) Sent() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.sent...)
}
