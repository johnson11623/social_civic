-- Story 1.1.3.2 — right to erasure (DPA 2019 s.40), run as an orchestrated
-- saga: one request, one row per step; the worker executes pending steps.

CREATE TABLE erasure_requests (
    id              BIGSERIAL   PRIMARY KEY,
    public_id       UUID        NOT NULL UNIQUE,
    user_id         BIGINT      NOT NULL REFERENCES users (id),
    reason          TEXT,
    state           SMALLINT    NOT NULL DEFAULT 1,     -- 1=pending, 2=completed, 3=failed (needs DPO action)
    requested_at    TIMESTAMPTZ NOT NULL,
    ack_by          TIMESTAMPTZ NOT NULL,               -- 48 hours (US-11)
    completion_by   TIMESTAMPTZ NOT NULL,               -- 30 days (US-11)
    completed_at    TIMESTAMPTZ,
    retained_fields TEXT[],                             -- what is kept and why, reported to the user
    CONSTRAINT erasure_state_valid CHECK (state IN (1, 2, 3)),
    CONSTRAINT erasure_deadlines CHECK (ack_by > requested_at AND completion_by > ack_by),
    CONSTRAINT erasure_completed_consistent CHECK ((state = 2) = (completed_at IS NOT NULL)),
    CONSTRAINT erasure_reason_len CHECK (reason IS NULL OR char_length(reason) <= 500)
);

-- One open request per user (pending or failed-awaiting-DPO).
CREATE UNIQUE INDEX uniq_erasure_open ON erasure_requests (user_id) WHERE state IN (1, 3);

CREATE TABLE erasure_steps (
    request_id BIGINT      NOT NULL REFERENCES erasure_requests (id),
    step       TEXT        NOT NULL,                    -- e.g. 'identity.anonymize'
    state      SMALLINT    NOT NULL DEFAULT 1,          -- 1=pending, 2=done, 3=failed
    attempts   INT         NOT NULL DEFAULT 0,
    last_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (request_id, step),
    CONSTRAINT erasure_step_state_valid CHECK (state IN (1, 2, 3))
);

CREATE INDEX idx_erasure_steps_pending ON erasure_steps (request_id) WHERE state = 1;

-- T-1.1.3.9 — what the DPO dashboard shows: every erasure not yet complete,
-- whether it is past its legal deadline, and which steps are stuck.
CREATE VIEW dpo_incomplete_erasures AS
SELECT r.public_id                                            AS request_id,
       r.user_id,
       r.state,
       r.requested_at,
       r.completion_by,
       now() > r.completion_by                                AS overdue,
       (array_agg(s.step ORDER BY s.step) FILTER (WHERE s.state <> 2))::text[] AS unfinished_steps,
       (array_agg(s.step ORDER BY s.step) FILTER (WHERE s.state = 3))::text[]  AS failed_steps,
       COALESCE(max(s.last_error), '')::text                  AS last_error
FROM erasure_requests r
JOIN erasure_steps s ON s.request_id = r.id
WHERE r.state <> 2
GROUP BY r.id;
