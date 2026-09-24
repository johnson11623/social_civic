-- Feature 1.2.2 — roles and moderator assignments (LLD v2.0 §2.2).
--
-- Deviation from the LLD: no separate groups table. A user's four groups
-- are their ward, constituency, county and the nation, already denormalized
-- on users (Feature 1.2.1), and each group is an admin unit. Assignments
-- reference admin_units directly, so a group can never drift from the IEBC
-- tree.

CREATE TABLE roles (
    code        TEXT PRIMARY KEY,
    scope_level SMALLINT,               -- 1..4 for moderator roles; NULL = platform-wide
    CONSTRAINT roles_scope_level_valid CHECK (scope_level BETWEEN 1 AND 4),
    CONSTRAINT roles_code_scope_unique UNIQUE (code, scope_level)
);

INSERT INTO roles (code, scope_level) VALUES
    ('ward_mod', 1), ('const_mod', 2), ('county_mod', 3), ('nat_mod', 4),
    ('appeals_member', NULL), ('dpo', NULL), ('sysadmin', NULL);

CREATE TABLE role_assignments (
    id           BIGSERIAL PRIMARY KEY,
    public_id    UUID        NOT NULL UNIQUE,
    user_id      BIGINT      NOT NULL REFERENCES users (id),
    role_code    TEXT        NOT NULL REFERENCES roles (code),
    unit_level   SMALLINT,              -- the group; NULL for platform roles
    unit_code    INT,
    appointed_by BIGINT      REFERENCES users (id),  -- NULL when granted by the admin CLI
    appointed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    term_end     TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    CONSTRAINT role_assignments_unit_pair CHECK ((unit_level IS NULL) = (unit_code IS NULL)),
    CONSTRAINT role_assignments_term CHECK (term_end IS NULL OR term_end > appointed_at),
    CONSTRAINT role_assignments_unit_fk FOREIGN KEY (unit_level, unit_code) REFERENCES admin_units (level, code),
    -- A scoped role sits at its own level (ward_mod on a ward, …); a platform
    -- role has no unit. MATCH SIMPLE skips the check when unit_level is NULL,
    -- and the CHECK below covers that side.
    CONSTRAINT role_assignments_role_level_fk FOREIGN KEY (role_code, unit_level) REFERENCES roles (code, scope_level)
);

-- Platform roles carry no unit.
CREATE FUNCTION role_assignments_platform_unit() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.unit_level IS NULL AND (SELECT scope_level FROM roles WHERE code = NEW.role_code) IS NOT NULL THEN
        RAISE EXCEPTION 'role % needs a unit', NEW.role_code USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER role_assignments_platform_unit BEFORE INSERT OR UPDATE ON role_assignments
    FOR EACH ROW EXECUTE FUNCTION role_assignments_platform_unit();

-- T-1.2.2.4 — one active role per (user, group); one grant of each platform role.
CREATE UNIQUE INDEX uniq_active_role_per_group ON role_assignments (user_id, unit_level, unit_code)
    WHERE revoked_at IS NULL AND unit_level IS NOT NULL;
CREATE UNIQUE INDEX uniq_active_platform_role ON role_assignments (user_id, role_code)
    WHERE revoked_at IS NULL AND unit_level IS NULL;
CREATE INDEX idx_role_assignments_user ON role_assignments (user_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_role_assignments_unit ON role_assignments (unit_level, unit_code) WHERE revoked_at IS NULL;
