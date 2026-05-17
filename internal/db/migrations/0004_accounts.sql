-- 0004_accounts: upstream account pool, source of truth.
--
-- Replaces the env-var single-account configuration with a database
-- table so accounts can be added, disabled, and adjusted live without
-- restarting the gateway.
--
-- credentials is JSONB rather than separate columns so each driver
-- can carry its own credential keys (api_key, aws_region,
-- workspace_id, aws_access_key_id, …) without schema migrations.
--
-- Bootstrap: cmd/omnihub auto-seeds a row from OMNIHUB_ANTHROPIC_API_KEY
-- or OMNIHUB_CLAUDE_PLATFORM_* env vars when this table is empty, so
-- existing MVP deployments upgrade transparently.

CREATE TABLE IF NOT EXISTS accounts (
    id              BIGSERIAL    PRIMARY KEY,
    name            VARCHAR(64)  NOT NULL UNIQUE,
    provider        VARCHAR(32)  NOT NULL,
    enabled         BOOLEAN      NOT NULL DEFAULT TRUE,
    weight          INTEGER      NOT NULL DEFAULT 100  CHECK (weight >= 0),
    priority        INTEGER      NOT NULL DEFAULT 0,
    cost_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1.0  CHECK (cost_multiplier >= 0),
    base_url        VARCHAR(255),
    credentials     JSONB        NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Resolver hot-path lookup: enabled accounts grouped by provider.
CREATE INDEX IF NOT EXISTS accounts_provider_enabled_idx
    ON accounts (provider)
    WHERE enabled = TRUE;
