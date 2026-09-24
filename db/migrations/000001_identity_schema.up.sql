-- T-1.1.1.1 — Identity & Verification schema (LLD v2.0 §2.1, hardened per F-01)
-- National ID is never stored raw: only an HMAC-SHA256 hash plus the KMS key version used.

CREATE TABLE users (
    id                     BIGSERIAL PRIMARY KEY,
    public_id              UUID        NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    national_id_hash       BYTEA       NOT NULL UNIQUE,            -- HMAC-SHA256
    national_id_key_version TEXT       NOT NULL,                   -- F-01: KMS pepper version, enables re-hash on rotation
    display_name           TEXT        NOT NULL,
    preferred_lang         TEXT        NOT NULL DEFAULT 'sw',
    ward_id                INT         NOT NULL,
    constituency_id        INT         NOT NULL,
    county_id              INT         NOT NULL,
    state                  SMALLINT    NOT NULL DEFAULT 1,          -- 1=active, 2=suspended, 3=erased
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_national_id_hash_len CHECK (octet_length(national_id_hash) = 32),
    CONSTRAINT users_preferred_lang_valid CHECK (preferred_lang IN ('en', 'sw')),
    CONSTRAINT users_state_valid          CHECK (state IN (1, 2, 3)),
    CONSTRAINT users_display_name_len     CHECK (char_length(display_name) BETWEEN 1 AND 100)
);

CREATE INDEX idx_users_ward ON users (ward_id) WHERE state = 1;

CREATE TABLE consents (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (id),
    version      TEXT        NOT NULL,
    granted_at   TIMESTAMPTZ NOT NULL,
    withdrawn_at TIMESTAMPTZ,
    ip_hash      BYTEA,
    UNIQUE (user_id, version),
    CONSTRAINT consents_withdrawn_after_granted CHECK (withdrawn_at IS NULL OR withdrawn_at >= granted_at)
);

CREATE TABLE verification_attempts (
    id           BIGSERIAL PRIMARY KEY,
    id_hash      BYTEA       NOT NULL,
    outcome      SMALLINT    NOT NULL,                              -- 1=success, 2=fail, 3=error
    reason_code  TEXT,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT verification_attempts_outcome_valid CHECK (outcome IN (1, 2, 3))
);

CREATE INDEX idx_verif_hash ON verification_attempts (id_hash, attempted_at DESC);
