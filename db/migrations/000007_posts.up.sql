-- Feature 2.1.2 — posts (LLD v2.0 §2.3), partitioned by month on created_at.
--
-- A post starts at ward level and is visible to its level's audience:
-- 1=ward, 2=constituency, 3=county, 4=national. The origin ward and its
-- ancestors are copied onto the row so feeds never join users.
--
-- Partitioned tables can't carry a UNIQUE constraint without the partition
-- key, so public_id (UUIDv7) is indexed, not constrained; UUIDv7 collisions
-- are not a practical concern. Other tables must not FK into posts.

CREATE TABLE posts (
    id              BIGSERIAL,
    public_id       UUID        NOT NULL,
    channel_id      BIGINT      NOT NULL REFERENCES channels (id),
    author_id       BIGINT      NOT NULL REFERENCES users (id),
    content         TEXT,                    -- NULL once tombstoned/purged
    media_url       TEXT,
    level           SMALLINT    NOT NULL DEFAULT 1,
    ward_id         INT         NOT NULL,    -- origin, immutable
    constituency_id INT         NOT NULL,
    county_id       INT         NOT NULL,
    score           REAL        NOT NULL DEFAULT 0,
    state           SMALLINT    NOT NULL DEFAULT 1,  -- 1=active 2=frozen 3=tombstoned 4=purged
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ,
    PRIMARY KEY (id, created_at),
    CONSTRAINT posts_level_valid CHECK (level BETWEEN 1 AND 4),
    CONSTRAINT posts_state_valid CHECK (state BETWEEN 1 AND 4),
    CONSTRAINT posts_score_nonneg CHECK (score >= 0),
    CONSTRAINT posts_content_len CHECK (content IS NULL OR char_length(content) BETWEEN 1 AND 500),
    CONSTRAINT posts_content_present CHECK (state IN (3, 4) OR content IS NOT NULL)
) PARTITION BY RANGE (created_at);

-- Catches rows for months nobody created a partition for, so inserts never fail.
CREATE TABLE posts_default PARTITION OF posts DEFAULT;

CREATE INDEX idx_posts_public_id ON posts (public_id);
CREATE INDEX idx_posts_author ON posts (author_id, created_at DESC);
CREATE INDEX idx_posts_channel ON posts (channel_id, created_at DESC) WHERE state = 1;
-- One partial index per feed level (LLD §2.3).
CREATE INDEX idx_feed_ward ON posts (ward_id, score DESC) WHERE level = 1 AND state = 1;
CREATE INDEX idx_feed_const ON posts (constituency_id, score DESC) WHERE level = 2 AND state = 1;
CREATE INDEX idx_feed_county ON posts (county_id, score DESC) WHERE level = 3 AND state = 1;
CREATE INDEX idx_feed_nat ON posts (score DESC) WHERE level = 4 AND state = 1;

-- Create monthly partitions from this month through `months_ahead` months on
-- (LLD §13: pre-create the next 3). Idempotent; the worker runs it monthly.
CREATE FUNCTION ensure_post_partitions(months_ahead INT DEFAULT 3) RETURNS INT
LANGUAGE plpgsql AS $$
DECLARE
    first DATE := date_trunc('month', now())::date;
    created INT := 0;
    m DATE;
    name TEXT;
BEGIN
    FOR i IN 0..months_ahead LOOP
        m := (first + make_interval(months => i))::date;
        name := format('posts_%s', to_char(m, 'YYYY_MM'));
        IF to_regclass(name) IS NULL THEN
            EXECUTE format('CREATE TABLE %I PARTITION OF posts FOR VALUES FROM (%L) TO (%L)',
                           name, m, (m + interval '1 month')::date);
            created := created + 1;
        END IF;
    END LOOP;
    RETURN created;
END;
$$;

SELECT ensure_post_partitions(3);
