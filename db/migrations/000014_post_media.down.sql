DELETE FROM posts WHERE content IS NULL AND state NOT IN (3, 4);
ALTER TABLE posts
    DROP CONSTRAINT posts_content_present,
    ADD CONSTRAINT posts_content_present CHECK (state IN (3, 4) OR content IS NOT NULL),
    DROP COLUMN IF EXISTS media_id;
