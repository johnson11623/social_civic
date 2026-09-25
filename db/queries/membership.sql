-- name: GetUserScopeByPublicID :one
SELECT id, public_id, ward_id, constituency_id, county_id FROM users WHERE public_id = $1 AND state = 1;

-- name: GetUserScopeByID :one
SELECT id, public_id, ward_id, constituency_id, county_id FROM users WHERE id = $1 AND state = 1;

-- name: InsertRoleAssignment :one
INSERT INTO role_assignments (public_id, user_id, role_code, unit_level, unit_code, appointed_by, term_end)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, appointed_at;

-- name: ActiveRoles :many
-- Roles in force now: not revoked and within their term.
SELECT public_id, role_code, unit_level, unit_code, appointed_at, term_end
FROM role_assignments
WHERE user_id = $1 AND revoked_at IS NULL AND (term_end IS NULL OR term_end > now())
ORDER BY unit_level NULLS FIRST, role_code;

-- name: RevokeUserRoles :execrows
UPDATE role_assignments SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;
