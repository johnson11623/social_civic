-- Feature 2.1.4 — schema test for migration 000009_feed. Rolls back.

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

-- Sponsored posts need both labels.
SELECT pg_temp.expect_error($q$INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id, sponsored, label_text_en)
    SELECT gen_random_uuid(), c.id, u.id, 'Ad', 551, 111, 22, TRUE, 'Sponsored' FROM channels c, users u$q$, '23514');
INSERT INTO posts (public_id, channel_id, author_id, content, ward_id, constituency_id, county_id, sponsored, label_text_en, label_text_sw)
SELECT gen_random_uuid(), c.id, u.id, 'Ad', 551, 111, 22, TRUE, 'Sponsored civic message', 'Ujumbe wa kiraia uliofadhiliwa'
FROM channels c, users u;

-- F-07: the flag and labels never change; other columns still can.
SELECT pg_temp.expect_error($q$UPDATE posts SET label_text_sw = 'x' WHERE sponsored$q$, '23514');
SELECT pg_temp.expect_error($q$UPDATE posts SET sponsored = TRUE, label_text_en = 'a', label_text_sw = 'b' WHERE NOT sponsored$q$, '23514');
UPDATE posts SET score = 10;

-- Feed indexes skip sponsored posts and order ties by id.
DO $$
BEGIN
    IF pg_get_indexdef('idx_feed_county'::regclass) NOT LIKE '%NOT sponsored%'
       OR pg_get_indexdef('idx_feed_county'::regclass) NOT LIKE '%id DESC%' THEN
        RAISE EXCEPTION 'idx_feed_county: %', pg_get_indexdef('idx_feed_county'::regclass);
    END IF;
END $$;

ROLLBACK;
\echo 'PASS 000009_feed'
