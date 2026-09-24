-- Feature 1.1.2 — schema test for migration 000004_auth_sessions. Rolls back.

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

INSERT INTO users (national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id,
                   msisdn_ciphertext, msisdn_key_version, msisdn_hash)
VALUES (decode(repeat('ab', 32), 'hex'), 'v1', 'With phone', 551, 111, 22, '\x01', 'pii-v1', decode(repeat('cd', 32), 'hex'));

-- Phone fields are all-or-nothing
SELECT pg_temp.expect_error($q$
    INSERT INTO users (national_id_hash, national_id_key_version, display_name, ward_id, constituency_id, county_id, msisdn_ciphertext)
    VALUES (decode(repeat('ef', 32), 'hex'), 'v1', 'Half phone', 551, 111, 22, '\x01')
$q$, '23514');

-- OTP challenges
INSERT INTO otp_challenges (user_id, code_hash, key_version, created_at, expires_at)
SELECT id, decode(repeat('11', 32), 'hex'), 'v1', now(), now() + interval '5 minutes' FROM users WHERE display_name = 'With phone';

SELECT pg_temp.expect_error($q$
    INSERT INTO otp_challenges (user_id, code_hash, key_version, created_at, expires_at)
    SELECT id, '\x01', 'v1', now(), now() + interval '5 minutes' FROM users WHERE display_name = 'With phone'
$q$, '23514');  -- code hash must be 32 bytes

SELECT pg_temp.expect_error($q$
    INSERT INTO otp_challenges (user_id, code_hash, key_version, created_at, expires_at)
    SELECT id, decode(repeat('22', 32), 'hex'), 'v1', now(), now() - interval '1 second' FROM users WHERE display_name = 'With phone'
$q$, '23514');  -- must expire after creation

SELECT pg_temp.expect_error($q$
    INSERT INTO otp_challenges (user_id, code_hash, key_version, created_at, expires_at)
    VALUES (999999999, decode(repeat('33', 32), 'hex'), 'v1', now(), now() + interval '5 minutes')
$q$, '23503');  -- user must exist

-- Refresh tokens
INSERT INTO refresh_tokens (jti, user_id, family_id, issued_at, expires_at)
SELECT '01900000-0000-7000-8000-000000000001', id, '01900000-0000-7000-8000-0000000000f1', now(), now() + interval '30 days'
FROM users WHERE display_name = 'With phone';

SELECT pg_temp.expect_error($q$
    UPDATE refresh_tokens SET revoked_at = now() WHERE jti = '01900000-0000-7000-8000-000000000001'
$q$, '23514');  -- revocation needs a reason

SELECT pg_temp.expect_error($q$
    INSERT INTO refresh_tokens (jti, user_id, family_id, issued_at, expires_at)
    SELECT '01900000-0000-7000-8000-000000000001', id, gen_random_uuid(), now(), now() + interval '1 day' FROM users WHERE display_name = 'With phone'
$q$, '23505');  -- jti unique

SELECT pg_temp.expect_error($q$
    INSERT INTO refresh_tokens (jti, user_id, family_id, issued_at, expires_at)
    SELECT gen_random_uuid(), id, gen_random_uuid(), now(), now() - interval '1 day' FROM users WHERE display_name = 'With phone'
$q$, '23514');  -- must expire after issue

ROLLBACK;

\echo 'PASS 000004_auth_sessions'
