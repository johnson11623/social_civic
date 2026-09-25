// Package media handles uploads and delivery of images and video (docs/media).
//
// Flow: the client asks for an upload (POST /v1/media/uploads) and gets a
// signed PUT URL to the private originals bucket; it uploads directly to
// storage (large files never pass through the API), then completes the
// upload (POST /v1/media/{id}/complete), which verifies the object and
// queues media.uploaded. The media worker (cmd/mediaworker) produces the
// variants into the public bucket — images re-encoded without metadata
// (EXIF, GPS), video as an HLS ladder — and marks the media ready.
package media

import (
	"encoding/json"
	"strings"
	"time"
)

// Kind is what was uploaded.
type Kind int16

const (
	KindImage Kind = 1
	KindVideo Kind = 2
)

func (k Kind) String() string {
	if k == KindVideo {
		return "video"
	}
	return "image"
}

// States of a media row.
const (
	StateUploading  int16 = 1
	StateProcessing int16 = 2
	StateReady      int16 = 3
	StateFailed     int16 = 4
)

var stateNames = map[int16]string{
	StateUploading: "uploading", StateProcessing: "processing", StateReady: "ready", StateFailed: "failed",
}

// Size limits (docs/media §7). Entitlement-based limits (T-W1.4.2.4) will
// lower these for free accounts once billing exists.
const (
	MaxImageBytes = 10 << 20
	MaxVideoBytes = 100 << 20
	maxAltText    = 1000
)

// accepted maps each uploadable content type to its kind.
var accepted = map[string]Kind{
	"image/jpeg":      KindImage,
	"image/png":       KindImage,
	"image/webp":      KindImage,
	"video/mp4":       KindVideo,
	"video/quicktime": KindVideo,
	"video/webm":      KindVideo,
}

// KindOf returns the kind for an accepted content type.
func KindOf(mimeType string) (Kind, bool) {
	k, ok := accepted[strings.ToLower(strings.TrimSpace(mimeType))]
	return k, ok
}

// MaxBytes is the size limit for a kind.
func MaxBytes(k Kind) int64 {
	if k == KindVideo {
		return MaxVideoBytes
	}
	return MaxImageBytes
}

// sniffMatches reports whether the first bytes of an upload look like the
// declared type (as http.DetectContentType reports it), so a renamed file
// or a script can't pass for an image.
func sniffMatches(k Kind, declared, sniffed string) bool {
	switch k {
	case KindImage:
		return sniffed == declared
	default:
		// DetectContentType knows MP4 and WebM; QuickTime shows as octet-stream,
		// and ffprobe in the worker is the real check for video.
		return strings.HasPrefix(sniffed, "video/") || sniffed == "application/octet-stream"
	}
}

// ImageVariant is one processed size of an image, in WebP with a JPEG fallback.
type ImageVariant struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	WebP   string `json:"webp"` // object keys in the public bucket
	JPEG   string `json:"jpeg"`
}

// Variants are what the worker produced (stored as JSONB).
type Variants struct {
	Images     []ImageVariant `json:"images,omitempty"`
	Poster     *ImageVariant  `json:"poster,omitempty"`     // video
	HLS        string         `json:"hls,omitempty"`        // master playlist key
	Renditions []string       `json:"renditions,omitempty"` // e.g. 240p, 480p
}

// MediaUploadedData is the payload of media.uploaded.
type MediaUploadedData struct {
	MediaID     string `json:"media_id"`
	Kind        string `json:"kind"`
	MimeType    string `json:"mime_type"`
	OriginalKey string `json:"original_key"`
	SizeBytes   int64  `json:"size_bytes"`
}

// MediaProcessedData is the payload of media.processed and media.failed.
type MediaProcessedData struct {
	MediaID string    `json:"media_id"`
	Kind    string    `json:"kind"`
	State   string    `json:"state"`
	Error   string    `json:"error,omitempty"`
	At      time.Time `json:"at"`
}

// ---- API shapes ---------------------------------------------------------------------

// VariantJSON is an image size with public URLs.
type VariantJSON struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	WebP   string `json:"webp_url,omitempty"`
	JPEG   string `json:"jpeg_url"`
}

// MediaJSON is a media item in API responses.
type MediaJSON struct {
	MediaID     string        `json:"media_id"`
	Kind        string        `json:"kind"`
	State       string        `json:"state"`
	AltText     string        `json:"alt_text"`
	Width       int           `json:"width,omitempty"`
	Height      int           `json:"height,omitempty"`
	DurationMS  int           `json:"duration_ms,omitempty"`
	Placeholder string        `json:"placeholder,omitempty"` // data: URI to show while loading
	Images      []VariantJSON `json:"images,omitempty"`
	Poster      *VariantJSON  `json:"poster,omitempty"`
	HLSURL      string        `json:"hls_url,omitempty"`
	Error       string        `json:"error,omitempty"`
}

// URL is the public address of a processed object key; empty for no key.
func URL(cdn, key string) string {
	if key == "" {
		return ""
	}
	return cdn + "/" + key
}

func variantJSON(cdn string, v ImageVariant) VariantJSON {
	out := VariantJSON{Name: v.Name, Width: v.Width, Height: v.Height, JPEG: cdn + "/" + v.JPEG}
	if v.WebP != "" { // absent when the worker's ffmpeg has no WebP encoder
		out.WebP = cdn + "/" + v.WebP
	}
	return out
}

// decodeVariants reads the JSONB column; nil or invalid gives no variants.
func decodeVariants(raw []byte) Variants {
	var v Variants
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &v)
	}
	return v
}

// Embedded renders the media column that post queries select (see
// db/queries/post.sql: id, kind, alt, w, h, ms, ph, v). Nil when there is
// no ready media.
func Embedded(cdn string, raw []byte) *MediaJSON {
	if len(raw) == 0 {
		return nil
	}
	var row struct {
		ID   string   `json:"id"`
		Kind int16    `json:"kind"`
		Alt  string   `json:"alt"`
		W    int      `json:"w"`
		H    int      `json:"h"`
		MS   int      `json:"ms"`
		PH   string   `json:"ph"`
		V    Variants `json:"v"`
	}
	if err := json.Unmarshal(raw, &row); err != nil || row.ID == "" {
		return nil
	}
	out := &MediaJSON{MediaID: row.ID, Kind: Kind(row.Kind).String(), State: "ready", AltText: row.Alt,
		Width: row.W, Height: row.H, DurationMS: row.MS, Placeholder: row.PH}
	for _, img := range row.V.Images {
		out.Images = append(out.Images, variantJSON(cdn, img))
	}
	if row.V.Poster != nil {
		p := variantJSON(cdn, *row.V.Poster)
		out.Poster = &p
	}
	if row.V.HLS != "" {
		out.HLSURL = cdn + "/" + row.V.HLS
	}
	return out
}
