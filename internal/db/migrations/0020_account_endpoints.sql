-- 0020_account_endpoints: per-account multi-endpoint failover.
--
-- endpoints is a JSONB array of ADDITIONAL upstream base URLs for the
-- account, tried in order after base_url when a request fails with a
-- transport error or a retriable status (5xx / 429) — claude-code-hub's
-- multi-endpoint failover, reimplemented. All endpoints share the
-- account's single credential set; this is intra-account failover that
-- runs before the existing inter-account failover.
--
-- Default '[]' means "only base_url". JSONB keeps the list atomic with
-- the row and reloads over the NOTIFY path.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS endpoints JSONB NOT NULL DEFAULT '[]'::jsonb;
