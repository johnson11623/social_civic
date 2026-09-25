package kafka

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	kgo "github.com/segmentio/kafka-go"

	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// End-to-end: outbox row → relay → Kafka → consumer. Needs TEST_DATABASE_URL
// and KAFKA_BROKERS (`make test-integration` provides both).
func env(t *testing.T) (*pgxpool.Pool, []string) {
	t.Helper()
	dbURL, brokers := os.Getenv("TEST_DATABASE_URL"), os.Getenv("KAFKA_BROKERS")
	if dbURL == "" || brokers == "" {
		t.Skip("TEST_DATABASE_URL and KAFKA_BROKERS not set; run `make test-integration`")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), "TRUNCATE outbox RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
	return pool, strings.Split(brokers, ",")
}

func TestEnsureTopicsIsIdempotent(t *testing.T) {
	_, brokers := env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	topic := "test.ensure." + uuid.NewString()
	t.Cleanup(func() { deleteTopic(t, brokers[0], topic) })
	for i := 0; i < 2; i++ {
		if err := EnsureTopics(ctx, brokers[0], []Topic{{Name: topic, Partitions: 3, ReplicationFactor: 1}}); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	conn, err := kgo.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	parts, err := conn.ReadPartitions(topic)
	if err != nil || len(parts) != 3 {
		t.Fatalf("partitions = %d, %v", len(parts), err)
	}
}

func TestProducerRefusesUnknownTopic(t *testing.T) {
	_, brokers := env(t)
	p := NewProducer(brokers)
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := p.Produce(ctx, []outbox.Message{{Topic: "test.missing." + uuid.NewString(), Key: "k", Value: []byte("{}")}})
	if err == nil {
		t.Fatal("expected an error: topics must not be auto-created")
	}
}

func TestOutboxToKafkaEndToEnd(t *testing.T) {
	pool, brokers := env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	topic := "test.user.registered." + uuid.NewString()
	t.Cleanup(func() { deleteTopic(t, brokers[0], topic) })
	if err := EnsureTopics(ctx, brokers[0], []Topic{{Name: topic, Partitions: 6, ReplicationFactor: 1}}); err != nil {
		t.Fatal(err)
	}

	producer := NewProducer(brokers)
	defer producer.Close()
	relay := &outbox.Relay{Pool: pool, Producer: producer, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	relayCtx, stopRelay := context.WithCancel(ctx)
	defer stopRelay()
	go func() { _ = relay.Run(relayCtx) }()

	// The key decides the partition; read every partition.
	readers := make([]*kgo.Reader, 6)
	got := make(chan kgo.Message, 16)
	for i := range readers {
		readers[i] = kgo.NewReader(kgo.ReaderConfig{Brokers: brokers, Topic: topic, Partition: i, MinBytes: 1, MaxBytes: 1 << 20, MaxWait: 50 * time.Millisecond})
		defer readers[i].Close()
		go func(r *kgo.Reader) {
			for {
				m, err := r.ReadMessage(ctx)
				if err != nil {
					return
				}
				got <- m
			}
		}(readers[i])
	}

	publish := func(key string, data any) (events.Event, time.Duration, kgo.Message) {
		t.Helper()
		evt := events.New("identity", events.TopicUserRegistered, time.Now(), data)
		start := time.Now()
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return outbox.Enqueue(ctx, tx, topic, key, evt)
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case m := <-got:
			return evt, time.Since(start), m
		case <-time.After(15 * time.Second):
			t.Fatal("event not received from Kafka within 15s")
		}
		return events.Event{}, 0, kgo.Message{}
	}

	// The first message on a brand-new topic pays one-off costs (leader
	// election, producer metadata). Production topics are created at worker
	// start-up, so measure steady state after a warm-up message.
	_, warm, _ := publish("0", map[string]any{"warmup": true})
	t.Logf("first message on a new topic: %s", warm)

	var worst time.Duration
	for i := 1; i <= 5; i++ {
		evt, latency, m := publish("42", map[string]any{"user_id": 42, "ward_id": 551, "seq": i})
		worst = max(worst, latency)
		if string(m.Key) != "42" {
			t.Errorf("key = %q, want 42", m.Key)
		}
		var hdr string
		for _, h := range m.Headers {
			if h.Key == "content-type" {
				hdr = string(h.Value)
			}
		}
		if hdr != outbox.ContentType {
			t.Errorf("content-type header = %q", hdr)
		}
		var e events.Event
		if err := json.Unmarshal(m.Value, &e); err != nil {
			t.Fatal(err)
		}
		if e.ID != evt.ID || e.Type != "user.registered" || e.SpecVersion != "1.0" || e.Source != "identity" {
			t.Errorf("event = %+v", e)
		}
	}
	t.Logf("steady-state commit → consumer latency, worst of 5: %s", worst)
	// Backlog T-1.1.1.7: consumers receive the event within 500ms.
	if worst > 500*time.Millisecond {
		t.Errorf("latency %s exceeds the 500ms target", worst)
	}
}

// deleteTopic removes a test's topic so repeated runs don't exhaust the
// broker's partition budget (Redpanda in dev mode has a small one).
func deleteTopic(t *testing.T, broker, topic string) {
	t.Helper()
	conn, err := kgo.Dial("tcp", broker)
	if err != nil {
		t.Logf("delete topic %s: %v", topic, err)
		return
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		t.Logf("delete topic %s: %v", topic, err)
		return
	}
	ctrl, err := kgo.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		t.Logf("delete topic %s: %v", topic, err)
		return
	}
	defer ctrl.Close()
	if err := ctrl.DeleteTopics(topic); err != nil {
		t.Logf("delete topic %s: %v", topic, err)
	}
}
