-- name: GetActiveUserByPublicID :one
SELECT id, public_id, display_name, ward_id, constituency_id, county_id FROM users WHERE public_id = $1 AND state = 1;

-- name: InsertChannel :one
INSERT INTO channels (public_id, ward_id, creator_id, name, description, category, read_only)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at;

-- name: ListWardChannels :many
-- #general first, then by name.
SELECT public_id, ward_id, creator_id, name, description, category, read_only, created_at
FROM channels
WHERE ward_id = $1 AND state = 1
ORDER BY (name = 'general') DESC, name;

-- name: GetChannelByPublicID :one
SELECT id, public_id, ward_id, creator_id, name, description, category, read_only, state, created_at
FROM channels
WHERE public_id = $1;

-- name: CountActiveWardMembers :one
SELECT count(*)::int FROM users WHERE ward_id = $1 AND state = 1;

-- name: EnsureGeneralChannels :execrows
-- One platform-owned #general channel per ward; idempotent.
INSERT INTO channels (public_id, ward_id, name, description, category)
SELECT gen_random_uuid(), code, 'general', NULL, 1
FROM admin_units
WHERE level = 1
ON CONFLICT (ward_id, name) DO NOTHING;

-- name: InsertPost :one
INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id, media_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, created_at;

-- name: GetAttachableMedia :one
-- Media a post may carry: the author's own, processed and ready.
SELECT id FROM media WHERE public_id = $1 AND owner_id = $2 AND state = 3;

-- name: GetPostByPublicID :one
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.constituency_id, p.county_id, p.score,
       p.state, p.created_at, p.channel_id, p.root_id, p.parent_id,
       p.sponsored, p.label_text_en, p.label_text_sw,
       c.public_id AS channel_public_id, c.name AS channel_name,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       COALESCE(pc.like_count, 0)::int AS like_count, COALESCE(pc.reply_count, 0)::int AS reply_count,
       p.author_id,
       COALESCE(ma.public_id::text, '')::text AS moderation_action_id, COALESCE(ma.action, '')::text AS moderation_action,
       COALESCE(ma.reason_code, '')::text AS moderation_reason,
       ma.appeal_due_at AS moderation_appeal_due_at,
       CASE WHEN md.id IS NULL THEN NULL ELSE jsonb_build_object(
           'id', md.public_id, 'kind', md.kind, 'alt', md.alt_text, 'w', md.width, 'h', md.height,
           'ms', md.duration_ms, 'ph', md.placeholder, 'v', md.variants) END::jsonb AS media
FROM posts p
JOIN channels c ON c.id = p.channel_id
JOIN users u ON u.id = p.author_id
LEFT JOIN post_counters pc ON pc.post_id = p.id
LEFT JOIN media md ON md.id = p.media_id AND md.state = 3
-- The decision behind a frozen or removed post (W1.4.3.5: reason + appeal).
LEFT JOIN LATERAL (
    SELECT public_id, action, reason_code, appeal_due_at
    FROM moderation_actions
    WHERE post_id = p.id AND p.state IN (2, 3) AND action <> 'restore'
    ORDER BY created_at DESC, id DESC
    LIMIT 1
) ma ON TRUE
WHERE p.public_id = $1;

-- name: GetPostPublicIDByID :one
SELECT public_id FROM posts WHERE id = $1;

-- name: HasLiked :one
SELECT EXISTS (SELECT 1 FROM post_likes WHERE post_id = $1 AND user_id = $2) AS liked;

-- name: InsertLike :execrows
INSERT INTO post_likes (post_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: DeleteLike :execrows
DELETE FROM post_likes WHERE post_id = $1 AND user_id = $2;

-- name: InsertActor :execrows
INSERT INTO post_actors (post_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: AddToCounters :one
-- Deltas are applied atomically; the row is created on first interaction.
INSERT INTO post_counters (post_id, like_count, reply_count, unique_actors, last_interaction_at)
VALUES (sqlc.arg(post_id), GREATEST(sqlc.arg(likes)::int, 0), GREATEST(sqlc.arg(replies)::int, 0), GREATEST(sqlc.arg(actors)::int, 0), now())
ON CONFLICT (post_id) DO UPDATE SET
    like_count          = post_counters.like_count + sqlc.arg(likes)::int,
    reply_count         = post_counters.reply_count + sqlc.arg(replies)::int,
    unique_actors       = post_counters.unique_actors + sqlc.arg(actors)::int,
    last_interaction_at = now(),
    updated_at          = now()
RETURNING like_count, reply_count;

-- name: InsertReply :one
INSERT INTO posts (public_id, channel_id, author_id, content, level, ward_id, constituency_id, county_id, root_id, parent_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, created_at;

-- name: ListThread :many
-- Replies of a thread in conversation order, keyset-paginated.
SELECT p.id, p.public_id, p.content, p.state, p.created_at, p.parent_id,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       COALESCE(pc.like_count, 0)::int AS like_count, COALESCE(pc.reply_count, 0)::int AS reply_count
FROM posts p
JOIN users u ON u.id = p.author_id
LEFT JOIN post_counters pc ON pc.post_id = p.id
WHERE p.root_id = sqlc.arg(root_id) AND p.state <> 4
  AND (p.created_at, p.id) > (sqlc.arg(after_time)::timestamptz, sqlc.arg(after_id)::bigint)
ORDER BY p.created_at, p.id
LIMIT sqlc.arg(max_rows);

-- name: PublicIDsByIDs :many
SELECT id, public_id FROM posts WHERE id = ANY(sqlc.arg(ids)::bigint[]);

-- Feature 2.1.4 — one level's organic feed, walking (score, id) downward.
-- Each level has its own query so the planner uses that level's partial index.

-- name: FeedWard :many
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.score, p.created_at,
       c.public_id AS channel_public_id, c.name AS channel_name,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       COALESCE(pc.like_count, 0)::int AS like_count, COALESCE(pc.reply_count, 0)::int AS reply_count,
       CASE WHEN md.id IS NULL THEN NULL ELSE jsonb_build_object(
           'id', md.public_id, 'kind', md.kind, 'alt', md.alt_text, 'w', md.width, 'h', md.height,
           'ms', md.duration_ms, 'ph', md.placeholder, 'v', md.variants) END::jsonb AS media
FROM posts p
JOIN channels c ON c.id = p.channel_id
JOIN users u ON u.id = p.author_id
LEFT JOIN post_counters pc ON pc.post_id = p.id
LEFT JOIN media md ON md.id = p.media_id AND md.state = 3
WHERE p.level = 1 AND p.state = 1 AND p.root_id IS NULL AND NOT p.sponsored
  AND p.ward_id = sqlc.arg(scope_id)::int
  AND (p.score, p.id) < (sqlc.arg(after_score)::real, sqlc.arg(after_id)::bigint)
ORDER BY p.score DESC, p.id DESC
LIMIT sqlc.arg(max_rows);

-- name: FeedConstituency :many
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.score, p.created_at,
       c.public_id AS channel_public_id, c.name AS channel_name,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       COALESCE(pc.like_count, 0)::int AS like_count, COALESCE(pc.reply_count, 0)::int AS reply_count,
       CASE WHEN md.id IS NULL THEN NULL ELSE jsonb_build_object(
           'id', md.public_id, 'kind', md.kind, 'alt', md.alt_text, 'w', md.width, 'h', md.height,
           'ms', md.duration_ms, 'ph', md.placeholder, 'v', md.variants) END::jsonb AS media
FROM posts p
JOIN channels c ON c.id = p.channel_id
JOIN users u ON u.id = p.author_id
LEFT JOIN post_counters pc ON pc.post_id = p.id
LEFT JOIN media md ON md.id = p.media_id AND md.state = 3
WHERE p.level = 2 AND p.state = 1 AND p.root_id IS NULL AND NOT p.sponsored
  AND p.constituency_id = sqlc.arg(scope_id)::int
  AND (p.score, p.id) < (sqlc.arg(after_score)::real, sqlc.arg(after_id)::bigint)
ORDER BY p.score DESC, p.id DESC
LIMIT sqlc.arg(max_rows);

-- name: FeedCounty :many
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.score, p.created_at,
       c.public_id AS channel_public_id, c.name AS channel_name,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       COALESCE(pc.like_count, 0)::int AS like_count, COALESCE(pc.reply_count, 0)::int AS reply_count,
       CASE WHEN md.id IS NULL THEN NULL ELSE jsonb_build_object(
           'id', md.public_id, 'kind', md.kind, 'alt', md.alt_text, 'w', md.width, 'h', md.height,
           'ms', md.duration_ms, 'ph', md.placeholder, 'v', md.variants) END::jsonb AS media
FROM posts p
JOIN channels c ON c.id = p.channel_id
JOIN users u ON u.id = p.author_id
LEFT JOIN post_counters pc ON pc.post_id = p.id
LEFT JOIN media md ON md.id = p.media_id AND md.state = 3
WHERE p.level = 3 AND p.state = 1 AND p.root_id IS NULL AND NOT p.sponsored
  AND p.county_id = sqlc.arg(scope_id)::int
  AND (p.score, p.id) < (sqlc.arg(after_score)::real, sqlc.arg(after_id)::bigint)
ORDER BY p.score DESC, p.id DESC
LIMIT sqlc.arg(max_rows);

-- name: FeedNational :many
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.score, p.created_at,
       c.public_id AS channel_public_id, c.name AS channel_name,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       COALESCE(pc.like_count, 0)::int AS like_count, COALESCE(pc.reply_count, 0)::int AS reply_count,
       CASE WHEN md.id IS NULL THEN NULL ELSE jsonb_build_object(
           'id', md.public_id, 'kind', md.kind, 'alt', md.alt_text, 'w', md.width, 'h', md.height,
           'ms', md.duration_ms, 'ph', md.placeholder, 'v', md.variants) END::jsonb AS media
FROM posts p
JOIN channels c ON c.id = p.channel_id
JOIN users u ON u.id = p.author_id
LEFT JOIN post_counters pc ON pc.post_id = p.id
LEFT JOIN media md ON md.id = p.media_id AND md.state = 3
WHERE p.level = 4 AND p.state = 1 AND p.root_id IS NULL AND NOT p.sponsored
  AND (p.score, p.id) < (sqlc.arg(after_score)::real, sqlc.arg(after_id)::bigint)
ORDER BY p.score DESC, p.id DESC
LIMIT sqlc.arg(max_rows);

-- name: LikedAmong :many
SELECT post_id FROM post_likes WHERE user_id = $1 AND post_id = ANY(sqlc.arg(post_ids)::bigint[]);

-- W2.1.3 — a channel's top-level posts, newest first, keyset on (created_at, id).
-- name: ListChannelPosts :many
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.score, p.created_at,
       p.sponsored, p.label_text_en, p.label_text_sw,
       u.public_id AS author_public_id, u.display_name AS author_display_name,
       COALESCE(pc.like_count, 0)::int AS like_count, COALESCE(pc.reply_count, 0)::int AS reply_count,
       CASE WHEN md.id IS NULL THEN NULL ELSE jsonb_build_object(
           'id', md.public_id, 'kind', md.kind, 'alt', md.alt_text, 'w', md.width, 'h', md.height,
           'ms', md.duration_ms, 'ph', md.placeholder, 'v', md.variants) END::jsonb AS media
FROM posts p
JOIN users u ON u.id = p.author_id
LEFT JOIN post_counters pc ON pc.post_id = p.id
LEFT JOIN media md ON md.id = p.media_id AND md.state = 3
WHERE p.channel_id = sqlc.arg(channel_id) AND p.state = 1 AND p.root_id IS NULL
  AND (p.created_at, p.id) < (sqlc.arg(after_time)::timestamptz, sqlc.arg(after_id)::bigint)
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg(max_rows);
