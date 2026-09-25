-- docs/media — uploaded media and their processed variants.
--
-- An upload starts as a row (uploading) with a signed URL to the private
-- originals bucket; completing it queues processing; the media worker
-- writes the variants to the public bucket and marks it ready (or failed).

CREATE TABLE media (
    id            BIGSERIAL   PRIMARY KEY,
    public_id     UUID        NOT NULL UNIQUE,
    owner_id      BIGINT      NOT NULL REFERENCES users (id),
    kind          SMALLINT    NOT NULL,             -- 1=image 2=video
    mime_type     TEXT        NOT NULL,
    declared_size BIGINT      NOT NULL,
    size_bytes    BIGINT,                           -- verified on completion
    alt_text      TEXT        NOT NULL DEFAULT '',  -- optional description (WCAG 1.1.1); pages fall back to a generic label
    state         SMALLINT    NOT NULL DEFAULT 1,   -- 1=uploading 2=processing 3=ready 4=failed
    original_key  TEXT        NOT NULL,
    width         INT,
    height        INT,
    duration_ms   INT,
    placeholder   TEXT,                             -- tiny blurred data: URI
    variants      JSONB,
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ,
    CONSTRAINT media_kind_valid CHECK (kind IN (1, 2)),
    CONSTRAINT media_state_valid CHECK (state BETWEEN 1 AND 4),
    CONSTRAINT media_alt_text_len CHECK (char_length(alt_text) <= 1000),
    CONSTRAINT media_size_positive CHECK (declared_size > 0 AND (size_bytes IS NULL OR size_bytes > 0)),
    CONSTRAINT media_ready_has_variants CHECK (state <> 3 OR variants IS NOT NULL)
);
CREATE INDEX idx_media_owner ON media (owner_id, created_at DESC);
