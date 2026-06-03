-- 0023_account_param_overrides: per-account generation-parameter overrides.
--
-- param_overrides forces IR-level generation parameters (max_tokens,
-- temperature, top_p, thinking budget) onto every request routed through
-- the account, before the driver renders them — claude-code-hub's
-- protocol parameter overrides, reimplemented as a matched-pair override
-- (the SAME driver renders the value; no cross-protocol transform).
--
-- JSONB object; each key is optional and only present when configured:
--   {"max_tokens": 4096, "temperature": 0.0, "top_p": 0.9,
--    "thinking_budget_tokens": 8000}
-- Default '{}' means "no overrides".

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS param_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;
