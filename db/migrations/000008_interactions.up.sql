-- Feature 2.1.3 — likes and replies (LLD v2.0 §2.3).
--
-- Deviation from the LLD: its UNIQUE (post_id, user_id) likes index can't
-- exist on a table partitioned by created_at (unique indexes must include
-- the partition key). Likes are hash-partitioned by post_id instead, so the
-- primary key enforces one like per user per post.
--
-- Replies are posts: same moderation, tombstones and length rules. They
-- inherit the thread root's level and audience, and are excluded from feeds.

ALTER TABLE posts
    ADD COLUMN root_id   BIGINT,   -- top-level post of the thread (NULL for top-level posts)
    ADD COLUMN parent_id BIGINT,   -- post or reply being answered
    ADD CONSTRAINT posts_thread_consistent CHECK ((root_id IS NULL) = (parent_id IS NULL));

CREATE INDEX idx_posts_thread ON posts (root_id, created_at, id) WHERE root_id IS NOT NULL;

-- Feeds show top-level posts only.
DROP INDEX idx_feed_ward, idx_feed_const, idx_feed_county, idx_feed_nat;
CREATE INDEX idx_feed_ward ON posts (ward_id, score DESC) WHERE level = 1 AND state = 1 AND root_id IS NULL;
CREATE INDEX idx_feed_const ON posts (constituency_id, score DESC) WHERE level = 2 AND state = 1 AND root_id IS NULL;
CREATE INDEX idx_feed_county ON posts (county_id, score DESC) WHERE level = 3 AND state = 1 AND root_id IS NULL;
CREATE INDEX idx_feed_nat ON posts (score DESC) WHERE level = 4 AND state = 1 AND root_id IS NULL;

CREATE TABLE post_likes (
    post_id    BIGINT      NOT NULL,
    user_id    BIGINT      NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
) PARTITION BY HASH (post_id);

-- Everyone who liked or replied, for the scoring's diversity factor.
CREATE TABLE post_actors (
    post_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users (id),
    PRIMARY KEY (post_id, user_id)
) PARTITION BY HASH (post_id);

DO $$
BEGIN
    FOR i IN 0..7 LOOP
        EXECUTE format('CREATE TABLE post_likes_p%s PARTITION OF post_likes FOR VALUES WITH (MODULUS 8, REMAINDER %s)', i, i);
        EXECUTE format('CREATE TABLE post_actors_p%s PARTITION OF post_actors FOR VALUES WITH (MODULUS 8, REMAINDER %s)', i, i);
    END LOOP;
END $$;

CREATE TABLE post_counters (
    post_id             BIGINT      PRIMARY KEY,
    like_count          INT         NOT NULL DEFAULT 0,
    reply_count         INT         NOT NULL DEFAULT 0,
    unique_actors       INT         NOT NULL DEFAULT 0,
    last_interaction_at TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT post_counters_nonneg CHECK (like_count >= 0 AND reply_count >= 0 AND unique_actors >= 0)
);
