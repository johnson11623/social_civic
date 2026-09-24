DROP INDEX IF EXISTS idx_feed_ward, idx_feed_const, idx_feed_county, idx_feed_nat;
CREATE INDEX idx_feed_ward ON posts (ward_id, score DESC) WHERE level = 1 AND state = 1 AND root_id IS NULL;
CREATE INDEX idx_feed_const ON posts (constituency_id, score DESC) WHERE level = 2 AND state = 1 AND root_id IS NULL;
CREATE INDEX idx_feed_county ON posts (county_id, score DESC) WHERE level = 3 AND state = 1 AND root_id IS NULL;
CREATE INDEX idx_feed_nat ON posts (score DESC) WHERE level = 4 AND state = 1 AND root_id IS NULL;
DROP TRIGGER IF EXISTS posts_sponsored_immutable ON posts;
DROP FUNCTION IF EXISTS posts_sponsored_immutable();
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_sponsored_labelled,
    DROP COLUMN IF EXISTS label_text_sw, DROP COLUMN IF EXISTS label_text_en, DROP COLUMN IF EXISTS sponsored;
