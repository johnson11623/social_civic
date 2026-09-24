DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS otp_challenges;
DROP INDEX IF EXISTS idx_users_msisdn_hash;
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_msisdn_complete,
    DROP COLUMN IF EXISTS msisdn_hash,
    DROP COLUMN IF EXISTS msisdn_key_version,
    DROP COLUMN IF EXISTS msisdn_ciphertext;
