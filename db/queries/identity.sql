-- name: InsertUser :one
INSERT INTO users (
    public_id, national_id_hash, national_id_key_version, display_name,
    preferred_lang, ward_id, constituency_id, county_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING id, public_id, created_at;

-- name: InsertConsent :exec
INSERT INTO consents (user_id, version, granted_at, ip_hash)
VALUES ($1, $2, $3, $4);
