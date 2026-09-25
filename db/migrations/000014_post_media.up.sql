-- Posts may carry one processed image or video (docs/media). Text is then
-- optional: a photo of a broken water point can speak for itself.
ALTER TABLE posts
    ADD COLUMN media_id BIGINT REFERENCES media (id),
    DROP CONSTRAINT posts_content_present,
    ADD CONSTRAINT posts_content_present CHECK (state IN (3, 4) OR content IS NOT NULL OR media_id IS NOT NULL);
