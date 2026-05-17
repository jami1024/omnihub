-- 0005_account_circuit_overrides: per-account circuit breaker tuning.
--
-- Three nullable columns let operators override the global env-var
-- thresholds for one specific account without disturbing the rest.
-- NULL means "use the global OMNIHUB_CIRCUIT_* default" — this is the
-- common case; only accounts with specific SLA needs set the columns.
--
-- Example: a low-priority sandbox account with frequent transient
-- 5xx blips might tolerate 20 failures before tripping, while a
-- production paid account stays at the 5-failure default.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS circuit_failure_threshold   INTEGER
        CHECK (circuit_failure_threshold IS NULL OR circuit_failure_threshold >= 0),
    ADD COLUMN IF NOT EXISTS circuit_open_duration_ms    BIGINT
        CHECK (circuit_open_duration_ms IS NULL OR circuit_open_duration_ms > 0),
    ADD COLUMN IF NOT EXISTS circuit_half_open_success   INTEGER
        CHECK (circuit_half_open_success IS NULL OR circuit_half_open_success > 0);
