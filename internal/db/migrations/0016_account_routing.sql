-- 0016_account_routing: per-account model redirects + spend caps.
--
-- model_redirects rewrites the requested model name before the driver
-- builds the upstream request, mirroring claude-code-hub's provider
-- model mapping. It is a JSONB array of rules, each
--     {"match_type": "exact|prefix|suffix|contains|regex",
--      "source": "...", "target": "..."}
-- evaluated in order; the first match wins. An empty array (the
-- default) means "forward the model unchanged". JSONB rather than a
-- side table keeps the whole rule set atomic with the account row and
-- lets the existing NOTIFY/refresh path reload it for free.
--
-- daily_usd_limit / total_usd_limit are optional spend ceilings the
-- resolver enforces per account: daily is a rolling 24h window, total
-- is lifetime. NULL (the default) means "no cap". NUMERIC(12,4)
-- matches the precision used for cost_usd elsewhere.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS model_redirects  JSONB         NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS daily_usd_limit  NUMERIC(12,4)
        CHECK (daily_usd_limit IS NULL OR daily_usd_limit >= 0),
    ADD COLUMN IF NOT EXISTS total_usd_limit  NUMERIC(12,4)
        CHECK (total_usd_limit IS NULL OR total_usd_limit >= 0);
