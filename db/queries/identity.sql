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

-- name: GetMFA :one
SELECT user_id, secret_enc, key_version, enabled_at, last_step FROM user_mfa WHERE user_id = $1;

-- name: StartMFAEnrolment :execrows
-- A new pending secret replaces an unfinished enrolment, never an enabled one.
INSERT INTO user_mfa (user_id, secret_enc, key_version) VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE SET secret_enc = EXCLUDED.secret_enc, key_version = EXCLUDED.key_version,
    last_step = 0, created_at = now()
WHERE user_mfa.enabled_at IS NULL;

-- name: AcceptMFAStep :execrows
-- Records a used code; enables MFA on first use. Refuses stale steps, so
-- two concurrent uses of one code can't both pass.
UPDATE user_mfa SET last_step = sqlc.arg(step), enabled_at = COALESCE(enabled_at, now())
WHERE user_id = sqlc.arg(user_id) AND last_step < sqlc.arg(step);

-- name: DeleteMFA :exec
DELETE FROM user_mfa WHERE user_id = $1;

-- name: GetProfile :one
-- avatar_key is the smallest processed size of the profile photo, if any.
SELECT u.id, u.public_id, u.display_name, u.preferred_lang, u.ward_id, u.created_at,
       COALESCE(av.public_id::text, '')::text AS avatar_id,
       COALESCE(av.variants->'images'->0->>'jpeg', '')::text AS avatar_key
FROM users u
LEFT JOIN media av ON av.id = u.avatar_media_id AND av.state = 3
WHERE u.public_id = $1 AND u.state = 1;

-- name: GetOwnReadyImage :one
-- A photo the user uploaded that has finished processing.
SELECT id FROM media WHERE public_id = $1 AND owner_id = $2 AND kind = 1 AND state = 3;

-- name: SetAvatar :exec
UPDATE users SET avatar_media_id = sqlc.narg(media_id), updated_at = now()
WHERE id = sqlc.arg(id) AND state = 1;

-- name: UpdateProfile :one
UPDATE users
SET display_name   = COALESCE(sqlc.narg(display_name), display_name),
    preferred_lang = COALESCE(sqlc.narg(preferred_lang), preferred_lang),
    updated_at     = now()
WHERE id = sqlc.arg(id) AND state = 1
RETURNING display_name, preferred_lang;
