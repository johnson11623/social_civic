-- EPIC 3.1 — schema test for migration 000011_moderation. Rolls back.

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

-- Reports: harm-based reasons only, once per user and post.
SELECT pg_temp.expect_error($q$INSERT INTO reports (public_id, post_id, reporter_id, reason_code, post_level)
    SELECT gen_random_uuid(), p.id, u.id, 'spam', 1 FROM posts p, users u$q$, '23514');
INSERT INTO reports (public_id, post_id, reporter_id, reason_code, post_level)
SELECT gen_random_uuid(), p.id, u.id, 'hate_speech', 1 FROM posts p, users u;
SELECT pg_temp.expect_error($q$INSERT INTO reports (public_id, post_id, reporter_id, reason_code, post_level)
    SELECT gen_random_uuid(), p.id, u.id, 'privacy', 1 FROM posts p, users u$q$, '23505');

-- Actions are immutable, except marking them overturned once.
INSERT INTO moderation_actions (public_id, post_id, post_public_id, actor_id, action, reason_code, scope_level,
                                previous_state, new_state, appeal_due_at)
SELECT gen_random_uuid(), p.id, p.public_id, u.id, 'hide', 'hate_speech', 1, 1, 3, now() + interval '14 days'
FROM posts p, users u;
SELECT pg_temp.expect_error($q$UPDATE moderation_actions SET reason_code = 'privacy'$q$, '23514');
SELECT pg_temp.expect_error($q$DELETE FROM moderation_actions$q$, '23514');
SELECT pg_temp.expect_error($q$INSERT INTO moderation_actions (public_id, post_id, post_public_id, actor_id, action,
    reason_code, scope_level, previous_state, new_state) SELECT gen_random_uuid(), p.id, p.public_id, u.id, 'ban',
    'hate_speech', 1, 1, 3 FROM posts p, users u$q$, '23514');
UPDATE moderation_actions SET overturned_at = now();
SELECT pg_temp.expect_error($q$UPDATE moderation_actions SET overturned_at = now() + interval '1 day'$q$, '23514');

-- One appeal per action; decided appeals carry decided_at.
INSERT INTO appeals (public_id, action_id, appellant_id, statement, due_at)
SELECT gen_random_uuid(), a.id, u.id, 'This was a report of a broken tap.', now() + interval '14 days'
FROM moderation_actions a, users u;
SELECT pg_temp.expect_error($q$INSERT INTO appeals (public_id, action_id, appellant_id, statement, due_at)
    SELECT gen_random_uuid(), a.id, u.id, 'again', now() FROM moderation_actions a, users u$q$, '23505');
SELECT pg_temp.expect_error($q$UPDATE appeals SET state = 3$q$, '23514');
UPDATE appeals SET state = 3, decided_at = now();

ROLLBACK;
\echo 'PASS 000011_moderation'
