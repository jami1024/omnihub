-- 0021_account_health_probe: per-account active health-probe opt-in.
--
-- health_probe_enabled controls whether the background prober actively
-- checks this account's upstream reachability (GET /v1/models) and feeds
-- the verdict into the existing circuit breaker, taking a sick upstream
-- out of rotation BEFORE user traffic hits it. NULL (the default) means
-- "inherit the global OMNIHUB_HEALTH_PROBE_ENABLED default"; TRUE / FALSE
-- are explicit per-account overrides. The nullable-no-default shape
-- mirrors circuit_failure_threshold so NULL stays distinguishable from an
-- explicit false.
--
-- No NOTIFY trigger change: accounts already notify on write (0006) and
-- the pool reloads, so the prober's next tick sees the new value.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS health_probe_enabled BOOLEAN;
