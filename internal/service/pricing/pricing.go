// Package pricing turns token-usage counts into a USD cost.
//
// The MVP ships a hard-coded table of Anthropic Claude prices that
// covers direct Anthropic and Claude Platform on AWS (both speak the
// Messages API and bill at the same rates). A future commit will load
// the table from YAML so operators can update prices without
// rebuilding the binary.
//
// Pricing precision: prices are stored as float64 USD per 1M tokens
// and the resulting cost is a float64 USD. We deliberately accept
// ~1e-7 USD precision drift in exchange for a simpler API; the lossy
// bit is many orders of magnitude smaller than any usable accounting
// unit (cents, etc.).
package pricing

import (
	"strings"

	"github.com/jami1024/omnihub/internal/service/usage"
)

// Price captures the per-token-type rates for one model, expressed in
// USD per 1 000 000 tokens.
type Price struct {
	InputPerMillion           float64
	OutputPerMillion          float64
	CacheCreation5mPerMillion float64
	CacheCreation1hPerMillion float64
	CacheReadPerMillion       float64
}

// Table maps a model-name prefix to its Price. Lookup uses the
// longest matching prefix, so "claude-haiku-4-5-20251001" resolves
// against the "claude-haiku-4-5" row.
type Table map[string]Price

// Default returns the built-in Anthropic price list. Numbers reflect
// the public price sheet for Claude 4.5 / 4.6 / 4.7 models. They are
// shared by direct Anthropic and Claude Platform on AWS — both bill
// at Anthropic's rates.
//
// To update prices: edit this map and ship a new release.
func Default() Table {
	return Table{
		"claude-haiku-4-5":  {InputPerMillion: 1.00, OutputPerMillion: 5.00, CacheCreation5mPerMillion: 1.25, CacheCreation1hPerMillion: 2.00, CacheReadPerMillion: 0.10},
		"claude-sonnet-4-5": {InputPerMillion: 3.00, OutputPerMillion: 15.00, CacheCreation5mPerMillion: 3.75, CacheCreation1hPerMillion: 6.00, CacheReadPerMillion: 0.30},
		"claude-sonnet-4-6": {InputPerMillion: 3.00, OutputPerMillion: 15.00, CacheCreation5mPerMillion: 3.75, CacheCreation1hPerMillion: 6.00, CacheReadPerMillion: 0.30},
		"claude-sonnet-4-7": {InputPerMillion: 3.00, OutputPerMillion: 15.00, CacheCreation5mPerMillion: 3.75, CacheCreation1hPerMillion: 6.00, CacheReadPerMillion: 0.30},
		"claude-opus-4-5":   {InputPerMillion: 15.00, OutputPerMillion: 75.00, CacheCreation5mPerMillion: 18.75, CacheCreation1hPerMillion: 30.00, CacheReadPerMillion: 1.50},
		"claude-opus-4-6":   {InputPerMillion: 15.00, OutputPerMillion: 75.00, CacheCreation5mPerMillion: 18.75, CacheCreation1hPerMillion: 30.00, CacheReadPerMillion: 1.50},
		"claude-opus-4-7":   {InputPerMillion: 15.00, OutputPerMillion: 75.00, CacheCreation5mPerMillion: 18.75, CacheCreation1hPerMillion: 30.00, CacheReadPerMillion: 1.50},
	}
}

// Calculate returns the USD cost of one upstream call together with a
// flag indicating whether a price was found. Unknown models return
// (0, false); the caller logs a warning and persists NULL.
//
// Anthropic does not split cache_creation_input_tokens into 5m vs 1h
// in the basic streaming events; we approximate by charging the 5m
// rate. When the breakdown becomes available the API will be extended
// without changing this signature.
func (t Table) Calculate(model string, u usage.Usage) (float64, bool) {
	p, ok := t.priceFor(model)
	if !ok {
		return 0, false
	}
	cost := costOfTokens(u.InputTokens, p.InputPerMillion) +
		costOfTokens(u.OutputTokens, p.OutputPerMillion) +
		costOfTokens(u.CacheCreationInputTokens, p.CacheCreation5mPerMillion) +
		costOfTokens(u.CacheReadInputTokens, p.CacheReadPerMillion)
	return cost, true
}

func costOfTokens(count int64, pricePerMillion float64) float64 {
	if count == 0 {
		return 0
	}
	return float64(count) * pricePerMillion / 1_000_000
}

func (t Table) priceFor(model string) (Price, bool) {
	if p, ok := t[model]; ok {
		return p, true
	}
	// Longest-prefix match. Iteration order on a Go map is
	// randomised; tracking the longest hit independently of order
	// makes the result deterministic.
	var (
		bestKeyLen int
		best       Price
		found      bool
	)
	for prefix, p := range t {
		if !strings.HasPrefix(model, prefix) {
			continue
		}
		if len(prefix) > bestKeyLen {
			bestKeyLen = len(prefix)
			best = p
			found = true
		}
	}
	return best, found
}
