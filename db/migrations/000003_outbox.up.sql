-- T-1.1.1.7 — Transactional outbox. Domain events are written in the same
-- transaction as the state change, then relayed to Kafka by the worker, so an
-- event is published if and only if its transaction committed.

CREATE TABLE outbox (
    id            BIGSERIAL   PRIMARY KEY,               -- relay order
    topic         TEXT        NOT NULL,                  -- e.g. 'user.registered'
    partition_key TEXT        NOT NULL,                  -- keeps per-entity ordering in Kafka
    event_id      UUID        NOT NULL UNIQUE,           -- CloudEvents id; consumers dedupe on it
    payload       JSONB       NOT NULL,                  -- full CloudEvents 1.0 envelope
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ,
    attempts      INT         NOT NULL DEFAULT 0,
    last_error    TEXT,
    CONSTRAINT outbox_topic_present CHECK (topic <> ''),
    CONSTRAINT outbox_key_present   CHECK (partition_key <> '')
);

-- The relay only ever scans unpublished rows, in id order.
CREATE INDEX idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
