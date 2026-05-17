-- 0009_account_health_events: per-account circuit breaker state changes.
--
-- One row per transition between closed / open / half-open. The
-- gateway emits rows asynchronously from the in-memory Tracker, so
-- this table is the durable log operators query to understand
-- account flapping ("how many times did this account trip last
-- week?") without needing Prometheus or a metrics backend.
--
-- account_id is intentionally NOT a foreign key: events outlive
-- account deletions so post-mortem queries still work. account_name
-- is denormalised for the same reason.

CREATE TABLE IF NOT EXISTS account_health_events (
    id              BIGSERIAL    PRIMARY KEY,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    account_id      BIGINT       NOT NULL,
    account_name    VARCHAR(64)  NOT NULL,
    from_state      VARCHAR(16)  NOT NULL,        -- closed | open | half-open
    to_state        VARCHAR(16)  NOT NULL,
    failure_count   INTEGER      NOT NULL DEFAULT 0,
    reason          TEXT                          -- nullable; populated for failure-driven transitions
);

CREATE INDEX IF NOT EXISTS account_health_events_account_id_idx
    ON account_health_events (account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS account_health_events_created_at_idx
    ON account_health_events (created_at DESC);
