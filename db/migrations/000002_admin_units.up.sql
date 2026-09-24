-- T-4.4.1.1 — Administrative boundary tree (IEBC 2012 delimitation, 2022 register).
-- Units are keyed by (level, code): IEBC codes are unique only within a level,
-- e.g. county 012, constituency 012 and ward 0012 are different places.
-- Levels match groups.level: 1=ward, 2=constituency, 3=county, 4=national.

CREATE TABLE admin_units (
    level             SMALLINT    NOT NULL,
    code              INT         NOT NULL,
    iebc_code         TEXT        NOT NULL,              -- zero-padded official code, e.g. '0017'
    name              TEXT        NOT NULL,              -- IEBC name (uppercase), canonical for matching
    display_name      TEXT        NOT NULL,              -- readable name, e.g. 'Ziwa la Ng''ombe'
    parent_level      SMALLINT,
    parent_code       INT,
    registered_voters INT,                               -- 2022 register; wards only
    source_version    TEXT        NOT NULL,              -- e.g. 'iebc-2022'
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (level, code),
    FOREIGN KEY (parent_level, parent_code) REFERENCES admin_units (level, code),
    CONSTRAINT admin_units_level_valid  CHECK (level BETWEEN 1 AND 4),
    CONSTRAINT admin_units_code_positive CHECK (code > 0),
    CONSTRAINT admin_units_parent_valid CHECK (
        (level = 4 AND parent_level IS NULL AND parent_code IS NULL)
        OR (level < 4 AND parent_level = level + 1 AND parent_code IS NOT NULL)
    ),
    CONSTRAINT admin_units_voters_nonneg CHECK (registered_voters IS NULL OR registered_voters >= 0),
    CONSTRAINT admin_units_names_present CHECK (name <> '' AND display_name <> '')
);

CREATE INDEX idx_admin_units_parent ON admin_units (parent_level, parent_code);
