package main

import (
	"testing"
	"time"
)

func TestLoadHealthProbeConfigReadsExtendedEnv(t *testing.T) {
	t.Setenv("OMNIHUB_HEALTH_PROBE_ENABLED", "true")
	t.Setenv("OMNIHUB_HEALTH_PROBE_INTERVAL", "45s")
	t.Setenv("OMNIHUB_HEALTH_PROBE_CONCURRENCY", "6")
	t.Setenv("OMNIHUB_HEALTH_PROBE_RED_THRESHOLD", "4")
	t.Setenv("OMNIHUB_HEALTH_PROBE_GREEN_THRESHOLD", "2")
	t.Setenv("OMNIHUB_HEALTH_PROBE_TIMEOUT", "9s")
	t.Setenv("OMNIHUB_HEALTH_PROBE_SLOW_THRESHOLD", "1800ms")

	cfg := loadHealthProbeConfig()

	if !cfg.GlobalDefault {
		t.Fatal("GlobalDefault = false, want true")
	}
	if cfg.Interval != 45*time.Second {
		t.Fatalf("Interval = %s, want 45s", cfg.Interval)
	}
	if cfg.Concurrency != 6 {
		t.Fatalf("Concurrency = %d, want 6", cfg.Concurrency)
	}
	if cfg.RedThreshold != 4 {
		t.Fatalf("RedThreshold = %d, want 4", cfg.RedThreshold)
	}
	if cfg.GreenThreshold != 2 {
		t.Fatalf("GreenThreshold = %d, want 2", cfg.GreenThreshold)
	}
	if cfg.Timeout != 9*time.Second {
		t.Fatalf("Timeout = %s, want 9s", cfg.Timeout)
	}
	if cfg.SlowThreshold != 1800*time.Millisecond {
		t.Fatalf("SlowThreshold = %s, want 1800ms", cfg.SlowThreshold)
	}
}
