package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/health"
)

const gatewaySettingsRefreshInterval = 5 * time.Second

// liveGatewaySettings is the process-local snapshot of gateway_settings.
// The admin UI persists settings to Postgres; this object refreshes them into
// the hot path without requiring a process restart.
type liveGatewaySettings struct {
	repo *repository.GatewaySettingsRepo

	mu sync.RWMutex
	s  repository.GatewaySettings
}

func newLiveGatewaySettings(repo *repository.GatewaySettingsRepo, fallback repository.GatewaySettings) *liveGatewaySettings {
	return &liveGatewaySettings{repo: repo, s: fallback}
}

func setupGatewaySettings(ctx context.Context) {
	fallback := loadGatewaySettingsFallback()
	if pool == nil {
		gatewaySettings = newLiveGatewaySettings(nil, fallback)
		slog.Info("gateway settings using env/default fallback")
		return
	}
	repo := repository.NewGatewaySettingsRepo(pool)
	gatewaySettings = newLiveGatewaySettings(repo, fallback)
	gatewaySettings.Start(ctx)
}

func loadGatewaySettingsFallback() repository.GatewaySettings {
	circuit := loadHealthConfig()
	probe := loadHealthProbeConfig()
	settings := repository.DefaultGatewaySettings()
	settings.HealthProbeEnabled = probe.GlobalDefault
	settings.HealthProbeIntervalMs = probe.Interval.Milliseconds()
	settings.HealthProbeConcurrency = probe.Concurrency
	settings.HealthProbeRedThreshold = probe.RedThreshold
	settings.HealthProbeGreenThreshold = probe.GreenThreshold
	settings.HealthProbeTimeoutMs = probe.Timeout.Milliseconds()
	settings.HealthProbeSlowThresholdMs = probe.SlowThreshold.Milliseconds()
	settings.CircuitFailureThreshold = circuit.FailureThreshold
	settings.CircuitOpenDurationMs = circuit.OpenDuration.Milliseconds()
	settings.CircuitHalfOpenSuccess = circuit.HalfOpenSuccessThreshold
	settings.FailoverMaxAttempts = loadFailoverMaxAttempts()
	return settings
}

func currentCircuitConfig() health.Config {
	if gatewaySettings != nil {
		return gatewaySettings.CircuitConfig()
	}
	return loadHealthConfig()
}

func currentProbeConfig() health.ProbeConfig {
	if gatewaySettings != nil {
		return gatewaySettings.ProbeConfig()
	}
	return loadHealthProbeConfig()
}

func (g *liveGatewaySettings) Start(ctx context.Context) {
	if g == nil || g.repo == nil {
		return
	}
	if err := g.Refresh(ctx); err != nil {
		slog.Warn("gateway settings initial refresh failed; using defaults", "err", err.Error())
	}
	go func() {
		t := time.NewTicker(gatewaySettingsRefreshInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := g.Refresh(ctx); err != nil {
					slog.Warn("gateway settings refresh failed", "err", err.Error())
				}
			}
		}
	}()
}

func (g *liveGatewaySettings) Refresh(ctx context.Context) error {
	s, err := g.repo.Get(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.s = s
	g.mu.Unlock()
	return nil
}

func (g *liveGatewaySettings) Snapshot() repository.GatewaySettings {
	if g == nil {
		return repository.DefaultGatewaySettings()
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.s
}

func (g *liveGatewaySettings) CircuitConfig() health.Config {
	return g.Snapshot().CircuitConfig()
}

func (g *liveGatewaySettings) ProbeConfig() health.ProbeConfig {
	return g.Snapshot().ProbeConfig()
}

func (g *liveGatewaySettings) FailoverMaxAttempts() int {
	return g.Snapshot().FailoverMaxAttempts
}
