-- 0032_gateway_settings: runtime-tunable gateway health and failover knobs.
--
-- portal_settings is intentionally limited to end-user portal policy. This
-- separate single-row table stores operator-controlled gateway behaviour:
-- active health probing, default circuit-breaker settings, and per-request
-- failover breadth. NULL is not used; the migration seeds the same defaults
-- the code shipped with before this table existed.

CREATE TABLE IF NOT EXISTS gateway_settings (
    id                              INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    health_probe_enabled            BOOLEAN NOT NULL DEFAULT FALSE,
    health_probe_interval_ms        BIGINT  NOT NULL DEFAULT 60000,
    health_probe_concurrency        INTEGER NOT NULL DEFAULT 4,
    health_probe_red_threshold      INTEGER NOT NULL DEFAULT 3,
    health_probe_green_threshold    INTEGER NOT NULL DEFAULT 1,
    health_probe_timeout_ms         BIGINT  NOT NULL DEFAULT 15000,
    health_probe_slow_threshold_ms  BIGINT  NOT NULL DEFAULT 2500,
    circuit_failure_threshold       INTEGER NOT NULL DEFAULT 5,
    circuit_open_duration_ms        BIGINT  NOT NULL DEFAULT 30000,
    circuit_half_open_success       INTEGER NOT NULL DEFAULT 1,
    failover_max_attempts           INTEGER NOT NULL DEFAULT 3,
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO gateway_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
