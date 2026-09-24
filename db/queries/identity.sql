-- name: InsertUser :one
INSERT INTO users (
    public_id, national_id_hash, national_id_key_version, display_name,
    preferred_lang, ward_id, constituency_id, county_id,
    msisdn_ciphertext, msisdn_key_version, msisdn_hash
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING id, public_id, created_at;

-- name: InsertConsent :exec
INSERT INTO consents (user_id, version, granted_at, ip_hash)
VALUES ($1, $2, $3, $4);

-- name: GetUserByNationalIDHash :one
SELECT id, public_id, state, preferred_lang, ward_id, constituency_id, county_id,
       msisdn_ciphertext, msisdn_key_version
FROM users
WHERE national_id_hash = $1;

-- name: GetUserByID :one
SELECT id, public_id, state, ward_id, constituency_id, county_id
FROM users
WHERE id = $1;

-- name: InvalidateActiveOTPs :exec
UPDATE otp_challenges
SET invalidated_at = now()
WHERE user_id = $1 AND consumed_at IS NULL AND invalidated_at IS NULL;

-- name: InsertOTPChallenge :one
-- created_at and expires_at both come from the application clock, so clock
-- skew between app and database cannot violate otp_expires_after_created.
INSERT INTO otp_challenges (user_id, code_hash, key_version, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: GetActiveOTPForUpdate :one
SELECT id, code_hash, expires_at, attempts
FROM otp_challenges
WHERE user_id = $1 AND consumed_at IS NULL AND invalidated_at IS NULL
ORDER BY id DESC
LIMIT 1
FOR UPDATE;

-- name: RecordFailedOTPAttempt :one
UPDATE otp_challenges
SET attempts = attempts + 1,
    invalidated_at = CASE WHEN attempts + 1 >= sqlc.arg(max_attempts)::int THEN now() END
WHERE id = $1
RETURNING attempts;

-- name: ConsumeOTP :execrows
UPDATE otp_challenges
SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL AND invalidated_at IS NULL;

-- name: InsertRefreshToken :exec
INSERT INTO refresh_tokens (jti, user_id, family_id, issued_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetRefreshTokenForUpdate :one
SELECT jti, user_id, family_id, expires_at, revoked_at, revoke_reason
FROM refresh_tokens
WHERE jti = $1
FOR UPDATE;

-- name: RotateRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now(), revoke_reason = 'rotated', replaced_by = $2
WHERE jti = $1 AND revoked_at IS NULL;

-- name: RevokeAllUserRefreshTokens :execrows
UPDATE refresh_tokens
SET revoked_at = now(), revoke_reason = sqlc.arg(reason)::text
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: GetUserIDByPublicID :one
SELECT id, state FROM users WHERE public_id = $1;

-- name: WithdrawConsent :one
UPDATE consents
SET withdrawn_at = $3
WHERE user_id = $1 AND version = $2 AND withdrawn_at IS NULL
RETURNING withdrawn_at;

-- name: GetConsent :one
SELECT granted_at, withdrawn_at FROM consents WHERE user_id = $1 AND version = $2;

-- name: HasActiveConsent :one
SELECT EXISTS (
    SELECT 1
    FROM consents c
    JOIN users u ON u.id = c.user_id
    WHERE u.public_id = $1 AND c.version = $2 AND c.withdrawn_at IS NULL AND u.state = 1
) AS active;
