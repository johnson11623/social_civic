-- name: InsertReport :one
INSERT INTO reports (public_id, post_id, reporter_id, reason_code, details, post_level)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at;

-- name: ModerationQueue :many
-- Reported posts the caller may act on (tiered authority, as
-- membership.Roles.CanModerate), one row per post.
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.constituency_id, p.county_id, p.state, p.created_at,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       count(r.id)::int AS report_count,
       min(r.created_at)::timestamptz AS first_reported_at,
       array_agg(DISTINCT r.reason_code)::text[] AS reasons
FROM reports r
JOIN posts p ON p.id = r.post_id
JOIN users u ON u.id = p.author_id
WHERE r.state = 1
  AND p.state = ANY(sqlc.arg(post_states)::smallint[])
  AND ((p.level <= 1 AND p.ward_id = ANY(sqlc.arg(wards)::int[]))
    OR (p.level <= 2 AND p.constituency_id = ANY(sqlc.arg(constituencies)::int[]))
    OR (p.level <= 3 AND p.county_id = ANY(sqlc.arg(counties)::int[]))
    OR sqlc.arg(national)::bool)
  AND (sqlc.arg(level)::smallint = 0 OR p.level = sqlc.arg(level)::smallint)
GROUP BY p.id, p.public_id, p.content, p.level, p.ward_id, p.constituency_id, p.county_id, p.state, p.created_at,
         u.public_id, u.display_name
ORDER BY CASE WHEN sqlc.arg(by_reports)::bool THEN count(r.id) END DESC NULLS LAST, min(r.created_at), p.id
LIMIT 100;

-- name: ResolveOpenReports :execrows
UPDATE reports SET state = sqlc.arg(state), resolved_at = now() WHERE post_id = sqlc.arg(post_id) AND state = 1;

-- name: InsertModerationAction :one
INSERT INTO moderation_actions (public_id, post_id, post_public_id, actor_id, action, reason_code, notes,
                                scope_level, previous_state, new_state, appeal_due_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, created_at;

-- name: SetPostState :exec
-- deleted_at marks removals (hide/delete); restoring clears it.
UPDATE posts SET state = sqlc.arg(state),
                 deleted_at = CASE WHEN sqlc.arg(state)::smallint = 3 THEN now() ELSE NULL END
WHERE id = sqlc.arg(post_id);

-- name: ListPostActions :many
SELECT a.public_id, a.action, a.reason_code, a.notes, a.scope_level, a.previous_state, a.new_state,
       a.appeal_due_at, a.overturned_at, a.created_at, u.public_id AS actor_public_id, u.display_name AS actor_display_name
FROM moderation_actions a
JOIN users u ON u.id = a.actor_id
WHERE a.post_id = $1
ORDER BY a.created_at DESC, a.id DESC;

-- name: GetActionForAppeal :one
SELECT a.id, a.public_id, a.post_id, a.post_public_id, a.action, a.reason_code, a.previous_state, a.new_state,
       a.appeal_due_at, a.overturned_at, a.actor_id, p.author_id, p.level, p.ward_id, p.constituency_id, p.county_id
FROM moderation_actions a
JOIN posts p ON p.id = a.post_id
WHERE a.public_id = $1;

-- name: InsertAppeal :one
INSERT INTO appeals (public_id, action_id, appellant_id, statement, due_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, filed_at;

-- name: GetAppeal :one
SELECT ap.id, ap.public_id, ap.state, ap.statement, ap.filed_at, ap.due_at, ap.appellant_id,
       a.id AS action_id, a.public_id AS action_public_id, a.post_id, a.post_public_id, a.action, a.reason_code,
       a.previous_state, a.new_state, a.actor_id,
       p.author_id, p.level, p.ward_id, p.constituency_id, p.county_id, p.state AS post_state
FROM appeals ap
JOIN moderation_actions a ON a.id = ap.action_id
JOIN posts p ON p.id = a.post_id
WHERE ap.public_id = $1
FOR UPDATE OF ap;

-- name: DecideAppeal :exec
UPDATE appeals SET state = $2, decided_at = now(), decided_by = $3, decision_notes = $4 WHERE id = $1;

-- name: MarkActionOverturned :exec
UPDATE moderation_actions SET overturned_at = now() WHERE id = $1 AND overturned_at IS NULL;

-- name: ListOpenAppeals :many
SELECT ap.public_id, ap.statement, ap.filed_at, ap.due_at,
       a.public_id AS action_public_id, a.post_public_id, a.action, a.reason_code, a.scope_level,
       p.content, u.display_name AS moderator_display_name
FROM appeals ap
JOIN moderation_actions a ON a.id = ap.action_id
JOIN posts p ON p.id = a.post_id
JOIN users u ON u.id = a.actor_id
WHERE ap.state = 1
ORDER BY ap.due_at, ap.id
LIMIT 100;
