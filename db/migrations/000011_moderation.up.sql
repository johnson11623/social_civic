-- EPIC 3.1 — reports, moderation actions, appeals (LLD v2.0 §2.4).
--
-- posts is partitioned by created_at, so these tables reference a post by
-- its row id without a foreign key (see 000007); the public id is copied for
-- audit readability.
--
-- Harm-based policy (T-3.1.2.1): only these reasons justify removing
-- speech. Disagreement or criticism of officials is not a reason.

CREATE TABLE reports (
    id          BIGSERIAL PRIMARY KEY,
    public_id   UUID        NOT NULL UNIQUE,
    post_id     BIGINT      NOT NULL,
    reporter_id BIGINT      NOT NULL REFERENCES users (id),
    reason_code TEXT        NOT NULL,
    details     TEXT,
    post_level  SMALLINT    NOT NULL,          -- level when reported: picks the queue
    state       SMALLINT    NOT NULL DEFAULT 1, -- 1=open 2=actioned 3=dismissed
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT reports_reason_valid CHECK (reason_code IN ('hate_speech', 'incitement', 'privacy', 'child_safety')),
    CONSTRAINT reports_details_len CHECK (details IS NULL OR char_length(details) <= 500),
    CONSTRAINT reports_state_valid CHECK (state BETWEEN 1 AND 3),
    CONSTRAINT reports_one_per_user UNIQUE (post_id, reporter_id)  -- T-3.1.1.4
);
CREATE INDEX idx_reports_open_post ON reports (post_id, created_at) WHERE state = 1;

CREATE TABLE moderation_actions (
    id             BIGSERIAL PRIMARY KEY,
    public_id      UUID        NOT NULL UNIQUE,
    post_id        BIGINT      NOT NULL,
    post_public_id UUID        NOT NULL,
    actor_id       BIGINT      NOT NULL REFERENCES users (id),
    action         TEXT        NOT NULL,
    reason_code    TEXT        NOT NULL,
    notes          TEXT,
    scope_level    SMALLINT    NOT NULL,       -- post level when acted on (tiered authority)
    previous_state SMALLINT    NOT NULL,
    new_state      SMALLINT    NOT NULL,
    appeal_due_at  TIMESTAMPTZ,                -- 14-day window (T-3.1.2.6); NULL for restore
    overturned_at  TIMESTAMPTZ,                -- set when an appeal overturns it: flags the moderator
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT moderation_actions_action_valid CHECK (action IN ('hide', 'delete', 'freeze', 'restore')),
    CONSTRAINT moderation_actions_reason_valid CHECK (reason_code IN ('hate_speech', 'incitement', 'privacy', 'child_safety')),
    CONSTRAINT moderation_actions_notes_len CHECK (notes IS NULL OR char_length(notes) <= 1000),
    CONSTRAINT moderation_actions_states CHECK (previous_state BETWEEN 1 AND 4 AND new_state BETWEEN 1 AND 4)
);
CREATE INDEX idx_moderation_actions_post ON moderation_actions (post_id, created_at DESC);
CREATE INDEX idx_moderation_actions_actor ON moderation_actions (actor_id, created_at DESC);

-- Every moderation decision is immutable (audit): only overturned_at may be set, once.
CREATE FUNCTION moderation_actions_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'moderation actions are immutable' USING ERRCODE = 'check_violation';
    END IF;
    IF (to_jsonb(NEW) - 'overturned_at') IS DISTINCT FROM (to_jsonb(OLD) - 'overturned_at')
       OR (OLD.overturned_at IS NOT NULL AND NEW.overturned_at IS DISTINCT FROM OLD.overturned_at) THEN
        RAISE EXCEPTION 'moderation actions are immutable' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER moderation_actions_immutable BEFORE UPDATE ON moderation_actions
    FOR EACH ROW EXECUTE FUNCTION moderation_actions_immutable();
CREATE TRIGGER moderation_actions_no_delete BEFORE DELETE ON moderation_actions
    FOR EACH ROW EXECUTE FUNCTION moderation_actions_immutable();

CREATE TABLE appeals (
    id             BIGSERIAL PRIMARY KEY,
    public_id      UUID        NOT NULL UNIQUE,
    action_id      BIGINT      NOT NULL UNIQUE REFERENCES moderation_actions (id),  -- one appeal per action
    appellant_id   BIGINT      NOT NULL REFERENCES users (id),
    statement      TEXT        NOT NULL,
    state          SMALLINT    NOT NULL DEFAULT 1,  -- 1=open 2=upheld 3=overturned
    filed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    due_at         TIMESTAMPTZ NOT NULL,           -- decision due: filed_at + 14 days
    decided_at     TIMESTAMPTZ,
    decided_by     BIGINT      REFERENCES users (id),
    decision_notes TEXT,
    CONSTRAINT appeals_statement_len CHECK (char_length(statement) BETWEEN 1 AND 2000),
    CONSTRAINT appeals_notes_len CHECK (decision_notes IS NULL OR char_length(decision_notes) <= 1000),
    CONSTRAINT appeals_state_valid CHECK (state BETWEEN 1 AND 3),
    CONSTRAINT appeals_decided CHECK ((state = 1) = (decided_at IS NULL))
);
CREATE INDEX idx_appeals_open ON appeals (due_at) WHERE state = 1;
