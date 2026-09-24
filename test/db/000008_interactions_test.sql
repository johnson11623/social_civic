-- Feature 2.1.3 — schema test for migration 000008_interactions. Rolls back.

BEGIN;

CREATE FUNCTION pg_temp.expect_error(stmt text, expected_state text) RETURNS void AS $$
DECLARE
    raised boolean := false;
BEGIN
    BEGIN
        EXECUTE stmt;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE <> expected_state THEN
            RAISE EXCEPTION 'expected SQLSTATE % but got % (%) for: %', expected_state, SQLSTATE, SQLERRM, stmt;
        END IF;
        raised := true;
    END;
    IF NOT raised THEN
        RAISE EXCEPTION 'expected SQLSTATE % but statement succeeded: %', expected_state, stmt;
    END IF;
END;
$$ LANGUAGE plpgsql;

DELETE FROM admin_units WHERE level = 1; DELETE FROM admin_units WHERE level = 2;
DELETE FROM admin_units WHERE level = 3; DELETE FROM admin_units;
INSERT INTO admin_units (level, code, iebc_code, name, display_name, source_version) VALUES (4, 1, '0', 'NATIONAL', 'National', 't');
INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, source_version)
VALUES (3, 22, '022', 'KIAMBU', 'Kiambu', 4, 1, 't'), (2, 111, '111', 'GATUNDU SOUTH', 'Gatundu South', 3, 22, 't'),
       (1, 551, '0551', 'KIAMWANGI', 'Kiamwangi', 2, 111, 't');
INSERT INTO users (national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
VALUES (decode(repeat('ab', 32), 'hex'), 'v1', 'A', 551, 111, 22);
INSERT INTO channels (public_id, ward_id, name) VALUES (gen_random_uuid(), 551, 'water');

INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id)
SELECT gen_random_uuid(), c.id, u.id, 'Water point broken', 551, 111, 22 FROM channels c, users u;

-- A reply needs both thread ids, or neither.
SELECT pg_temp.expect_error($q$INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id, root_id)
    SELECT gen_random_uuid(), c.id, u.id, 'x', 551, 111, 22, 1 FROM channels c, users u$q$, '23514');
INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id, root_id, parent_id)
SELECT gen_random_uuid(), c.id, u.id, 'Me too', 551, 111, 22, p.id, p.id FROM channels c, users u, posts p;

-- One like per user per post, on hash partitions.
INSERT INTO post_likes (post_id, user_id) SELECT p.id, u.id FROM posts p, users u WHERE p.root_id IS NULL;
SELECT pg_temp.expect_error($q$INSERT INTO post_likes (post_id, user_id) SELECT p.id, u.id FROM posts p, users u WHERE p.root_id IS NULL$q$, '23505');
DO $$
BEGIN
    IF (SELECT tableoid::regclass::text FROM post_likes LIMIT 1) NOT LIKE 'post_likes_p%' THEN
        RAISE EXCEPTION 'like did not land in a hash partition';
    END IF;
    IF (SELECT count(*) FROM pg_inherits WHERE inhparent = 'post_actors'::regclass) <> 8 THEN
        RAISE EXCEPTION 'post_actors should have 8 partitions';
    END IF;
END $$;

-- Counters never go negative.
INSERT INTO post_counters (post_id) SELECT id FROM posts WHERE root_id IS NULL;
SELECT pg_temp.expect_error('UPDATE post_counters SET like_count = -1', '23514');

-- Feed indexes skip replies.
DO $$
BEGIN
    IF pg_get_indexdef('idx_feed_ward'::regclass) NOT LIKE '%root_id IS NULL%' THEN
        RAISE EXCEPTION 'idx_feed_ward must exclude replies';
    END IF;
END $$;

ROLLBACK;
\echo 'PASS 000008_interactions'
