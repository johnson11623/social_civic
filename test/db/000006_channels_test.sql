-- Feature 2.1.1 — schema test for migration 000006_channels. Rolls back.
-- Needs admin_units rows: inserts a minimal chain.

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

INSERT INTO channels (public_id, ward_id, name) VALUES (gen_random_uuid(), 551, 'water-points');

SELECT pg_temp.expect_error($q$ INSERT INTO channels (public_id, ward_id, name) VALUES (gen_random_uuid(), 551, 'water-points') $q$, '23505');
SELECT pg_temp.expect_error($q$ INSERT INTO channels (public_id, ward_id, name) VALUES (gen_random_uuid(), 9999, 'roads') $q$, '23503');
SELECT pg_temp.expect_error($q$ INSERT INTO channels (public_id, ward_id, name) VALUES (gen_random_uuid(), 551, 'Water Points') $q$, '23514');
SELECT pg_temp.expect_error($q$ INSERT INTO channels (public_id, ward_id, name) VALUES (gen_random_uuid(), 551, 'a') $q$, '23514');
SELECT pg_temp.expect_error($q$ INSERT INTO channels (public_id, ward_id, name, category) VALUES (gen_random_uuid(), 551, 'roads', 9) $q$, '23514');
SELECT pg_temp.expect_error($q$ INSERT INTO channels (public_id, ward_id, name, description) VALUES (gen_random_uuid(), 551, 'roads', repeat('x', 141)) $q$, '23514');
SELECT pg_temp.expect_error($q$ INSERT INTO channels (public_id, ward_id, ward_level, name) VALUES (gen_random_uuid(), 111, 2, 'roads') $q$, '428C9');  -- ward_level is generated

ROLLBACK;

\echo 'PASS 000006_channels'
