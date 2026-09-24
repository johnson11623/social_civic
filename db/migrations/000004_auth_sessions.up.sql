-- Feature 1.1.2 — login with SMS one-time codes, and refresh-token sessions.

-- Phone number for SMS login codes. PII: stored encrypted (AES-256-GCM, key
-- version recorded for rotation) plus an HMAC for lookups (e.g. USSD). Not
-- unique: households share phones. Nullable only for rows created before
-- this migration; registration now requires it.
ALTER TABLE users
    ADD COLUMN msisdn_ciphertext  BYTEA,
    ADD COLUMN msisdn_key_version TEXT,
    ADD COLUMN msisdn_hash        BYTEA,
    ADD CONSTRAINT users_msisdn_complete CHECK (
        (msisdn_ciphertext IS NULL AND msisdn_key_version IS NULL AND msisdn_hash IS NULL)
        OR (msisdn_ciphertext IS NOT NULL AND msisdn_key_version IS NOT NULL AND octet_length(msisdn_hash) = 32)
    );

CREATE INDEX idx_users_msisdn_hash ON users (msisdn_hash) WHERE msisdn_hash IS NOT NULL;

-- T-1.1.2.1/T-1.1.2.3 — one-time login codes. Only an HMAC of the code is stored.
CREATE TABLE otp_challenges (
    id             BIGSERIAL   PRIMARY KEY,
    user_id        BIGINT      NOT NULL REFERENCES users (id),
    code_hash      BYTEA       NOT NULL,
    key_version    TEXT        NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    attempts       INT         NOT NULL DEFAULT 0,
    consumed_at    TIMESTAMPTZ,              -- used successfully
    invalidated_at TIMESTAMPTZ,              -- superseded by a newer code or locked out
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT otp_code_hash_len   CHECK (octet_length(code_hash) = 32),
    CONSTRAINT otp_attempts_nonneg CHECK (attempts >= 0),
    CONSTRAINT otp_expires_after_created CHECK (expires_at > created_at)
);

CREATE INDEX idx_otp_active ON otp_challenges (user_id, id DESC)
    WHERE consumed_at IS NULL AND invalidated_at IS NULL;

-- T-1.1.2.4/T-1.1.2.5 — refresh tokens (F-03: rotation with reuse detection).
-- Each login starts a family; each refresh revokes the presented token and
-- issues its successor in the same family.
CREATE TABLE refresh_tokens (
    jti           UUID        PRIMARY KEY,
    user_id       BIGINT      NOT NULL REFERENCES users (id),
    family_id     UUID        NOT NULL,
    issued_at     TIMESTAMPTZ NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    revoke_reason TEXT,                      -- 'rotated', 'reuse_detected', 'logout', 'admin'
    replaced_by   UUID,
    CONSTRAINT refresh_expires_after_issued CHECK (expires_at > issued_at),
    CONSTRAINT refresh_revocation_complete CHECK ((revoked_at IS NULL) = (revoke_reason IS NULL))
);

CREATE INDEX idx_refresh_user_active ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_refresh_family ON refresh_tokens (family_id);
