-- Feature 2.1.2 — schema test for migration 000007_posts. Rolls back.

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

DO $$
BEGIN
    IF (SELECT tableoid::regclass::text FROM posts LIMIT 1) <> 'posts_' || to_char(now(), 'YYYY_MM') THEN
        RAISE EXCEPTION 'new post did not land in the current month partition';
    END IF;
    IF (SELECT level FROM posts LIMIT 1) <> 1 OR (SELECT state FROM posts LIMIT 1) <> 1 THEN
        RAISE EXCEPTION 'posts must start at ward level, active';
    END IF;
END $$;

-- A row far outside the pre-created months falls into the default partition.
INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id, created_at)
SELECT gen_random_uuid(), c.id, u.id, 'From the future', 551, 111, 22, now() + interval '3 years' FROM channels c, users u;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM posts_default) THEN RAISE EXCEPTION 'default partition missing row'; END IF;
END $$;

SELECT pg_temp.expect_error($q$ INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id, level)
  SELECT gen_random_uuid(), c.id, u.id, 'x', 551, 111, 22, 5 FROM channels c, users u $q$, '23514');   -- level
SELECT pg_temp.expect_error($q$ INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id)
  SELECT gen_random_uuid(), c.id, u.id, repeat('x', 501), 551, 111, 22 FROM channels c, users u $q$, '23514');   -- length
SELECT pg_temp.expect_error($q$ INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id)
  SELECT gen_random_uuid(), c.id, u.id, NULL, 551, 111, 22 FROM channels c, users u $q$, '23514');   -- active needs content
SELECT pg_temp.expect_error($q$ INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id)
  SELECT gen_random_uuid(), 999999, u.id, 'x', 551, 111, 22 FROM users u $q$, '23503');   -- channel FK
SELECT pg_temp.expect_error($q$ UPDATE posts SET score = -1 $q$, '23514');

-- Tombstoning clears content legitimately.
UPDATE posts SET state = 3, content = NULL, deleted_at = now();

ROLLBACK;

\echo 'PASS 000007_posts'
