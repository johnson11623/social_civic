-- T-4.4.1.1 — schema test for migration 000002_admin_units. Rolls back.

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

DELETE FROM admin_units WHERE level = 1;
DELETE FROM admin_units WHERE level = 2;
DELETE FROM admin_units WHERE level = 3;
DELETE FROM admin_units;

-- A valid chain: national → county → constituency → ward
INSERT INTO admin_units (level, code, iebc_code, name, display_name, source_version)
VALUES (4, 1, '0', 'NATIONAL', 'National', 't');
INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, source_version)
VALUES (3, 1, '001', 'MOMBASA', 'Mombasa', 4, 1, 't'),
       (2, 1, '001', 'CHANGAMWE', 'Changamwe', 3, 1, 't');
INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, registered_voters, source_version)
VALUES (1, 1, '0001', 'PORT REITZ', 'Port Reitz', 2, 1, 17817, 't');

-- Same code at different levels is allowed (IEBC codes are per level) — done above with code 1 ×4.

SELECT pg_temp.expect_error($q$
    INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, source_version)
    VALUES (1, 1, '0001', 'DUP', 'Dup', 2, 1, 't')
$q$, '23505');  -- duplicate (level, code)

SELECT pg_temp.expect_error($q$
    INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, source_version)
    VALUES (1, 2, '0002', 'ORPHAN', 'Orphan', 2, 999, 't')
$q$, '23503');  -- parent must exist

SELECT pg_temp.expect_error($q$
    INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, source_version)
    VALUES (1, 3, '0003', 'SKIPS', 'Skips', 3, 1, 't')
$q$, '23514');  -- ward's parent must be a constituency

SELECT pg_temp.expect_error($q$
    INSERT INTO admin_units (level, code, iebc_code, name, display_name, source_version)
    VALUES (1, 4, '0004', 'NO PARENT', 'No parent', 't')
$q$, '23514');  -- non-national units need a parent

SELECT pg_temp.expect_error($q$
    INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, source_version)
    VALUES (5, 1, '1', 'X', 'X', NULL, NULL, 't')
$q$, '23514');  -- level out of range

SELECT pg_temp.expect_error($q$
    INSERT INTO admin_units (level, code, iebc_code, name, display_name, parent_level, parent_code, registered_voters, source_version)
    VALUES (1, 5, '0005', 'NEG', 'Neg', 2, 1, -1, 't')
$q$, '23514');  -- voters non-negative

SELECT pg_temp.expect_error($q$
    DELETE FROM admin_units WHERE level = 2 AND code = 1
$q$, '23503');  -- cannot delete a unit that has children

ROLLBACK;

\echo 'PASS 000002_admin_units'
