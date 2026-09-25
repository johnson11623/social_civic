// Command mediaworker processes uploaded media (docs/media): it consumes
// media.uploaded, runs ffmpeg for image variants and the HLS ladder, stores
// the results in the public bucket and marks each upload ready.
//
// Locally it runs in a container with ffmpeg (`make media-worker`).
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
	kgo "github.com/segmentio/kafka-go"

	"github.com/johnson11623/social_civic/internal/media"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/kafka"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("media worker exited", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	dbURL := os.Getenv("DATABASE_URL")
	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	if dbURL == "" || brokers[0] == "" {
		return errors.New("DATABASE_URL and KAFKA_BROKERS are required")
	}
	storage, err := media.NewStorage(media.StorageConfigFromEnv())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	setup, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := storage.EnsureBuckets(setup); err != nil {
		return err
	}
	if err := kafka.EnsureTopics(setup, brokers[0], []kafka.Topic{{Name: events.TopicMediaUploaded, Partitions: 6, ReplicationFactor: 1}}); err != nil {
		return err
	}

	renditions, err := media.ParseRenditions(os.Getenv("MEDIA_VIDEO_RENDITIONS"))
	if err != nil {
		return err
	}
	reader := kgo.NewReader(kgo.ReaderConfig{
		Brokers: brokers, GroupID: "media-transcode", Topic: events.TopicMediaUploaded,
		MinBytes: 1, MaxBytes: 1 << 20, MaxWait: time.Second,
	})
	defer reader.Close()

	w := &media.Worker{
		Pool: pool, Storage: storage, Reader: reader, TmpDir: os.Getenv("MEDIA_TMP_DIR"), Logger: logger, Now: time.Now,
		Processor: &media.Processor{Renditions: renditions},
	}
	logger.Info("media worker started", "topic", events.TopicMediaUploaded, "renditions", len(renditions))
	return w.Run(ctx)
}
