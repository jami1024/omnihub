-- 0038_proxies: proxy pool — upstream egress proxies as a first-class
-- resource (phase C of the upstream-OAuth roadmap, "代理池").
--
-- Until now an account carried a single inline proxy_url. This promotes
-- proxies to their own table so several accounts can share one proxy,
-- credentials are managed in one place, and an expiring proxy can fall
-- back to a backup (or direct) without editing every account.
--
-- accounts.proxy_id binds an account to a proxy (multi-to-one, nullable
-- = direct or legacy proxy_url). The legacy accounts.proxy_url column is
-- kept: an account with proxy_id = NULL still honours proxy_url, so this
-- migration is non-breaking and no data is moved.
--
-- The `password` value is encrypted at rest by the application cipher
-- (same enc:v1: scheme as account credentials), so the column is plain
-- TEXT here; the gateway decrypts on load.

CREATE TABLE IF NOT EXISTS proxies (
    id              BIGSERIAL   PRIMARY KEY,
    name            VARCHAR(100) NOT NULL UNIQUE,
    protocol        TEXT        NOT NULL DEFAULT 'http'
        CHECK (protocol IN ('http', 'https', 'socks5', 'socks5h')),
    host            TEXT        NOT NULL,
    port            INT         NOT NULL CHECK (port BETWEEN 1 AND 65535),
    username        TEXT        NOT NULL DEFAULT '',
    password        TEXT        NOT NULL DEFAULT '',  -- encrypted at rest
    status          TEXT        NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    expires_at      TIMESTAMPTZ,                       -- NULL = never expires
    -- Expiry fallback: when this proxy is past expires_at, route bound
    -- accounts via the backup proxy ('proxy'), direct ('direct'), or
    -- keep using it ('none' — the default, i.e. expiry is advisory).
    fallback_mode   TEXT        NOT NULL DEFAULT 'none'
        CHECK (fallback_mode IN ('none', 'direct', 'proxy')),
    backup_proxy_id BIGINT      REFERENCES proxies(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS proxy_id BIGINT REFERENCES proxies(id) ON DELETE SET NULL;

-- Instant in-memory ProxyPool refresh on proxies mutations, mirroring
-- the accounts NOTIFY trigger (migration 0006).
CREATE OR REPLACE FUNCTION omnihub_notify_proxies_changed()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('omnihub_proxies_changed', TG_OP);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS omnihub_proxies_notify ON proxies;
CREATE TRIGGER omnihub_proxies_notify
    AFTER INSERT OR UPDATE OR DELETE ON proxies
    FOR EACH STATEMENT
    EXECUTE FUNCTION omnihub_notify_proxies_changed();
