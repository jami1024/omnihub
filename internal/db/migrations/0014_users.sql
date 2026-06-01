-- 0014_users: end-user accounts for the self-service portal.
--
-- Distinct from admin_users (the console operators). A user owns zero or
-- more virtual api_keys and signs into the /portal surface to see their
-- own usage, spend, and keys. Registration is open (self-signup), so the
-- table is the source of truth for portal login.
--
-- Passwords are bcrypt-hashed (admin.HashPassword); the cleartext is
-- never stored.

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL    PRIMARY KEY,
    username      VARCHAR(64)  NOT NULL UNIQUE,
    email         VARCHAR(255),
    password_hash CHAR(60)     NOT NULL,
    enabled       BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Link virtual keys to their owning user. NULL = an admin/system key
-- (created from the console or seeded from env), which no portal user
-- owns. ON DELETE SET NULL keeps a deleted user's keys alive but
-- unowned; the admin can clean them up.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS api_keys_user_id_idx ON api_keys (user_id);
