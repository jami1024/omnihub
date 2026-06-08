package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/health"
)

// GatewaySettingsRepo reads/writes the single-row gateway_settings table:
// runtime-tunable provider health, circuit-breaker, and failover policy.
type GatewaySettingsRepo struct {
	pool *pgxpool.Pool
}

func NewGatewaySettingsRepo(pool *pgxpool.Pool) *GatewaySettingsRepo {
	return &GatewaySettingsRepo{pool: pool}
}

// GatewaySettings is the JSON/admin representation of gateway runtime knobs.
// Durations are exposed as milliseconds so the web UI can use plain number
// inputs and the DB stores deterministic integer values.
type GatewaySettings struct {
	HealthProbeEnabled         bool  `json:"health_probe_enabled"`
	HealthProbeIntervalMs      int64 `json:"health_probe_interval_ms"`
	HealthProbeConcurrency     int   `json:"health_probe_concurrency"`
	HealthProbeRedThreshold    int   `json:"health_probe_red_threshold"`
	HealthProbeGreenThreshold  int   `json:"health_probe_green_threshold"`
	HealthProbeTimeoutMs       int64 `json:"health_probe_timeout_ms"`
	HealthProbeSlowThresholdMs int64 `json:"health_probe_slow_threshold_ms"`
	CircuitFailureThreshold    int   `json:"circuit_failure_threshold"`
	CircuitOpenDurationMs      int64 `json:"circuit_open_duration_ms"`
	CircuitHalfOpenSuccess     int   `json:"circuit_half_open_success"`
	FailoverMaxAttempts        int   `json:"failover_max_attempts"`
}

func DefaultGatewaySettings() GatewaySettings {
	cfg := health.DefaultConfig()
	probe := health.DefaultProbeConfig()
	return GatewaySettings{
		HealthProbeEnabled:         probe.GlobalDefault,
		HealthProbeIntervalMs:      probe.Interval.Milliseconds(),
		HealthProbeConcurrency:     probe.Concurrency,
		HealthProbeRedThreshold:    probe.RedThreshold,
		HealthProbeGreenThreshold:  probe.GreenThreshold,
		HealthProbeTimeoutMs:       probe.Timeout.Milliseconds(),
		HealthProbeSlowThresholdMs: probe.SlowThreshold.Milliseconds(),
		CircuitFailureThreshold:    cfg.FailureThreshold,
		CircuitOpenDurationMs:      cfg.OpenDuration.Milliseconds(),
		CircuitHalfOpenSuccess:     cfg.HalfOpenSuccessThreshold,
		FailoverMaxAttempts:        3,
	}
}

func (s GatewaySettings) CircuitConfig() health.Config {
	return health.Config{
		FailureThreshold:         s.CircuitFailureThreshold,
		OpenDuration:             time.Duration(s.CircuitOpenDurationMs) * time.Millisecond,
		HalfOpenSuccessThreshold: s.CircuitHalfOpenSuccess,
	}
}

func (s GatewaySettings) ProbeConfig() health.ProbeConfig {
	return health.NormalizeProbeConfig(health.ProbeConfig{
		GlobalDefault:  s.HealthProbeEnabled,
		Interval:       time.Duration(s.HealthProbeIntervalMs) * time.Millisecond,
		Concurrency:    s.HealthProbeConcurrency,
		RedThreshold:   s.HealthProbeRedThreshold,
		GreenThreshold: s.HealthProbeGreenThreshold,
		Timeout:        time.Duration(s.HealthProbeTimeoutMs) * time.Millisecond,
		SlowThreshold:  time.Duration(s.HealthProbeSlowThresholdMs) * time.Millisecond,
	})
}

func (r *GatewaySettingsRepo) Get(ctx context.Context) (GatewaySettings, error) {
	s := DefaultGatewaySettings()
	err := r.pool.QueryRow(ctx, `
        SELECT health_probe_enabled, health_probe_interval_ms, health_probe_concurrency,
               health_probe_red_threshold, health_probe_green_threshold,
               health_probe_timeout_ms, health_probe_slow_threshold_ms,
               circuit_failure_threshold, circuit_open_duration_ms, circuit_half_open_success,
               failover_max_attempts
          FROM gateway_settings WHERE id = 1`).
		Scan(&s.HealthProbeEnabled, &s.HealthProbeIntervalMs, &s.HealthProbeConcurrency,
			&s.HealthProbeRedThreshold, &s.HealthProbeGreenThreshold,
			&s.HealthProbeTimeoutMs, &s.HealthProbeSlowThresholdMs,
			&s.CircuitFailureThreshold, &s.CircuitOpenDurationMs, &s.CircuitHalfOpenSuccess,
			&s.FailoverMaxAttempts)
	if err != nil {
		return DefaultGatewaySettings(), fmt.Errorf("read gateway_settings: %w", err)
	}
	return s, nil
}

func (r *GatewaySettingsRepo) Update(ctx context.Context, s GatewaySettings) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO gateway_settings (
            id, health_probe_enabled, health_probe_interval_ms, health_probe_concurrency,
            health_probe_red_threshold, health_probe_green_threshold,
            health_probe_timeout_ms, health_probe_slow_threshold_ms,
            circuit_failure_threshold, circuit_open_duration_ms, circuit_half_open_success,
            failover_max_attempts, updated_at
        )
        VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
        ON CONFLICT (id) DO UPDATE SET
            health_probe_enabled = EXCLUDED.health_probe_enabled,
            health_probe_interval_ms = EXCLUDED.health_probe_interval_ms,
            health_probe_concurrency = EXCLUDED.health_probe_concurrency,
            health_probe_red_threshold = EXCLUDED.health_probe_red_threshold,
            health_probe_green_threshold = EXCLUDED.health_probe_green_threshold,
            health_probe_timeout_ms = EXCLUDED.health_probe_timeout_ms,
            health_probe_slow_threshold_ms = EXCLUDED.health_probe_slow_threshold_ms,
            circuit_failure_threshold = EXCLUDED.circuit_failure_threshold,
            circuit_open_duration_ms = EXCLUDED.circuit_open_duration_ms,
            circuit_half_open_success = EXCLUDED.circuit_half_open_success,
            failover_max_attempts = EXCLUDED.failover_max_attempts,
            updated_at = NOW()`,
		s.HealthProbeEnabled, s.HealthProbeIntervalMs, s.HealthProbeConcurrency,
		s.HealthProbeRedThreshold, s.HealthProbeGreenThreshold,
		s.HealthProbeTimeoutMs, s.HealthProbeSlowThresholdMs,
		s.CircuitFailureThreshold, s.CircuitOpenDurationMs, s.CircuitHalfOpenSuccess,
		s.FailoverMaxAttempts)
	if err != nil {
		return fmt.Errorf("update gateway_settings: %w", err)
	}
	return nil
}
