-- 0037_pool_controls: account-pool enhancements (phase 6 of the
-- upstream-OAuth design, docs/architecture/upstream-oauth-plugins.md §17).
--
--   accounts.max_concurrency   — per-account in-flight request cap.
--                                0 (default) = unlimited; enforcement is
--                                in-process (gateway handlers).
--   provider_groups.routing_policy — how the resolver picks inside the
--                                top priority bucket when every candidate
--                                belongs to this group. weighted_random
--                                is the historical behaviour.
--
-- Defaults make this migration inert for existing rows.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS max_concurrency INT NOT NULL DEFAULT 0
        CHECK (max_concurrency >= 0);

ALTER TABLE provider_groups
    ADD COLUMN IF NOT EXISTS routing_policy TEXT NOT NULL DEFAULT 'weighted_random'
        CHECK (routing_policy IN ('weighted_random', 'round_robin'));
