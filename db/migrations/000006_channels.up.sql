-- Feature 2.1.1 — ward channels (LLD v2.0 §2.3; Web App Design v2.0 §3.3–3.4).
-- A channel belongs to exactly one ward; names are unique within the ward.
-- No private channels: every channel is visible to the whole ward.

CREATE TABLE channels (
    id          BIGSERIAL   PRIMARY KEY,
    public_id   UUID        NOT NULL UNIQUE,
    ward_id     INT         NOT NULL,                        -- IEBC ward code
    ward_level  SMALLINT    GENERATED ALWAYS AS (1) STORED,  -- lets the FK target admin_units(level, code)
    creator_id  BIGINT      REFERENCES users (id),           -- NULL for platform channels (#general)
    name        TEXT        NOT NULL,                        -- lowercase words joined by hyphens
    description TEXT,
    category    SMALLINT    NOT NULL DEFAULT 1,              -- 1=general 2=services 3=opportunities 4=safety 5=culture
    read_only   BOOLEAN     NOT NULL DEFAULT FALSE,          -- only moderators post (e.g. #announcements)
    state       SMALLINT    NOT NULL DEFAULT 1,              -- 1=active 2=archived
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (ward_level, ward_id) REFERENCES admin_units (level, code),
    CONSTRAINT channels_name_unique_in_ward UNIQUE (ward_id, name),
    CONSTRAINT channels_name_format CHECK (name ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(name) BETWEEN 2 AND 40),
    CONSTRAINT channels_description_len CHECK (description IS NULL OR char_length(description) <= 140),
    CONSTRAINT channels_category_valid CHECK (category BETWEEN 1 AND 5),
    CONSTRAINT channels_state_valid CHECK (state IN (1, 2))
);

CREATE INDEX idx_channels_ward_active ON channels (ward_id, name) WHERE state = 1;
