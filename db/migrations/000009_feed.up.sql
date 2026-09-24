-- Feature 2.1.4 — feed composition (LLD v2.0 §2.3, §7).
--
-- Sponsored posts carry an immutable bilingual label (F-07) and are kept out
-- of organic ranking: the feed indexes skip them. Feed indexes gain id as a
-- tiebreaker so pages walk (score, id) with a keyset cursor.

ALTER TABLE posts
    ADD COLUMN sponsored     BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN label_text_en TEXT,
    ADD COLUMN label_text_sw TEXT,
    ADD CONSTRAINT posts_sponsored_labelled CHECK (
        NOT sponsored OR (label_text_en IS NOT NULL AND label_text_sw IS NOT NULL));

-- F-07: the sponsored flag and its labels can't change after insert.
CREATE FUNCTION posts_sponsored_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.sponsored IS DISTINCT FROM OLD.sponsored
       OR NEW.label_text_en IS DISTINCT FROM OLD.label_text_en
       OR NEW.label_text_sw IS DISTINCT FROM OLD.label_text_sw THEN
        RAISE EXCEPTION 'sponsored label is immutable' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER posts_sponsored_immutable BEFORE UPDATE ON posts
    FOR EACH ROW EXECUTE FUNCTION posts_sponsored_immutable();

DROP INDEX idx_feed_ward, idx_feed_const, idx_feed_county, idx_feed_nat;
CREATE INDEX idx_feed_ward ON posts (ward_id, score DESC, id DESC)
    WHERE level = 1 AND state = 1 AND root_id IS NULL AND NOT sponsored;
CREATE INDEX idx_feed_const ON posts (constituency_id, score DESC, id DESC)
    WHERE level = 2 AND state = 1 AND root_id IS NULL AND NOT sponsored;
CREATE INDEX idx_feed_county ON posts (county_id, score DESC, id DESC)
    WHERE level = 3 AND state = 1 AND root_id IS NULL AND NOT sponsored;
CREATE INDEX idx_feed_nat ON posts (score DESC, id DESC)
    WHERE level = 4 AND state = 1 AND root_id IS NULL AND NOT sponsored;
