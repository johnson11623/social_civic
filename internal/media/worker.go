package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	kgo "github.com/segmentio/kafka-go"

	"github.com/johnson11623/social_civic/internal/media/mediadb"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Worker consumes media.uploaded, processes each upload and records the
// result (T-LOCAL-05, T-LOCAL-06). Offsets are committed only after a
// message is handled, so a restart resumes where it stopped.
type Worker struct {
	Pool      *pgxpool.Pool
	Storage   *Storage
	Processor *Processor
	Reader    *kgo.Reader
	TmpDir    string
	Logger    *slog.Logger
	Now       func() time.Time
}

// Run processes messages until ctx ends.
func (w *Worker) Run(ctx context.Context) error {
	for {
		msg, err := w.Reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("fetch: %w", err)
		}
		w.handleWithRetry(ctx, msg.Value)
		if err := w.Reader.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
			return fmt.Errorf("commit: %w", err)
		}
	}
}

// handleWithRetry retries transient faults (storage, database) with backoff;
// after the last attempt the media is marked failed so it doesn't hang.
func (w *Worker) handleWithRetry(ctx context.Context, value []byte) {
	var env struct {
		Data MediaUploadedData `json:"data"`
	}
	if err := json.Unmarshal(value, &env); err != nil {
		w.Logger.ErrorContext(ctx, "media event unreadable", "err", err)
		return
	}
	id, err := uuid.Parse(env.Data.MediaID)
	if err != nil {
		w.Logger.ErrorContext(ctx, "media event without id", "err", err)
		return
	}
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		err := w.Handle(ctx, id)
		if err == nil || ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrInvalidMedia) || attempt == 4 {
			w.Logger.WarnContext(ctx, "media failed", "media", id, "attempt", attempt, "err", err)
			w.fail(ctx, id, err)
			return
		}
		w.Logger.WarnContext(ctx, "media processing retry", "media", id, "attempt", attempt, "err", err)
		select {
		case <-time.After(backoff):
			backoff *= 3
		case <-ctx.Done():
			return
		}
	}
}

// Handle processes one upload. Media not in the processing state (already
// done, or rejected) is skipped, so redelivery is harmless.
func (w *Worker) Handle(ctx context.Context, publicID uuid.UUID) error {
	start := w.Now()
	m, err := mediadb.New(w.Pool).GetMedia(ctx, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.State != StateProcessing {
		return nil
	}
	dir, err := os.MkdirTemp(w.TmpDir, "media-"+publicID.String()+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	in := filepath.Join(dir, "original")
	if err := w.Storage.DownloadOriginal(ctx, m.OriginalKey, in); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		return err
	}
	var res Result
	if Kind(m.Kind) == KindVideo {
		res, err = w.Processor.Video(ctx, in, out)
	} else {
		res, err = w.Processor.Image(ctx, in, out)
	}
	if err != nil {
		return err
	}
	// Keys are prefixed with the media id: unique, so cacheable forever.
	prefix := publicID.String() + "/"
	for _, f := range res.Files {
		if err := w.Storage.UploadPublic(ctx, prefix+f.Key, f.Path, f.ContentType); err != nil {
			return fmt.Errorf("upload %s: %w", f.Key, err)
		}
	}
	res.Variants = prefixed(res.Variants, prefix)
	variants, err := json.Marshal(res.Variants)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, w.Pool, func(tx pgx.Tx) error {
		n, err := mediadb.New(tx).MarkMediaReady(ctx, mediadb.MarkMediaReadyParams{
			ID: m.ID, Variants: variants, Placeholder: pgtypeText(res.Placeholder),
			Width: int4(res.Width), Height: int4(res.Height), DurationMs: int4(res.DurationMS),
		})
		if err != nil || n == 0 {
			return err
		}
		return outbox.Enqueue(ctx, tx, events.TopicMediaProcessed, publicID.String(),
			events.New("media", events.TopicMediaProcessed, w.Now(), MediaProcessedData{
				MediaID: publicID.String(), Kind: Kind(m.Kind).String(), State: "ready", At: w.Now(),
			}))
	})
	if err == nil {
		w.Logger.InfoContext(ctx, "media ready", "media", publicID, "kind", Kind(m.Kind).String(),
			"files", len(res.Files), "took", w.Now().Sub(start).Round(time.Millisecond))
	}
	return err
}

func (w *Worker) fail(ctx context.Context, id uuid.UUID, cause error) {
	m, err := mediadb.New(w.Pool).GetMedia(ctx, id)
	if err != nil {
		return
	}
	msg := "processing failed"
	if errors.Is(cause, ErrInvalidMedia) {
		msg = "not a usable image or video"
	}
	_ = pgx.BeginFunc(ctx, w.Pool, func(tx pgx.Tx) error {
		n, err := mediadb.New(tx).MarkMediaFailed(ctx, mediadb.MarkMediaFailedParams{ID: m.ID, Error: pgtypeText(msg)})
		if err != nil || n == 0 {
			return err
		}
		return outbox.Enqueue(ctx, tx, events.TopicMediaFailed, id.String(),
			events.New("media", events.TopicMediaFailed, w.Now(), MediaProcessedData{
				MediaID: id.String(), Kind: Kind(m.Kind).String(), State: "failed", Error: msg, At: w.Now(),
			}))
	})
}

func prefixed(v Variants, prefix string) Variants {
	key := func(k string) string {
		if k == "" {
			return ""
		}
		return prefix + k
	}
	for i := range v.Images {
		v.Images[i].WebP, v.Images[i].JPEG = key(v.Images[i].WebP), key(v.Images[i].JPEG)
	}
	if v.Poster != nil {
		p := *v.Poster
		p.WebP, p.JPEG = key(p.WebP), key(p.JPEG)
		v.Poster = &p
	}
	if v.HLS != "" {
		v.HLS = prefix + v.HLS
	}
	return v
}

func int4(n int) pgtype.Int4 { return pgtype.Int4{Int32: int32(n), Valid: n > 0} }
