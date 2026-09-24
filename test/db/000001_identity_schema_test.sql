-- T-1.1.1.1 — schema test for migration 000001_identity_schema.
-- Runs in a transaction and rolls back; any failed assertion aborts with a non-zero exit.

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

-- Tables exist
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['users', 'consents', 'verification_attempts'] LOOP
        IF to_regclass('public.' || t) IS NULL THEN
            RAISE EXCEPTION 'missing table %', t;
        END IF;
    END LOOP;
END $$;

-- Indexes exist, including the partial index on active users
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_users_ward' AND indexdef LIKE '%WHERE (state = 1)%') THEN
        RAISE EXCEPTION 'missing partial index idx_users_ward';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_verif_hash' AND indexdef LIKE '%(id_hash, attempted_at DESC)%') THEN
        RAISE EXCEPTION 'missing index idx_verif_hash';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'users_national_id_hash_key') THEN
        RAISE EXCEPTION 'missing unique index on users.national_id_hash';
    END IF;
END $$;

-- Valid rows are accepted
INSERT INTO users (national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
VALUES (decode(repeat('ab', 32), 'hex'), 'v1', 'Wanjiku M.', 1203, 145, 12);
INSERT INTO consents (user_id, version, granted_at)
SELECT id, '2026-01', now() FROM users WHERE display_name = 'Wanjiku M.';
INSERT INTO verification_attempts (id_hash, outcome) VALUES (decode(repeat('ab', 32), 'hex'), 1);

-- Defaults
DO $$
BEGIN
    IF (SELECT preferred_lang FROM users WHERE display_name = 'Wanjiku M.') <> 'sw' THEN
        RAISE EXCEPTION 'preferred_lang should default to sw';
    END IF;
    IF (SELECT state FROM users WHERE display_name = 'Wanjiku M.') <> 1 THEN
        RAISE EXCEPTION 'state should default to 1 (active)';
    END IF;
END $$;

-- Constraints are enforced
SELECT pg_temp.expect_error($q$
    INSERT INTO users (national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
    VALUES (decode(repeat('ab', 32), 'hex'), 'v1', 'Duplicate', 1203, 145, 12)
$q$, '23505');  -- duplicate national ID

SELECT pg_temp.expect_error($q$
    INSERT INTO consents (user_id, version, granted_at)
    SELECT id, '2026-01', now() FROM users WHERE display_name = 'Wanjiku M.'
$q$, '23505');  -- duplicate consent version

SELECT pg_temp.expect_error($q$
    INSERT INTO users (national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id)
    VALUES ('\x01', 'v1', 'Short hash', 1203, 145, 12)
$q$, '23514');  -- hash must be 32 bytes

SELECT pg_temp.expect_error($q$
    INSERT INTO users (national_id_hash, display_name, ward_id, constituency_id, county_id)
    VALUES (decode(repeat('cd', 32), 'hex'), 'No key version', 1203, 145, 12)
$q$, '23502');  -- key version required (F-01)

SELECT pg_temp.expect_error($q$
    INSERT INTO users (national_id_hash, national_id_key_version, display_name, preferred_lang, ward_id, constituency_id, county_id)
    VALUES (decode(repeat('cd', 32), 'hex'), 'v1', 'French', 'fr', 1203, 145, 12)
$q$, '23514');  -- lang must be en or sw

SELECT pg_temp.expect_error($q$
    INSERT INTO users (national_id_hash, national_id_key_version, display_name, state, ward_id, constituency_id, county_id)
    VALUES (decode(repeat('cd', 32), 'hex'), 'v1', 'Bad state', 9, 1203, 145, 12)
$q$, '23514');  -- invalid state

SELECT pg_temp.expect_error($q$
    INSERT INTO consents (user_id, version, granted_at) VALUES (999999999, '2026-01', now())
$q$, '23503');  -- consent must reference a user

SELECT pg_temp.expect_error($q$
    INSERT INTO consents (user_id, version, granted_at, withdrawn_at)
    SELECT id, '2026-02', now(), now() - interval '1 day' FROM users WHERE display_name = 'Wanjiku M.'
$q$, '23514');  -- withdrawal cannot precede grant

SELECT pg_temp.expect_error($q$
    INSERT INTO verification_attempts (id_hash, outcome) VALUES ('\x01', 9)
$q$, '23514');  -- invalid outcome

ROLLBACK;

\echo 'PASS 000001_identity_schema'
