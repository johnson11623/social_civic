// Package kafka adapts segmentio/kafka-go to the outbox Producer and creates topics.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	kgo "github.com/segmentio/kafka-go"

	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Producer publishes synchronously with acks from all in-sync replicas.
// Topics are never auto-created: a missing topic is a deployment error.
type Producer struct {
	w *kgo.Writer
}

// NewProducer connects to the given brokers.
func NewProducer(brokers []string) *Producer {
	return &Producer{w: &kgo.Writer{
		Addr:                   kgo.TCP(brokers...),
		Balancer:               &kgo.Hash{}, // same key → same partition → per-key order
		RequiredAcks:           kgo.RequireAll,
		AllowAutoTopicCreation: false,
		BatchTimeout:           5 * time.Millisecond, // the outbox already batches
		WriteTimeout:           10 * time.Second,
	}}
}

// Produce implements outbox.Producer.
func (p *Producer) Produce(ctx context.Context, msgs []outbox.Message) error {
	out := make([]kgo.Message, len(msgs))
	for i, m := range msgs {
		out[i] = kgo.Message{Topic: m.Topic, Key: []byte(m.Key), Value: m.Value}
		for k, v := range m.Headers {
			out[i].Headers = append(out[i].Headers, kgo.Header{Key: k, Value: []byte(v)})
		}
	}
	return p.w.WriteMessages(ctx, out...)
}

// Close flushes and closes the producer.
func (p *Producer) Close() error { return p.w.Close() }

// Topic describes a topic to create.
type Topic struct {
	Name              string
	Partitions        int
	ReplicationFactor int
}

// EnsureTopics creates any missing topics; existing topics are left unchanged.
func EnsureTopics(ctx context.Context, broker string, topics []Topic) error {
	var d kgo.Dialer
	conn, err := d.DialContext(ctx, "tcp", broker)
	if err != nil {
		return fmt.Errorf("kafka: dial %s: %w", broker, err)
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("kafka: find controller: %w", err)
	}
	cc, err := d.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("kafka: dial controller: %w", err)
	}
	defer cc.Close()

	for _, t := range topics {
		err := cc.CreateTopics(kgo.TopicConfig{
			Topic:             t.Name,
			NumPartitions:     t.Partitions,
			ReplicationFactor: t.ReplicationFactor,
		})
		if err != nil && !errors.Is(err, kgo.TopicAlreadyExists) {
			return fmt.Errorf("kafka: create topic %s: %w", t.Name, err)
		}
	}
	return nil
}
