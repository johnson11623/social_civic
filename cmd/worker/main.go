// Command worker runs background processing: the outbox relay that publishes
// domain events to Kafka, and the erasure saga. Elevation and moderation
// workers join later.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/johnson11623/social_civic/internal/identity"
	"github.com/johnson11623/social_civic/internal/membership"
	"github.com/johnson11623/social_civic/internal/post"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/kafka"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker exited", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	dbURL := os.Getenv("DATABASE_URL")
	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	if dbURL == "" || brokers[0] == "" {
		return errors.New("DATABASE_URL and KAFKA_BROKERS are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	setupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	topics := make([]kafka.Topic, 0, len(events.AllTopics))
	for _, t := range events.AllTopics {
		topics = append(topics, kafka.Topic{Name: t, Partitions: 6, ReplicationFactor: 1})
	}
	if err := kafka.EnsureTopics(setupCtx, brokers[0], topics); err != nil {
		return err
	}

	producer := kafka.NewProducer(brokers)
	defer producer.Close()

	relay := &outbox.Relay{Pool: pool, Producer: producer, Logger: logger}
	steps := identity.DefaultErasureSteps()
	steps[identity.StepMembershipRevoke] = membership.RevokeRolesStep
	erasure := &identity.ErasureProcessor{Pool: pool, Logger: logger, Now: time.Now, Steps: steps}

	logger.Info("worker started", "components", []string{"outbox-relay", "erasure-saga", "post-partitions"}, "topics", events.AllTopics)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return relay.Run(gctx) })
	g.Go(func() error { return erasure.Run(gctx) })
	g.Go(func() error { return post.RunPartitionMaintenance(gctx, pool, logger) })
	err = g.Wait()
	logger.Info("worker stopped")
	return err
}
