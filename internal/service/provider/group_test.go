package provider

import "testing"

func TestEffectiveCostMultiplier(t *testing.T) {
	cases := []struct {
		name     string
		account  float64
		group    float64
		expected float64
	}{
		{"ungrouped default group", 2.0, 0, 2.0},   // group 0 → treated as 1
		{"grouped stacks", 2.0, 1.5, 3.0},          // 2 × 1.5
		{"group subsidy", 1.0, 0.5, 0.5},           // 1 × 0.5
		{"both one", 1.0, 1.0, 1.0},                // identity
	}
	for _, tc := range cases {
		a := &Account{CostMultiplier: tc.account, GroupCostMultiplier: tc.group}
		if got := a.EffectiveCostMultiplier(); got != tc.expected {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.expected)
		}
	}
	var nilAcc *Account
	if got := nilAcc.EffectiveCostMultiplier(); got != 1 {
		t.Errorf("nil account: got %v, want 1", got)
	}
}
