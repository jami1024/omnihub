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
	// 1M input × $1.00/M  +  500k output × $5.00/M  =  $1.00 + $2.50
	want := 3.50
	if !approxEqual(got, want) {
		t.Errorf("cost: want %g, got %g", want, got)
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
	// 100 × $1/M + 50 × $5/M = 0.0001 + 0.00025 = 0.00035
	want := 0.00035
	if !approxEqual(got, want) {
		t.Errorf("cost: want %g, got %g", want, got)
	}
}

func TestCacheTokensCharged(t *testing.T) {
	tbl := pricing.Default()
	got, _ := tbl.Calculate("claude-sonnet-4-5", usage.Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheCreationInputTokens: 1_000_000, // billed at 5m rate ($3.75/M)
		CacheReadInputTokens:     1_000_000, // billed at $0.30/M
	})
	want := 3.75 + 0.30
	if !approxEqual(got, want) {
		t.Errorf("cost: want %g, got %g", want, got)
	}
}

func TestUnknownModelReturnsFalse(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("gpt-4-turbo", usage.Usage{InputTokens: 100})
	if ok {
		t.Errorf("unknown model should return ok=false, got cost=%g", got)
	}
	if got != 0 {
		t.Errorf("unknown model should return 0 cost, got %g", got)
	}
}

func TestZeroUsage(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("claude-opus-4-7", usage.Usage{})
	if !ok {
		t.Errorf("known model with zero usage should still match")
	}
	if got != 0 {
		t.Errorf("zero usage should cost 0, got %g", got)
	}
}

func TestLongestPrefixWins(t *testing.T) {
	// Build a table where two prefixes both match. Verify the longer
	// one is picked.
	tbl := pricing.Table{
		"claude-haiku":     {InputPerMillion: 100.0}, // bogus, must NOT be used
		"claude-haiku-4-5": {InputPerMillion: 1.0},   // correct match
	}
	got, _ := tbl.Calculate("claude-haiku-4-5-20251001", usage.Usage{InputTokens: 1_000_000})
	if !approxEqual(got, 1.0) {
		t.Errorf("longest-prefix match should pick 1.0 not 100.0, got %g", got)
	}
}
