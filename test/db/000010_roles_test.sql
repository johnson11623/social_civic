-- Feature 1.2.2 — schema test for migration 000010_roles. Rolls back.

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
-- Seven roles, moderator roles bound to their level.
DO $$
BEGIN
    IF (SELECT count(*) FROM roles) <> 7 OR (SELECT scope_level FROM roles WHERE code = 'county_mod') <> 3 THEN
        RAISE EXCEPTION 'role catalog wrong';
    END IF;
END $$;

-- A role sits at its own level; units must exist; platform roles carry no unit.
SELECT pg_temp.expect_error($q$INSERT INTO role_assignments (public_id, user_id, role_code, unit_level, unit_code)
    SELECT gen_random_uuid(), id, 'county_mod', 1, 551 FROM users$q$, '23503');
SELECT pg_temp.expect_error($q$INSERT INTO role_assignments (public_id, user_id, role_code, unit_level, unit_code)
    SELECT gen_random_uuid(), id, 'const_mod', 2, 999 FROM users$q$, '23503');
SELECT pg_temp.expect_error($q$INSERT INTO role_assignments (public_id, user_id, role_code, unit_level, unit_code)
    SELECT gen_random_uuid(), id, 'sysadmin', 1, 551 FROM users$q$, '23503');
SELECT pg_temp.expect_error($q$INSERT INTO role_assignments (public_id, user_id, role_code)
    SELECT gen_random_uuid(), id, 'ward_mod' FROM users$q$, '23514');
SELECT pg_temp.expect_error($q$INSERT INTO role_assignments (public_id, user_id, role_code, unit_level)
    SELECT gen_random_uuid(), id, 'ward_mod', 1 FROM users$q$, '23514');

INSERT INTO role_assignments (public_id, user_id, role_code, unit_level, unit_code)
SELECT gen_random_uuid(), id, 'ward_mod', 1, 551 FROM users;

-- One active role per (user, group); revoked roles don't count.
SELECT pg_temp.expect_error($q$INSERT INTO role_assignments (public_id, user_id, role_code, unit_level, unit_code)
    SELECT gen_random_uuid(), id, 'ward_mod', 1, 551 FROM users$q$, '23505');
UPDATE role_assignments SET revoked_at = now();
INSERT INTO role_assignments (public_id, user_id, role_code, unit_level, unit_code)
SELECT gen_random_uuid(), id, 'ward_mod', 1, 551 FROM users;

-- Platform roles: once each.
INSERT INTO role_assignments (public_id, user_id, role_code) SELECT gen_random_uuid(), id, 'sysadmin' FROM users;
SELECT pg_temp.expect_error($q$INSERT INTO role_assignments (public_id, user_id, role_code)
    SELECT gen_random_uuid(), id, 'sysadmin' FROM users$q$, '23505');

ROLLBACK;
\echo 'PASS 000010_roles'
