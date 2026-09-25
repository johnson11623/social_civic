-- Story 1.1.3.2 — schema test for migration 000005_erasure. Rolls back.

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

INSERT INTO users (national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
VALUES (decode(repeat('ab', 32), 'hex'), 'v1', 'Erase me', 551, 111, 22);

INSERT INTO erasure_requests (public_id, user_id, requested_at, ack_by, completion_by)
SELECT '01900000-0000-7000-8000-000000000001', id, now(), now() + interval '48 hours', now() + interval '30 days' FROM users;
INSERT INTO erasure_steps (request_id, step) SELECT id, 'identity.anonymize' FROM erasure_requests;

-- The view lists the open request as not overdue, with its unfinished step.
DO $$
BEGIN
    IF (SELECT count(*) FROM dpo_incomplete_erasures WHERE NOT overdue AND unfinished_steps = ARRAY['identity.anonymize']) <> 1 THEN
        RAISE EXCEPTION 'dpo_incomplete_erasures should list the pending request';
    END IF;
END $$;

SELECT pg_temp.expect_error($q$
    INSERT INTO erasure_requests (public_id, user_id, requested_at, ack_by, completion_by)
    SELECT gen_random_uuid(), id, now(), now() + interval '48 hours', now() + interval '30 days' FROM users
$q$, '23505');  -- one open request per user

SELECT pg_temp.expect_error($q$
    UPDATE erasure_requests SET state = 2
$q$, '23514');  -- completed requires completed_at

SELECT pg_temp.expect_error($q$
    UPDATE erasure_requests SET completion_by = requested_at
$q$, '23514');  -- deadlines must be ordered

SELECT pg_temp.expect_error($q$
    UPDATE erasure_requests SET reason = repeat('x', 501)
$q$, '23514');  -- reason length

SELECT pg_temp.expect_error($q$
    INSERT INTO erasure_steps (request_id, step) SELECT id, 'identity.anonymize' FROM erasure_requests
$q$, '23505');  -- step once per request

-- Completing the request removes it from the view and frees the user for a new one.
UPDATE erasure_steps SET state = 2;
UPDATE erasure_requests SET state = 2, completed_at = now();
DO $$
BEGIN
    IF (SELECT count(*) FROM dpo_incomplete_erasures) <> 0 THEN
        RAISE EXCEPTION 'completed requests must not appear on the DPO view';
    END IF;
END $$;

ROLLBACK;

\echo 'PASS 000005_erasure'
