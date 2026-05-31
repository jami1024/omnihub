-- 0012_admin_users: login accounts for the web admin UI.
--
-- Deliberately distinct from api_keys (which authenticate gateway
-- traffic via x-api-key / Bearer cleartext keys). Admin users have a
-- username + bcrypt-encoded password and authenticate the React SPA via
-- short-lived JWTs minted by POST /admin/api/login.
--
-- The cleartext password is never stored; bcrypt always emits a
-- 60-character hash including its own algorithm/cost/salt prefix.
--
-- No LISTEN/NOTIFY trigger here: login looks the user up by username
-- on each call, there's no in-memory pool to invalidate. Volume is
-- "tens of logins per day per admin" — a live DB lookup is cheaper
-- than the cache-coherency machinery.
--
-- Bootstrap is via the CLI: `omnihub admin add --username=root` (which
-- prompts for the password) — same shape as `omnihub key add`.

CREATE TABLE IF NOT EXISTS admin_users (
    id              BIGSERIAL    PRIMARY KEY,
    username        VARCHAR(64)  NOT NULL UNIQUE,
    password_hash   CHAR(60)     NOT NULL,                    -- bcrypt is always 60 bytes
    enabled         BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE  admin_users IS
    'Web UI login accounts. Distinct from api_keys (gateway traffic auth).';
COMMENT ON COLUMN admin_users.password_hash IS
    'bcrypt hash (60 chars, includes algorithm/cost/salt). Cleartext is never stored.';
