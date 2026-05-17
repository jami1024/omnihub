package pricing_test

import (
	"math"
	"testing"

	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/usage"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestDefaultHaikuCalculation(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("claude-haiku-4-5", usage.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 500_000,
	})
	if !ok {
		t.Fatalf("expected price for claude-haiku-4-5")
	}
	// 1M input × $1.00/MTok + 500k output × $5.00/MTok = $1.00 + $2.50
	if !approxEqual(got.Total, 3.50) {
		t.Errorf("total: want 3.50, got %g", got.Total)
	}
	if !approxEqual(got.Input, 1.00) {
		t.Errorf("input bucket: want 1.00, got %g", got.Input)
	}
	if !approxEqual(got.Output, 2.50) {
		t.Errorf("output bucket: want 2.50, got %g", got.Output)
	}
}

func TestPrefixMatchPicksFullVersionedID(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("claude-haiku-4-5-20251001", usage.Usage{
		InputTokens:  100,
		OutputTokens: 50,
	})
	if !ok {
		t.Fatalf("expected prefix match for versioned id")
	}
	// 100 × $1.00/M + 50 × $5.00/M = 0.0001 + 0.00025 = 0.00035
	if !approxEqual(got.Total, 0.00035) {
		t.Errorf("total: want 0.00035, got %g", got.Total)
	}
}

func TestCacheTokensCharged(t *testing.T) {
	tbl := pricing.Default()
	got, _ := tbl.Calculate("claude-sonnet-4-5", usage.Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheCreationInputTokens: 1_000_000, // billed at 5m rate ($3.75/MTok)
		CacheReadInputTokens:     1_000_000, // billed at $0.30/MTok
	})
	want := 3.75 + 0.30
	if !approxEqual(got.Total, want) {
		t.Errorf("total: want %g, got %g", want, got.Total)
	}
	if !approxEqual(got.CacheCreation5m, 3.75) {
		t.Errorf("cache_creation_5m bucket: want 3.75, got %g", got.CacheCreation5m)
	}
	if !approxEqual(got.CacheRead, 0.30) {
		t.Errorf("cache_read bucket: want 0.30, got %g", got.CacheRead)
	}
}

func TestUnknownModelReturnsFalse(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("gpt-4-turbo", usage.Usage{InputTokens: 100})
	if ok {
		t.Errorf("unknown model should return ok=false, got cost=%g", got.Total)
	}
	if got.Total != 0 {
		t.Errorf("unknown model should return 0 cost, got %g", got.Total)
	}
}

func TestOpus47NewPricing(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("claude-opus-4-7", usage.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 100_000,
	})
	if !ok {
		t.Fatalf("expected price for claude-opus-4-7")
	}
	// $5 + 0.1 × $25 = $7.50 (post 2026-04-16 reduction)
	if !approxEqual(got.Total, 7.50) {
		t.Errorf("Opus 4.7 cost should reflect new $5/$25 pricing: want 7.50, got %g", got.Total)
	}
}

func TestLongestPrefixWins(t *testing.T) {
	tbl := pricing.Table{
		"claude-haiku": {InputCostPerToken: 100e-6}, // bogus, must NOT be used
		"claude-haiku-4-5": {
			InputCostPerToken:  1e-6, // correct match
			OutputCostPerToken: 5e-6,
		},
	}
	got, _ := tbl.Calculate("claude-haiku-4-5-20251001", usage.Usage{InputTokens: 1_000_000})
	if !approxEqual(got.Total, 1.0) {
		t.Errorf("longest-prefix should pick 1.0 not 100.0, got %g", got.Total)
	}
}

func TestCacheRateFallbackFromInput(t *testing.T) {
	// Price with ONLY input/output set (no explicit cache fields).
	// Expect canonical fallback ratios off input price.
	tbl := pricing.Table{
		"thin-model": {InputCostPerToken: 10e-6, OutputCostPerToken: 50e-6},
	}
	got, _ := tbl.Calculate("thin-model", usage.Usage{
		CacheCreationInputTokens: 1_000_000, // expect $10 × 1.25 = $12.50
		CacheReadInputTokens:     1_000_000, // expect $10 × 0.10 = $1.00
	})
	if !approxEqual(got.CacheCreation5m, 12.50) {
		t.Errorf("cache_creation fallback: want 12.50, got %g", got.CacheCreation5m)
	}
	if !approxEqual(got.CacheRead, 1.00) {
		t.Errorf("cache_read fallback: want 1.00, got %g", got.CacheRead)
	}
}

func TestApplyMultiplier(t *testing.T) {
	base := pricing.Breakdown{
		Input:           1.00,
		Output:          2.00,
		CacheCreation5m: 0.50,
		CacheRead:       0.10,
		Total:           3.60,
	}

	scaled := base.ApplyMultiplier(0.5)
	if !approxEqual(scaled.Total, 1.80) {
		t.Errorf("0.5× total: want 1.80, got %g", scaled.Total)
	}
	if !approxEqual(scaled.Input, 0.50) || !approxEqual(scaled.Output, 1.00) {
		t.Errorf("0.5× buckets: input=%g, output=%g (want 0.50, 1.00)",
			scaled.Input, scaled.Output)
	}
	if scaled.Multiplier != 0.5 {
		t.Errorf("multiplier recorded: want 0.5, got %g", scaled.Multiplier)
	}

	// Multiplier 1.0 or <= 0 returns unchanged copy.
	if got := base.ApplyMultiplier(1.0); got.Multiplier != 0 {
		t.Errorf("1.0 multiplier should not be recorded, got %g", got.Multiplier)
	}
	if got := base.ApplyMultiplier(0); !approxEqual(got.Total, base.Total) {
		t.Errorf("0 multiplier should return unchanged copy")
	}
}
