-- T-1.1.1.7 — schema test for migration 000003_outbox. Rolls back.

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

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_outbox_unpublished' AND indexdef LIKE '%WHERE (published_at IS NULL)%') THEN
        RAISE EXCEPTION 'missing partial index idx_outbox_unpublished';
    END IF;
END $$;

INSERT INTO outbox (topic, partition_key, event_id, payload)
VALUES ('user.registered', '1', '01900000-0000-7000-8000-000000000001', '{"specversion":"1.0"}');

DO $$
BEGIN
    IF (SELECT attempts FROM outbox WHERE partition_key = '1') <> 0
       OR (SELECT published_at FROM outbox WHERE partition_key = '1') IS NOT NULL THEN
        RAISE EXCEPTION 'new rows must be unpublished with 0 attempts';
    END IF;
END $$;

SELECT pg_temp.expect_error($q$
    INSERT INTO outbox (topic, partition_key, event_id, payload)
    VALUES ('user.registered', '2', '01900000-0000-7000-8000-000000000001', '{}')
$q$, '23505');  -- event_id unique

SELECT pg_temp.expect_error($q$
    INSERT INTO outbox (topic, partition_key, event_id, payload)
    VALUES ('', '3', '01900000-0000-7000-8000-000000000003', '{}')
$q$, '23514');  -- topic required

SELECT pg_temp.expect_error($q$
    INSERT INTO outbox (topic, partition_key, event_id, payload)
    VALUES ('user.registered', '', '01900000-0000-7000-8000-000000000004', '{}')
$q$, '23514');  -- key required

SELECT pg_temp.expect_error($q$
    INSERT INTO outbox (topic, partition_key, event_id, payload)
    VALUES ('user.registered', '5', '01900000-0000-7000-8000-000000000005', 'not json')
$q$, '22P02');  -- payload must be JSON

ROLLBACK;

\echo 'PASS 000003_outbox'
