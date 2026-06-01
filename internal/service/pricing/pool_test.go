package pricing

import (
	"testing"

	"github.com/jami1024/omnihub/internal/service/usage"
)

func TestOverlayDBWins(t *testing.T) {
	base := Table{
		"gpt-4o": {InputCostPerToken: 2.5e-6},
		"shared": {InputCostPerToken: 1e-6},
	}
	over := Table{
		"shared":  {InputCostPerToken: 9e-6}, // overrides base
		"gpt-5.2": {InputCostPerToken: 5e-6}, // new
	}
	got := Overlay(base, over)
	if got["gpt-4o"].InputCostPerToken != 2.5e-6 {
		t.Errorf("base-only entry lost: %v", got["gpt-4o"])
	}
	if got["shared"].InputCostPerToken != 9e-6 {
		t.Errorf("overlay should win on collision, got %v", got["shared"].InputCostPerToken)
	}
	if got["gpt-5.2"].InputCostPerToken != 5e-6 {
		t.Errorf("overlay-only entry missing")
	}
	// Inputs must be untouched.
	if len(base) != 2 || len(over) != 2 {
		t.Errorf("Overlay mutated an input")
	}
}

func TestPoolReplaceIsLive(t *testing.T) {
	p := NewPool(Table{"gpt-5.2": {InputCostPerToken: 5e-6, OutputCostPerToken: 1e-5}})
	u := usage.Usage{InputTokens: 1000, OutputTokens: 100}

	b, ok := p.Calculate("gpt-5.2", u)
	if !ok || b.Total == 0 {
		t.Fatalf("expected priced result, got ok=%v total=%v", ok, b.Total)
	}
	first := b.Total

	// Swap in a cheaper table; the pool must reflect it immediately.
	p.Replace(Table{"gpt-5.2": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6}})
	b2, ok := p.Calculate("gpt-5.2", u)
	if !ok || b2.Total >= first {
		t.Errorf("Replace not reflected: before=%v after=%v", first, b2.Total)
	}

	// Unknown model → not priced.
	if _, ok := p.Calculate("nonexistent", u); ok {
		t.Error("unknown model should not be priced")
	}
}
