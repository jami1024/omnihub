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

// TestOpusCurrentTierPricing covers every Opus version on the
// reduced $5/$25/MTok tier (4.5 / 4.6 / 4.7 share the same numbers).
// 4.1 stays on the legacy tier and is exercised separately.
func TestOpusCurrentTierPricing(t *testing.T) {
	tbl := pricing.Default()
	for _, model := range []string{"claude-opus-4-5", "claude-opus-4-6", "claude-opus-4-7"} {
		t.Run(model, func(t *testing.T) {
			got, ok := tbl.Calculate(model, usage.Usage{
				InputTokens:  1_000_000,
				OutputTokens: 100_000,
			})
			if !ok {
				t.Fatalf("expected price entry for %s", model)
			}
			// $5 + 0.1 × $25 = $7.50
			if !approxEqual(got.Total, 7.50) {
				t.Errorf("%s should price at current tier ($5/$25 per MTok): want 7.50, got %g", model, got.Total)
			}
		})
	}
}

// TestOpus41LegacyTier locks in the legacy $15/$75 pricing for the
// 4.1 series so a future "move everything to current tier" refactor
// trips a test before going live.
func TestOpus41LegacyTier(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("claude-opus-4-1", usage.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 100_000,
	})
	if !ok {
		t.Fatalf("expected price entry for claude-opus-4-1")
	}
	// $15 + 0.1 × $75 = $22.50
	if !approxEqual(got.Total, 22.50) {
		t.Errorf("Opus 4.1 should stay on legacy $15/$75 tier: want 22.50, got %g", got.Total)
	}
}

// TestOpus46CacheWriteMatchesInvoice regression-tests the bug
// reported 2026-05-17: a real Claude Platform on AWS invoice showed
// 29 701 cache_creation tokens billed at $0.185631 on Opus 4.6
// ($6.25/MTok), but OmniHub had 4.6 misfiled under the legacy
// $18.75/MTok tier and recorded 3× the cost.
func TestOpus46CacheWriteMatchesInvoice(t *testing.T) {
	tbl := pricing.Default()
	got, _ := tbl.Calculate("claude-opus-4-6", usage.Usage{
		InputTokens:              3,
		OutputTokens:             19,
		CacheCreationInputTokens: 29_701,
	})
	// $0.000015 + $0.000475 + $0.18563125 = $0.18612125
	// (Invoice rounds the cache row to $0.185631; OmniHub keeps the
	// quarter-micro-cent for accurate daily-cap math.)
	if !approxEqual(got.Total, 0.18612125) {
		t.Errorf("Opus 4.6 cost should match invoice (~$0.186121): got %g", got.Total)
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

// TestGPT4oPricing locks in the $2.50 / $10 / MTok rate for GPT-4o.
// Stable since the 2024-10 prompt-caching launch; a future "everything
// is GPT-5 now" refactor that drops or moves this entry trips here.
func TestGPT4oPricing(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("gpt-4o", usage.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 500_000,
	})
	if !ok {
		t.Fatalf("expected price for gpt-4o")
	}
	// 1M × $2.50 + 0.5M × $10 = $7.50
	if !approxEqual(got.Total, 7.50) {
		t.Errorf("gpt-4o total: want 7.50, got %g", got.Total)
	}
}

func TestGPT4oMiniPricing(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("gpt-4o-mini", usage.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 500_000,
	})
	if !ok {
		t.Fatalf("expected price for gpt-4o-mini")
	}
	// 1M × $0.15 + 0.5M × $0.60 = $0.45
	if !approxEqual(got.Total, 0.45) {
		t.Errorf("gpt-4o-mini total: want 0.45, got %g", got.Total)
	}
}

// TestGPT4oMiniPrefixBeatsGPT4o makes sure a versioned mini model id
// hits the mini rate, not the 17× more expensive base rate. Anthropic
// lookups already exercise longest-prefix, but the OpenAI naming
// (sibling models sharing a prefix) makes this regression matter.
func TestGPT4oMiniPrefixBeatsGPT4o(t *testing.T) {
	tbl := pricing.Default()
	got, ok := tbl.Calculate("gpt-4o-mini-2024-07-18", usage.Usage{
		InputTokens: 1_000_000,
	})
	if !ok {
		t.Fatalf("expected versioned mini id to match")
	}
	if !approxEqual(got.Total, 0.15) {
		t.Errorf("versioned mini should hit $0.15 (mini tier), got %g", got.Total)
	}
}

// TestGPT4oCachedReadUsesOpenAIRatio verifies the cached_input price is
// 50% off (OpenAI's ratio), not 10% off (Anthropic's). The fallback in
// Calculate would silently misbill at the Anthropic ratio if the entry
// forgot to set CacheReadInputTokenCost explicitly.
func TestGPT4oCachedReadUsesOpenAIRatio(t *testing.T) {
	tbl := pricing.Default()
	got, _ := tbl.Calculate("gpt-4o", usage.Usage{
		CacheReadInputTokens: 1_000_000,
	})
	if !approxEqual(got.Total, 1.25) {
		t.Errorf("gpt-4o cached read should be $1.25/MTok (50%% off), got %g", got.Total)
	}
}

// TestDeepSeekV4FlashAliases verifies both deprecated legacy names and
// the new "v4-flash" id resolve to the same V4 Flash tier. The
// deprecation lands 2026-07-24; until then all three still bill.
func TestDeepSeekV4FlashAliases(t *testing.T) {
	tbl := pricing.Default()
	for _, model := range []string{"deepseek-chat", "deepseek-reasoner", "deepseek-v4-flash"} {
		t.Run(model, func(t *testing.T) {
			got, ok := tbl.Calculate(model, usage.Usage{
				InputTokens:  1_000_000,
				OutputTokens: 1_000_000,
			})
			if !ok {
				t.Fatalf("expected price entry for %s", model)
			}
			// 1M × $0.14 + 1M × $0.28 = $0.42
			if !approxEqual(got.Total, 0.42) {
				t.Errorf("%s total: want 0.42, got %g", model, got.Total)
			}
		})
	}
}

// TestDeepSeekCacheHitMatches98Off locks in the 98% cache-hit discount
// (the market-leading cache rate, 50× cheaper than the same call without
// cache hit).
func TestDeepSeekCacheHitMatches98Off(t *testing.T) {
	tbl := pricing.Default()
	got, _ := tbl.Calculate("deepseek-chat", usage.Usage{
		CacheReadInputTokens: 1_000_000,
	})
	if !approxEqual(got.Total, 0.0028) {
		t.Errorf("deepseek cache hit: want 0.0028 (98%% off), got %g", got.Total)
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
