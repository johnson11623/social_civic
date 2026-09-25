# Local media (docs/media)

Uploads, processing and delivery of photos and video, fully local.

| Piece | Where | What |
|---|---|---|
| Storage (S3 API) | `localhost:19000` | SeaweedFS. `civic-originals` (private, signed URLs only) and `civic-media` (processed, public-read). Keys: `civicdev` / `civicdev-secret-change-me`. |
| CDN | `localhost:18080/media/variants/…` | Caddy in front of `civic-media`, production cache headers. Originals are never served. |
| Worker | container `mediaworker` | Go + ffmpeg 8.1: image variants (WebP + JPEG, 320/1080/2048, metadata stripped), blurred placeholder, video poster + HLS (`240p,480p` locally; `MEDIA_VIDEO_RENDITIONS` to change). |

MinIO, which docs/media names, no longer publishes images; SeaweedFS speaks
the same S3 API, and the Go code uses a standard S3 client, so production
S3 is a configuration change.

## Run it

```sh
make run-api        # API (also creates the buckets); needs make media-up first for uploads
make run-worker     # outbox relay: publishes media.uploaded to Kafka
make media-worker   # starts SeaweedFS, Caddy and the media worker
cd civic-console && pnpm dev
```

Then post from the web app with the photo/video button. The first
`make media-worker` builds the image and compiles dependencies (slow once;
caches persist in the `gobuild`/`gomod` volumes).

## Flow

1. `POST /v1/media/uploads` → signed PUT URL (15 min) to `civic-originals`.
2. The browser PUTs the file directly (never through the API).
3. `POST /v1/media/{id}/complete` → size and content checked, `media.uploaded` queued.
4. The worker writes variants to `civic-media/{id}/…`, marks the media ready, emits `media.processed`.
5. `POST /v1/channels/{id}/posts` with `media_id` attaches it; posts carry CDN URLs.

## Tests

```sh
make test-media     # real ffmpeg, PostgreSQL and S3, inside the worker container
```

## Reset

```sh
make media-reset    # deletes all stored media
```
