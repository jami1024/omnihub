// Package pricing turns token-usage counts into a USD cost.
//
// The price-data shape mirrors LiteLLM's
// model_prices_and_context_window.json (per-token, snake_case field
// names with `json:` tags) so a future commit can sync prices from
// upstream without rewriting the struct. claude-code-hub uses the
// same shape via a TOML mirror at claude-code-hub.app/config/prices-base.toml.
//
// The default table ships hard-coded Anthropic prices (direct API and
// Claude Platform on AWS bill at the same rates). When a model is not
// in the table, Calculate returns (Breakdown{}, false) and the caller
// is expected to log a warning and persist NULL.
//
// Cache-rate fallbacks: if a price entry omits an explicit
// cache_creation / cache_read cost, the canonical Anthropic ratios
// off the input price are used (5m = 1.25×, 1h = 2.0×, read = 0.10×).
package pricing

import (
	"strings"

	"github.com/jami1024/omnihub/internal/service/usage"
)

// Price captures the per-token rates for one model, in USD per token
// (not per million). Field names mirror LiteLLM's JSON convention so
// the struct can directly Unmarshal an upstream price entry.
type Price struct {
	InputCostPerToken                   float64 `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token,omitempty"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost,omitempty"`
	CacheCreationInputTokenCostAbove1Hr float64 `json:"cache_creation_input_token_cost_above_1hr,omitempty"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost,omitempty"`
}

// Breakdown carries the per-bucket cost for one upstream call plus
// the rolled-up total. The struct is the canonical wire shape for the
// message_requests.cost_breakdown JSONB column.
type Breakdown struct {
	Input           float64 `json:"input"`
	Output          float64 `json:"output"`
	CacheCreation5m float64 `json:"cache_creation_5m,omitempty"`
	CacheCreation1h float64 `json:"cache_creation_1h,omitempty"`
	CacheRead       float64 `json:"cache_read,omitempty"`
	Total           float64 `json:"total"`
	// Multiplier is the account-level cost multiplier that was applied
	// to every bucket. Omitted from JSON when 1.0 (i.e. no multiplier).
	Multiplier float64 `json:"multiplier,omitempty"`
}

// ApplyMultiplier returns a copy of b with every bucket multiplied by
// m. The Multiplier field is set so analytics can recover the base
// cost as `bucket / multiplier`. m <= 0 or m == 1.0 returns b
// unchanged.
func (b Breakdown) ApplyMultiplier(m float64) Breakdown {
	if m <= 0 || m == 1.0 {
		return b
	}
	return Breakdown{
		Input:           b.Input * m,
		Output:          b.Output * m,
		CacheCreation5m: b.CacheCreation5m * m,
		CacheCreation1h: b.CacheCreation1h * m,
		CacheRead:       b.CacheRead * m,
		Total:           b.Total * m,
		Multiplier:      m,
	}
}

// Table maps a model-name prefix to its Price. Lookup uses the
// longest matching prefix, so "claude-haiku-4-5-20251001" resolves
// against the "claude-haiku-4-5" row.
type Table map[string]Price

// Default returns the built-in Anthropic price list, sourced from
// https://platform.claude.com/docs/en/about-claude/pricing as of
// 2026-05. Direct Anthropic and Claude Platform on AWS share rates.
//
// Cache rates follow the canonical Anthropic ratios off input
// (5m = 1.25×, 1h = 2.0×, read = 0.10×); they are listed explicitly
// here so an upstream sync can override them per model.
//
// All values are USD per token (multiply the $/MTok rate by 1e-6).
func Default() Table {
	return Table{
		// Haiku 4.5 — $1.00 / $5.00 / MTok
		"claude-haiku-4-5": {
			InputCostPerToken:                   1.00e-6,
			OutputCostPerToken:                  5.00e-6,
			CacheCreationInputTokenCost:         1.25e-6,
			CacheCreationInputTokenCostAbove1Hr: 2.00e-6,
			CacheReadInputTokenCost:             0.10e-6,
		},

		// Sonnet 4.5 / 4.6 — $3.00 / $15.00 / MTok (no Sonnet 4.7 as of 2026-05)
		"claude-sonnet-4-5": sonnetPrice(),
		"claude-sonnet-4-6": sonnetPrice(),

		// Opus 4.1 — legacy tier: $15.00 / $75.00 / MTok. Only the
		// 4.1 series remains at these rates; everything 4.5+ moved
		// to the lower tier with the 2026-04-16 reprice.
		"claude-opus-4-1": opusLegacyPrice(),

		// Opus 4.5 / 4.6 / 4.7 — current tier: $5.00 / $25.00 / MTok.
		// Verified against claude-code-hub's cloud-price-table for
		// 4.5 and against an actual Claude Platform on AWS invoice
		// for 4.6 ($6.25/MTok cache write 5m).
		"claude-opus-4-5": opusCurrentPrice(),
		"claude-opus-4-6": opusCurrentPrice(),
		"claude-opus-4-7": opusCurrentPrice(),
	}
}

func opusCurrentPrice() Price {
	return Price{
		InputCostPerToken:                   5.00e-6,
		OutputCostPerToken:                  25.00e-6,
		CacheCreationInputTokenCost:         6.25e-6,
		CacheCreationInputTokenCostAbove1Hr: 10.00e-6,
		CacheReadInputTokenCost:             0.50e-6,
	}
}

func sonnetPrice() Price {
	return Price{
		InputCostPerToken:                   3.00e-6,
		OutputCostPerToken:                  15.00e-6,
		CacheCreationInputTokenCost:         3.75e-6,
		CacheCreationInputTokenCostAbove1Hr: 6.00e-6,
		CacheReadInputTokenCost:             0.30e-6,
	}
}

func opusLegacyPrice() Price {
	return Price{
		InputCostPerToken:                   15.00e-6,
		OutputCostPerToken:                  75.00e-6,
		CacheCreationInputTokenCost:         18.75e-6,
		CacheCreationInputTokenCostAbove1Hr: 30.00e-6,
		CacheReadInputTokenCost:             1.50e-6,
	}
}

// Calculate returns the per-bucket breakdown plus rolled-up total in
// USD for one upstream call. The second return reports whether a
// price was found; unknown models return (Breakdown{}, false).
//
// Anthropic's SSE stream does not currently split
// cache_creation_input_tokens into 5m vs 1h. Until the sniffer learns
// to parse the breakdown, the full cache-creation count is billed at
// the 5m rate. The Breakdown's CacheCreation1h bucket therefore stays
// 0 in MVP traffic.
//
// Cache fallback: when an explicit cache rate is absent (e.g. an
// imported price entry only sets input_cost_per_token), the
// canonical Anthropic ratios are used so a thin upstream entry still
// produces a credible cost.
func (t Table) Calculate(model string, u usage.Usage) (Breakdown, bool) {
	p, ok := t.priceFor(model)
	if !ok {
		return Breakdown{}, false
	}

	cache5mRate := p.CacheCreationInputTokenCost
	if cache5mRate == 0 && p.InputCostPerToken > 0 {
		cache5mRate = p.InputCostPerToken * 1.25
	}
	cache1hRate := p.CacheCreationInputTokenCostAbove1Hr
	if cache1hRate == 0 && p.InputCostPerToken > 0 {
		cache1hRate = p.InputCostPerToken * 2.0
	}
	cacheReadRate := p.CacheReadInputTokenCost
	if cacheReadRate == 0 && p.InputCostPerToken > 0 {
		cacheReadRate = p.InputCostPerToken * 0.10
	}

	// Until we parse the 5m/1h breakdown out of SSE events, treat the
	// whole cache_creation_input_tokens count as 5m.
	b := Breakdown{
		Input:           float64(u.InputTokens) * p.InputCostPerToken,
		Output:          float64(u.OutputTokens) * p.OutputCostPerToken,
		CacheCreation5m: float64(u.CacheCreationInputTokens) * cache5mRate,
		CacheRead:       float64(u.CacheReadInputTokens) * cacheReadRate,
		// CacheCreation1h stays 0 until the sniffer reports 1h tokens.
	}
	_ = cache1hRate
	b.Total = b.Input + b.Output + b.CacheCreation5m + b.CacheCreation1h + b.CacheRead
	return b, true
}

func (t Table) priceFor(model string) (Price, bool) {
	if p, ok := t[model]; ok {
		return p, true
	}
	// Longest-prefix match. Map iteration order is randomised; we track
	// the longest hit independently of order so the result is
	// deterministic across runs.
	var (
		bestLen int
		best    Price
		found   bool
	)
	for prefix, p := range t {
		if !strings.HasPrefix(model, prefix) {
			continue
		}
		if len(prefix) > bestLen {
			bestLen = len(prefix)
			best = p
			found = true
		}
	}
	return best, found
}
