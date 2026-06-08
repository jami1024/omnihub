package repository

import "testing"

func TestValidatePlanRejectsNegativeCredit(t *testing.T) {
	p := Plan{Name: "Starter", IncludedCreditUSD: -1, PriceRatio: 1, Enabled: true}
	if err := ValidatePlan(p); err == nil {
		t.Fatal("expected negative credit rejected")
	}
}

func TestValidatePlanAcceptsPaygOveragePlan(t *testing.T) {
	p := Plan{Name: "Starter", Description: "Basic", PriceUSD: 9, IncludedCreditUSD: 10, ValidDays: intPtr(30), PriceRatio: 1, AllowPaygOverage: true, Enabled: true}
	if err := ValidatePlan(p); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
}

func intPtr(v int) *int { return &v }
