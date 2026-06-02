-- 0017_message_account_idx: index for per-account spend-cap queries.
--
-- The per-account spend guard (0016's daily_usd_limit / total_usd_limit)
-- sums cost_usd grouped by account_name — both a rolling-24h window and
-- a lifetime total. This composite index serves the 24h query (range on
-- created_at within an account) and the lifetime SUM (account_name
-- prefix scan) without touching the request hot path, which never reads
-- by account_name.

CREATE INDEX IF NOT EXISTS message_requests_account_name_idx
    ON message_requests (account_name, created_at DESC);
