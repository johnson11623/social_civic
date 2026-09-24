DROP TABLE IF EXISTS post_counters;
DROP TABLE IF EXISTS post_actors;
DROP TABLE IF EXISTS post_likes;
DROP INDEX IF EXISTS idx_feed_ward, idx_feed_const, idx_feed_county, idx_feed_nat, idx_posts_thread;
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_thread_consistent, DROP COLUMN IF EXISTS parent_id, DROP COLUMN IF EXISTS root_id;
CREATE INDEX idx_feed_ward ON posts (ward_id, score DESC) WHERE level = 1 AND state = 1;
CREATE INDEX idx_feed_const ON posts (constituency_id, score DESC) WHERE level = 2 AND state = 1;
CREATE INDEX idx_feed_county ON posts (county_id, score DESC) WHERE level = 3 AND state = 1;
CREATE INDEX idx_feed_nat ON posts (score DESC) WHERE level = 4 AND state = 1;
