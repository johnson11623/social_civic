-- name: GetUploaderByPublicID :one
SELECT id FROM users WHERE public_id = $1 AND state = 1;

-- name: InsertMedia :one
INSERT INTO media (public_id, owner_id, kind, mime_type, declared_size, alt_text, original_key)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at;

-- name: GetMedia :one
SELECT id, public_id, owner_id, kind, mime_type, declared_size, size_bytes, alt_text, state, original_key,
       width, height, duration_ms, placeholder, variants, error, created_at, processed_at
FROM media
WHERE public_id = $1;

-- name: MarkMediaProcessing :execrows
UPDATE media SET state = 2, size_bytes = sqlc.arg(size_bytes), updated_at = now()
WHERE id = sqlc.arg(id) AND state = 1;

-- name: MarkMediaReady :execrows
UPDATE media
SET state = 3, width = sqlc.narg(width), height = sqlc.narg(height), duration_ms = sqlc.narg(duration_ms),
    placeholder = sqlc.narg(placeholder), variants = sqlc.arg(variants), error = NULL,
    processed_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND state = 2;

-- name: MarkMediaFailed :execrows
UPDATE media SET state = 4, error = sqlc.arg(error), updated_at = now()
WHERE id = sqlc.arg(id) AND state IN (1, 2);
