package provider

import "github.com/jami1024/omnihub/internal/ir"

// ParamOverrides are per-account generation-parameter overrides applied
// to a request before the driver builds it. Each field is optional (nil
// = leave the client's value untouched); a set field FORCES that value
// on every request routed through the account. This is a matched-pair
// override of IR-level fields the same driver then renders — NOT a
// cross-protocol transform.
//
// Typical uses: pin a deterministic temperature for a reproducibility
// account, cap max_tokens to bound cost, or force an extended-thinking
// budget for a reasoning-tuned account.
type ParamOverrides struct {
	MaxTokens      *int     `json:"max_tokens,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
	ThinkingBudget *int     `json:"thinking_budget_tokens,omitempty"`
}

// Any reports whether at least one override is configured.
func (p ParamOverrides) Any() bool {
	return p.MaxTokens != nil || p.Temperature != nil || p.TopP != nil || p.ThinkingBudget != nil
}

// ApplyTo mutates req in place, setting each configured override. The
// caller must pass a request it owns (a clone), since the shared request
// must stay intact for retries on other accounts.
func (p ParamOverrides) ApplyTo(req *ir.UnifiedRequest) {
	if req == nil {
		return
	}
	if p.MaxTokens != nil {
		req.MaxTokens = *p.MaxTokens
	}
	if p.Temperature != nil {
		t := *p.Temperature
		req.Temperature = &t
	}
	if p.TopP != nil {
		v := *p.TopP
		req.TopP = &v
	}
	if p.ThinkingBudget != nil {
		req.Thinking = &ir.ThinkingConfig{Type: "enabled", BudgetTokens: *p.ThinkingBudget}
	}
}
