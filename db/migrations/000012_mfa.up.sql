-- F-08 — TOTP multi-factor authentication for privileged roles.
--
-- The secret is encrypted with the PII key (AES-256-GCM, bound to the user
-- id). last_step records the last accepted TOTP time step so a code can't
-- be replayed.

CREATE TABLE user_mfa (
    user_id     BIGINT      PRIMARY KEY REFERENCES users (id),
    secret_enc  BYTEA       NOT NULL,
    key_version TEXT        NOT NULL,
    enabled_at  TIMESTAMPTZ,              -- NULL while enrolment is pending
    last_step   BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
