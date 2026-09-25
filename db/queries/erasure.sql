-- name: InsertErasureRequest :one
INSERT INTO erasure_requests (public_id, user_id, reason, requested_at, ack_by, completion_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: InsertErasureStep :exec
INSERT INTO erasure_steps (request_id, step) VALUES ($1, $2);

-- name: ClaimPendingErasureStep :one
SELECT s.request_id, s.step, s.attempts, r.public_id AS request_public_id, r.user_id
FROM erasure_steps s
JOIN erasure_requests r ON r.id = s.request_id
WHERE s.state = 1
ORDER BY s.request_id, s.step
LIMIT 1
FOR UPDATE OF s SKIP LOCKED;

-- name: MarkErasureStepDone :exec
UPDATE erasure_steps
SET state = 2, attempts = attempts + 1, last_error = NULL, updated_at = now()
WHERE request_id = $1 AND step = $2;

-- name: RecordErasureStepFailure :one
UPDATE erasure_steps
SET attempts = attempts + 1,
    last_error = sqlc.arg(last_error)::text,
    state = CASE WHEN attempts + 1 >= sqlc.arg(max_attempts)::int THEN 3 ELSE 1 END,
    updated_at = now()
WHERE request_id = sqlc.arg(request_id) AND step = sqlc.arg(step)
RETURNING state;

-- name: CountUnfinishedErasureSteps :one
SELECT count(*)::int FROM erasure_steps WHERE request_id = $1 AND state <> 2;

-- name: CompleteErasureRequest :exec
UPDATE erasure_requests
SET state = 2, completed_at = $2, retained_fields = $3
WHERE id = $1;

-- name: FailErasureRequest :exec
UPDATE erasure_requests SET state = 3 WHERE id = $1 AND state = 1;

-- name: DeleteVerificationAttemptsForUser :exec
DELETE FROM verification_attempts va
WHERE va.id_hash = (SELECT u.national_id_hash FROM users u WHERE u.id = $1);

-- name: AnonymizeUser :exec
UPDATE users
SET national_id_hash        = $2,
    national_id_key_version = 'erased',
    display_name            = '[deleted user]',
    msisdn_ciphertext       = NULL,
    msisdn_key_version      = NULL,
    msisdn_hash             = NULL,
    avatar_media_id         = NULL,
    state                   = 3,
    updated_at              = now()
WHERE id = $1;

-- name: ScrubConsentIPs :exec
UPDATE consents SET ip_hash = NULL WHERE user_id = $1;

-- name: DeleteUserOTPs :exec
DELETE FROM otp_challenges WHERE user_id = $1;

-- name: ListIncompleteErasures :many
SELECT request_id, user_id, state, requested_at, completion_by, overdue,
       unfinished_steps, failed_steps, last_error
FROM dpo_incomplete_erasures
ORDER BY overdue DESC, completion_by;

-- name: GetOpenErasure :one
SELECT public_id, state, requested_at, completion_by FROM erasure_requests WHERE user_id = $1 AND state IN (1, 3);
