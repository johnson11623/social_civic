package sms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Africa's Talking bulk SMS endpoints. The "sandbox" username goes to the
// sandbox, which delivers only to the simulator.
const (
	atLiveURL    = "https://api.africastalking.com/version1/messaging"
	atSandboxURL = "https://api.sandbox.africastalking.com/version1/messaging"
)

// AfricasTalking sends SMS through Africa's Talking.
type AfricasTalking struct {
	Username string
	APIKey   string
	SenderID string // registered alphanumeric sender ID; empty uses the shared short code
	Endpoint string // overrides the URL (tests); empty picks live or sandbox
	Client   *http.Client
	Logger   *slog.Logger
}

// AfricasTalkingFromEnv reads AFRICASTALKING_USERNAME, _API_KEY and
// _SENDER_ID. ok is false when no API key is set.
func AfricasTalkingFromEnv(logger *slog.Logger) (s *AfricasTalking, ok bool) {
	key := strings.TrimSpace(os.Getenv("AFRICASTALKING_API_KEY"))
	if key == "" {
		return nil, false
	}
	return &AfricasTalking{
		Username: strings.TrimSpace(os.Getenv("AFRICASTALKING_USERNAME")),
		APIKey:   key,
		SenderID: strings.TrimSpace(os.Getenv("AFRICASTALKING_SENDER_ID")),
		Client:   &http.Client{Timeout: 15 * time.Second},
		Logger:   logger,
	}, true
}

// atResponse is the body of a send.
type atResponse struct {
	SMSMessageData struct {
		Message    string `json:"Message"`
		Recipients []struct {
			StatusCode int    `json:"statusCode"`
			Number     string `json:"number"`
			Status     string `json:"status"`
			Cost       string `json:"cost"`
			MessageID  string `json:"messageId"`
		} `json:"Recipients"`
	} `json:"SMSMessageData"`
}

// ErrRejected is returned when Africa's Talking refuses the message
// (invalid number, sender ID or balance, blacklisted recipient...).
var ErrRejected = errors.New("sms rejected")

// Send implements Sender.
func (s *AfricasTalking) Send(ctx context.Context, to, text string) error {
	form := url.Values{"username": {s.Username}, "to": {to}, "message": {text}}
	if s.SenderID != "" {
		form.Set("from", s.SenderID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("apiKey", s.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("africastalking: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		// The body is plain text on auth errors ("The supplied authentication is invalid").
		return fmt.Errorf("africastalking: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out atResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("africastalking: unreadable response: %w", err)
	}
	if len(out.SMSMessageData.Recipients) == 0 {
		return fmt.Errorf("%w: %s", ErrRejected, out.SMSMessageData.Message)
	}
	r := out.SMSMessageData.Recipients[0]
	// 100 Processed, 101 Sent, 102 Queued; anything else is a refusal.
	if r.StatusCode < 100 || r.StatusCode > 102 {
		return fmt.Errorf("%w: %d %s", ErrRejected, r.StatusCode, r.Status)
	}
	if s.Logger != nil {
		s.Logger.InfoContext(ctx, "sms sent", "to", Mask(to), "status", r.Status, "cost", r.Cost, "message_id", r.MessageID)
	}
	return nil
}

func (s *AfricasTalking) endpoint() string {
	switch {
	case s.Endpoint != "":
		return s.Endpoint
	case s.Username == "sandbox":
		return atSandboxURL
	default:
		return atLiveURL
	}
}

// Async sends in the background, so a slow carrier doesn't hold the request
// (and response timing doesn't vary with delivery). Failures are logged.
type Async struct {
	Sender  Sender
	Logger  *slog.Logger
	Timeout time.Duration
}

// Send implements Sender; it returns at once.
func (a Async) Send(ctx context.Context, to, text string) error {
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		if err := a.Sender.Send(ctx, to, text); err != nil {
			a.Logger.ErrorContext(ctx, "sms failed", "to", Mask(to), "err", err)
		}
	}()
	return nil
}

// Mask hides the middle of a phone number for logs.
func Mask(to string) string {
	if len(to) > 7 {
		return to[:7] + "***" + to[len(to)-2:]
	}
	return to
}
