-- 0008_api_keys: virtual API keys as a first-class DB entity.
--
-- Replaces the env-var single-string list with a table so each key
-- can carry its own metadata, limits, and lifecycle. Step 1 of the
-- "client limits" rollout — limit columns land in the table but are
-- not yet enforced; a follow-up commit adds the Limits Guard.
--
-- The key value itself is never stored. On insert the caller hashes
-- the key (sha256 hex), stores the hash, and surfaces the cleartext
-- to the operator exactly once. Authentication compares
-- sha256(submitted) against key_hash — high-entropy keys (32+ random
-- bytes) make bcrypt-style slow hashing unnecessary; the index
-- already gives constant-time DB-side lookup.

CREATE TABLE IF NOT EXISTS api_keys (
    id              BIGSERIAL    PRIMARY KEY,
    name            VARCHAR(64)  NOT NULL UNIQUE,
    key_hash        CHAR(64)     NOT NULL UNIQUE,  -- sha256 hex, 64 chars
    label           VARCHAR(64),                    -- displayed in logs
    enabled         BOOLEAN      NOT NULL DEFAULT TRUE,

    -- Limits (populated, not yet enforced — Step 2 lights up the guard).
    daily_usd_limit  NUMERIC(12,4),                 -- NULL = no limit
    rpm_limit        INTEGER  CHECK (rpm_limit IS NULL OR rpm_limit > 0),
    allowed_models   JSONB,                          -- NULL/empty = all models

    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Hot-path lookup by hash: every request does sha256(submitted_key)
-- and probes this index. The UNIQUE constraint already creates one,
-- but naming it explicitly makes it visible in pg_indexes.
-- (Postgres backs UNIQUE with a btree index; the implicit name is
--  api_keys_key_hash_key. Renaming is unnecessary.)

-- Instant refresh trigger — same pattern as accounts (migration 0006).
CREATE OR REPLACE FUNCTION omnihub_notify_api_keys_changed()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('omnihub_api_keys_changed', TG_OP);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS omnihub_api_keys_notify ON api_keys;
CREATE TRIGGER omnihub_api_keys_notify
    AFTER INSERT OR UPDATE OR DELETE ON api_keys
    FOR EACH STATEMENT
    EXECUTE FUNCTION omnihub_notify_api_keys_changed();
