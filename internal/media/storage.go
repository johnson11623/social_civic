package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// StorageConfig points at an S3-compatible store (SeaweedFS locally, S3 in
// production).
type StorageConfig struct {
	Endpoint       string // host:port the service reaches, e.g. localhost:19000 or seaweedfs:8333
	PublicEndpoint string // host:port browsers reach for signed uploads; defaults to Endpoint
	AccessKey      string
	SecretKey      string
	Region         string
	UseTLS         bool
	Originals      string // private bucket: uploads, EXIF intact, signed URLs only
	Public         string // public-read bucket: processed variants, served by the CDN
}

// Storage wraps the object store.
type Storage struct {
	client  *minio.Client
	presign *minio.Client // signs with the browser-facing host
	cfg     StorageConfig
}

// ErrNotFound is returned for a missing object.
var ErrNotFound = errors.New("media: object not found")

// NewStorage connects to the store. Region is fixed so signing never needs a
// network round trip.
func NewStorage(cfg StorageConfig) (*Storage, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.PublicEndpoint == "" {
		cfg.PublicEndpoint = cfg.Endpoint
	}
	opts := func() *minio.Options {
		return &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.UseTLS, Region: cfg.Region}
	}
	client, err := minio.New(cfg.Endpoint, opts())
	if err != nil {
		return nil, err
	}
	presign, err := minio.New(cfg.PublicEndpoint, opts())
	if err != nil {
		return nil, err
	}
	return &Storage{client: client, presign: presign, cfg: cfg}, nil
}

// EnsureBuckets creates the two buckets if they don't exist (development).
func (s *Storage) EnsureBuckets(ctx context.Context) error {
	for _, b := range []string{s.cfg.Originals, s.cfg.Public} {
		ok, err := s.client.BucketExists(ctx, b)
		if err != nil {
			return fmt.Errorf("bucket %s: %w", b, err)
		}
		if !ok {
			if err := s.client.MakeBucket(ctx, b, minio.MakeBucketOptions{Region: s.cfg.Region}); err != nil {
				return fmt.Errorf("make bucket %s: %w", b, err)
			}
		}
	}
	return nil
}

// PresignUpload returns a URL the client PUTs the original to.
func (s *Storage) PresignUpload(ctx context.Context, key string, ttl time.Duration) (*url.URL, error) {
	return s.presign.PresignedPutObject(ctx, s.cfg.Originals, key, ttl)
}

// StatOriginal returns an uploaded original's size and its first bytes (for
// content sniffing).
func (s *Storage) StatOriginal(ctx context.Context, key string) (int64, []byte, error) {
	info, err := s.client.StatObject(ctx, s.cfg.Originals, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == 404 {
			return 0, nil, ErrNotFound
		}
		return 0, nil, err
	}
	opts := minio.GetObjectOptions{}
	_ = opts.SetRange(0, 511)
	obj, err := s.client.GetObject(ctx, s.cfg.Originals, key, opts)
	if err != nil {
		return 0, nil, err
	}
	defer obj.Close()
	head, err := io.ReadAll(io.LimitReader(obj, 512))
	return info.Size, head, err
}

// DownloadOriginal writes an original to a local file.
func (s *Storage) DownloadOriginal(ctx context.Context, key, path string) error {
	return s.client.FGetObject(ctx, s.cfg.Originals, key, path, minio.GetObjectOptions{})
}

// UploadPublic stores a processed file in the public bucket, cacheable forever
// (keys are unique per media).
func (s *Storage) UploadPublic(ctx context.Context, key, path, contentType string) error {
	_, err := s.client.FPutObject(ctx, s.cfg.Public, key, path, minio.PutObjectOptions{
		ContentType: contentType, CacheControl: "public, max-age=31536000, immutable",
	})
	return err
}

// DeleteOriginal removes an original (a rejected upload).
func (s *Storage) DeleteOriginal(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.cfg.Originals, key, minio.RemoveObjectOptions{})
}

// StorageConfigFromEnv reads S3_ENDPOINT, S3_PUBLIC_ENDPOINT, S3_ACCESS_KEY,
// S3_SECRET_KEY, S3_REGION, S3_USE_TLS, S3_ORIGINALS_BUCKET and S3_MEDIA_BUCKET.
func StorageConfigFromEnv() StorageConfig {
	env := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	return StorageConfig{
		Endpoint: os.Getenv("S3_ENDPOINT"), PublicEndpoint: os.Getenv("S3_PUBLIC_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY"),
		Region: env("S3_REGION", "us-east-1"), UseTLS: os.Getenv("S3_USE_TLS") == "true",
		Originals: env("S3_ORIGINALS_BUCKET", "civic-originals"), Public: env("S3_MEDIA_BUCKET", "civic-media"),
	}
}
