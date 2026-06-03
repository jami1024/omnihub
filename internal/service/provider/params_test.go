package provider

import (
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
)

func ip(v int) *int         { return &v }
func fp(v float64) *float64 { return &v }

func TestParamOverridesApplyTo(t *testing.T) {
	temp := 0.7
	req := &ir.UnifiedRequest{
		Model:       "m",
		MaxTokens:   100,
		Temperature: &temp,
	}
	ov := ParamOverrides{
		MaxTokens:      ip(4096),
		Temperature:    fp(0.0),
		TopP:           fp(0.9),
		ThinkingBudget: ip(8000),
	}
	ov.ApplyTo(req)

	if req.MaxTokens != 4096 {
		t.Errorf("MaxTokens: want 4096, got %d", req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != 0.0 {
		t.Errorf("Temperature: want 0.0, got %v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("TopP: want 0.9, got %v", req.TopP)
	}
	if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 8000 {
		t.Errorf("Thinking: want enabled/8000, got %+v", req.Thinking)
	}
}

func TestParamOverridesEmptyIsNoop(t *testing.T) {
	temp := 0.5
	req := &ir.UnifiedRequest{MaxTokens: 50, Temperature: &temp}
	var ov ParamOverrides
	if ov.Any() {
		t.Fatal("zero ParamOverrides should report Any()==false")
	}
	ov.ApplyTo(req)
	if req.MaxTokens != 50 || req.Temperature == nil || *req.Temperature != 0.5 || req.Thinking != nil {
		t.Errorf("empty overrides must not change the request: %+v", req)
	}
}

// Overrides must NOT alias the override pointers into the request (the
// account is shared and reused across requests).
func TestParamOverridesNoAlias(t *testing.T) {
	ov := ParamOverrides{Temperature: fp(0.2)}
	req := &ir.UnifiedRequest{}
	ov.ApplyTo(req)
	*req.Temperature = 1.9 // mutate the request's copy
	if *ov.Temperature != 0.2 {
		t.Errorf("override pointer was aliased into the request; got %v", *ov.Temperature)
	}
}
