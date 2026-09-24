-- name: GetActiveUserByPublicID :one
SELECT id, ward_id, constituency_id, county_id FROM users WHERE public_id = $1 AND state = 1;

-- name: InsertChannel :one
INSERT INTO channels (public_id, ward_id, creator_id, name, description, category)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, created_at;

-- name: ListWardChannels :many
-- #general first, then by name.
SELECT public_id, ward_id, name, description, category, read_only, created_at
FROM channels
WHERE ward_id = $1 AND state = 1
ORDER BY (name = 'general') DESC, name;

-- name: GetChannelByPublicID :one
SELECT id, public_id, ward_id, name, description, category, read_only, state, created_at
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
INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at;

-- name: GetPostByPublicID :one
SELECT p.id, p.public_id, p.content, p.level, p.ward_id, p.constituency_id, p.county_id, p.score,
       p.state, p.created_at, c.public_id AS channel_public_id, c.name AS channel_name,
       u.public_id AS author_public_id, u.display_name AS author_display_name
FROM posts p
JOIN channels c ON c.id = p.channel_id
JOIN users u ON u.id = p.author_id
WHERE p.public_id = $1;
