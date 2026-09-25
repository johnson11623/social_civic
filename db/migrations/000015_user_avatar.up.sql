-- Profile photos: an image uploaded through /v1/media (processed like any
-- photo: re-encoded without EXIF/GPS, resized, served from the CDN).
ALTER TABLE users ADD COLUMN avatar_media_id BIGINT REFERENCES media(id) ON DELETE SET NULL;
