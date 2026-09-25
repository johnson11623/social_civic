package media

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/media/mediadb"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// UploadTTL is how long a signed upload URL works (docs/media §7).
const UploadTTL = 15 * time.Minute

// Handlers serves /v1/media behind authn.Middleware.
type Handlers struct {
	Pool    *pgxpool.Pool
	Storage *Storage
	CDNBase string // e.g. http://localhost:18080/media/variants, or /media/variants behind the web app
	// UploadBase, when set, replaces the scheme and host of signed upload
	// URLs (e.g. "/s3" in development, where the web app proxies it to
	// storage with the original Host, so the signature still holds). Lets
	// devices other than this machine upload through one address.
	UploadBase string
	Logger     *slog.Logger
	Now        func() time.Time
}

func (h *Handlers) internal(w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(r.Context(), "media failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
}

func (h *Handlers) caller(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := authn.FromContext(r.Context())
	id, err := uuid.Parse(p.Subject)
	if !ok || err != nil {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return 0, false
	}
	userID, err := mediadb.New(h.Pool).GetUploaderByPublicID(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return 0, false
	}
	if err != nil {
		h.internal(w, r, "caller", err)
		return 0, false
	}
	return userID, true
}

// UploadRequest is the body of POST /v1/media/uploads.
type UploadRequest struct {
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	AltText   string `json:"alt_text"`
}

// UploadTarget tells the client where and how to PUT the file.
type UploadTarget struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// UploadResponse is returned by POST /v1/media/uploads.
type UploadResponse struct {
	MediaID string       `json:"media_id"`
	Kind    string       `json:"kind"`
	Upload  UploadTarget `json:"upload"`
}

// CreateUpload serves POST /v1/media/uploads (T-LOCAL-07): validates the
// declared file and returns a signed PUT URL to the private bucket.
func (h *Handlers) CreateUpload(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.caller(w, r)
	if !ok {
		return
	}
	var req UploadRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	alt := strings.TrimSpace(req.AltText)
	kind, ok := KindOf(req.MimeType)
	switch {
	case utf8.RuneCountInString(alt) > maxAltText:
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgAltTextTooLong,
			problem.FieldError{Field: "alt_text", Code: "too_long"})
		return
	case !ok:
		problem.Write(w, r, http.StatusUnprocessableEntity, "unsupported_type", i18n.MsgMediaUnsupported,
			problem.FieldError{Field: "mime_type", Code: "unsupported"})
		return
	case req.SizeBytes <= 0 || req.SizeBytes > MaxBytes(kind):
		problem.Write(w, r, http.StatusUnprocessableEntity, "too_large", i18n.MsgMediaTooLarge,
			problem.FieldError{Field: "size_bytes", Code: "too_large"})
		return
	}
	id := uuid.Must(uuid.NewV7())
	key := id.String() + "/original"
	target, err := h.Storage.PresignUpload(r.Context(), key, UploadTTL)
	if err != nil {
		h.internal(w, r, "presign", err)
		return
	}
	mime := strings.ToLower(strings.TrimSpace(req.MimeType))
	if _, err := mediadb.New(h.Pool).InsertMedia(r.Context(), mediadb.InsertMediaParams{
		PublicID: id, OwnerID: owner, Kind: int16(kind), MimeType: mime, DeclaredSize: req.SizeBytes,
		AltText: alt, OriginalKey: key,
	}); err != nil {
		h.internal(w, r, "insert", err)
		return
	}
	httpjson.Write(w, http.StatusCreated, UploadResponse{
		MediaID: id.String(), Kind: kind.String(),
		Upload: UploadTarget{URL: h.uploadURL(target), Method: http.MethodPut,
			Headers: map[string]string{"Content-Type": mime}, ExpiresAt: h.Now().Add(UploadTTL).UTC()},
	})
}

// load returns the path's media row if the caller owns it.
func (h *Handlers) load(w http.ResponseWriter, r *http.Request, owner int64) (mediadb.GetMediaRow, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "media_id"))
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgMediaNotFound)
		return mediadb.GetMediaRow{}, false
	}
	m, err := mediadb.New(h.Pool).GetMedia(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && m.OwnerID != owner) {
		problem.Write(w, r, http.StatusNotFound, "not_found", i18n.MsgMediaNotFound)
		return mediadb.GetMediaRow{}, false
	}
	if err != nil {
		h.internal(w, r, "load", err)
		return mediadb.GetMediaRow{}, false
	}
	return m, true
}

// Complete serves POST /v1/media/{media_id}/complete (T-LOCAL-08): checks
// the object arrived, is within limits and is what it claims to be, then
// queues processing.
func (h *Handlers) Complete(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.caller(w, r)
	if !ok {
		return
	}
	m, ok := h.load(w, r, owner)
	if !ok {
		return
	}
	if m.State != StateUploading {
		// Completing twice is harmless: report the current state.
		httpjson.Write(w, http.StatusOK, h.toJSON(m))
		return
	}
	kind := Kind(m.Kind)
	size, head, err := h.Storage.StatOriginal(r.Context(), m.OriginalKey)
	if errors.Is(err, ErrNotFound) {
		problem.Write(w, r, http.StatusConflict, "upload_missing", i18n.MsgUploadMissing)
		return
	}
	if err != nil {
		h.internal(w, r, "stat", err)
		return
	}
	if size > MaxBytes(kind) || !sniffMatches(kind, m.MimeType, http.DetectContentType(head)) {
		// Refuse and discard: the signed URL can't limit what was sent.
		_ = h.Storage.DeleteOriginal(r.Context(), m.OriginalKey)
		reason := "content does not match declared type"
		if size > MaxBytes(kind) {
			reason = "too large"
		}
		if _, err := mediadb.New(h.Pool).MarkMediaFailed(r.Context(), mediadb.MarkMediaFailedParams{ID: m.ID, Error: pgtypeText(reason)}); err != nil {
			h.internal(w, r, "reject", err)
			return
		}
		problem.Write(w, r, http.StatusUnprocessableEntity, "upload_rejected", i18n.MsgUploadRejected)
		return
	}
	err = pgx.BeginFunc(r.Context(), h.Pool, func(tx pgx.Tx) error {
		n, err := mediadb.New(tx).MarkMediaProcessing(r.Context(), mediadb.MarkMediaProcessingParams{ID: m.ID, SizeBytes: pgtype.Int8{Int64: size, Valid: true}})
		if err != nil || n == 0 {
			return err
		}
		return outbox.Enqueue(r.Context(), tx, events.TopicMediaUploaded, m.PublicID.String(),
			events.New("media", events.TopicMediaUploaded, h.Now(), MediaUploadedData{
				MediaID: m.PublicID.String(), Kind: kind.String(), MimeType: m.MimeType,
				OriginalKey: m.OriginalKey, SizeBytes: size,
			}))
	})
	if err != nil {
		h.internal(w, r, "complete", err)
		return
	}
	m.State, m.SizeBytes = StateProcessing, pgtype.Int8{Int64: size, Valid: true}
	httpjson.Write(w, http.StatusOK, h.toJSON(m))
}

// Get serves GET /v1/media/{media_id}: the owner polls it until ready.
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	owner, ok := h.caller(w, r)
	if !ok {
		return
	}
	m, ok := h.load(w, r, owner)
	if !ok {
		return
	}
	httpjson.Write(w, http.StatusOK, h.toJSON(m))
}

func (h *Handlers) toJSON(m mediadb.GetMediaRow) MediaJSON {
	out := MediaJSON{
		MediaID: m.PublicID.String(), Kind: Kind(m.Kind).String(), State: stateNames[m.State], AltText: m.AltText,
		Placeholder: m.Placeholder.String, Error: m.Error.String,
	}
	out.Width, out.Height, out.DurationMS = int(m.Width.Int32), int(m.Height.Int32), int(m.DurationMs.Int32)
	if m.State == StateReady {
		v := decodeVariants(m.Variants)
		for _, img := range v.Images {
			out.Images = append(out.Images, variantJSON(h.CDNBase, img))
		}
		if v.Poster != nil {
			p := variantJSON(h.CDNBase, *v.Poster)
			out.Poster = &p
		}
		if v.HLS != "" {
			out.HLSURL = h.CDNBase + "/" + v.HLS
		}
	}
	return out
}

// uploadURL is where the client sends the file (see UploadBase).
func (h *Handlers) uploadURL(u *url.URL) string {
	if h.UploadBase == "" {
		return u.String()
	}
	return strings.TrimSuffix(h.UploadBase, "/") + u.RequestURI()
}

func pgtypeText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
