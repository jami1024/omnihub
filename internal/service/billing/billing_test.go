package billing

import (
	"context"
	"testing"
	"time"
)

type fakeStore struct {
	grantCredit   float64
	walletBalance float64
	allowPayg     bool
	ratio         float64
	consumed      float64
}

func (f *fakeStore) planRatio() float64 {
	if f.ratio == 0 {
		return 1
	}
	return f.ratio
}

func (f *fakeStore) ActiveGrantForUser(context.Context, int64, time.Time) (*ActiveGrant, error) {
	if f.grantCredit <= 0 && !f.allowPayg {
		return &ActiveGrant{ID: 1, UserID: 7, CreditRemainingUSD: f.grantCredit, PriceRatioSnapshot: f.planRatio(), AllowPaygOverage: false}, nil
	}
	if f.grantCredit <= 0 {
		return nil, nil
	}
	return &ActiveGrant{ID: 1, UserID: 7, CreditRemainingUSD: f.grantCredit, PriceRatioSnapshot: f.planRatio(), AllowPaygOverage: f.allowPayg}, nil
}

func (f *fakeStore) ConsumeGrantCredit(_ context.Context, _, _ int64, amount float64, _ *time.Time) (float64, error) {
	used := amount
	if f.grantCredit < amount {
		used = f.grantCredit
	}
	f.consumed += used
	f.grantCredit -= used
	return used, nil
}

func (f *fakeStore) WalletBalance(context.Context, int64) (float64, error) {
	return f.walletBalance, nil
}

func TestChargeUsesPlanFirstThenWallet(t *testing.T) {
	store := &fakeStore{grantCredit: 3, walletBalance: 10, allowPayg: true}
	svc := New(store)
	res, err := svc.Charge(context.Background(), 7, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.BilledUSD != 5 || res.PlanUSD != 3 || res.WalletUSD != 2 {
		t.Fatalf("got %+v", res)
	}
}

func TestChargeRejectsWhenPlanExhaustedAndNoOverage(t *testing.T) {
	store := &fakeStore{grantCredit: 1, walletBalance: 10, allowPayg: false}
	svc := New(store)
	if _, err := svc.Charge(context.Background(), 7, 2, 1); err == nil {
		t.Fatal("expected rejection")
	}
}

// With an active grant, billing uses the PLAN's snapshot ratio, not the
// user's pay-as-you-go ratio: a $2 cost at plan ratio 1.5 bills $3, drawn
// from plan credit. The userRatio argument (0.5 here) is deliberately
// different and must be ignored while the plan governs the price.
func TestChargeUsesPlanRatioWhenGranted(t *testing.T) {
	store := &fakeStore{grantCredit: 10, walletBalance: 0, allowPayg: false, ratio: 1.5}
	svc := New(store)
	res, err := svc.Charge(context.Background(), 7, 2, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if res.BilledUSD != 3 || res.PlanUSD != 3 || res.WalletUSD != 0 {
		t.Fatalf("expected billed=3 plan=3 wallet=0, got %+v", res)
	}
}

// Without an active grant, billing falls back to the user's pay-as-you-go
// ratio: a $2 cost at user ratio 1.5 bills $3 against the wallet.
func TestChargeUsesUserRatioWithoutGrant(t *testing.T) {
	store := &fakeStore{grantCredit: 0, walletBalance: 10, allowPayg: true}
	svc := New(store)
	res, err := svc.Charge(context.Background(), 7, 2, 1.5)
	if err != nil {
		t.Fatal(err)
	}
	if res.BilledUSD != 3 || res.PlanUSD != 0 || res.WalletUSD != 3 {
		t.Fatalf("expected billed=3 plan=0 wallet=3, got %+v", res)
	}
}

func TestAvailableBalanceIncludesPlanCreditForZeroWalletUser(t *testing.T) {
	// Plan-only user: $0 wallet, $0.50 active plan credit. Admission must
	// see a positive balance, otherwise the gate 402s them before plan
	// credit is ever consulted.
	store := &fakeStore{grantCredit: 0.5, walletBalance: 0, allowPayg: true}
	svc := New(store)
	bal, err := svc.AvailableBalance(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 0.5 {
		t.Fatalf("expected available balance 0.5, got %v", bal)
	}
}

func TestAvailableBalanceSumsPlanAndWallet(t *testing.T) {
	store := &fakeStore{grantCredit: 3, walletBalance: 10, allowPayg: true}
	svc := New(store)
	bal, err := svc.AvailableBalance(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 13 {
		t.Fatalf("expected available balance 13, got %v", bal)
	}
}

func TestAvailableBalanceFallsBackToWalletWithoutPlan(t *testing.T) {
	// No active grant (grantCredit 0, overage allowed → store returns nil):
	// available balance is just the wallet.
	store := &fakeStore{grantCredit: 0, walletBalance: 4, allowPayg: true}
	svc := New(store)
	bal, err := svc.AvailableBalance(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 4 {
		t.Fatalf("expected available balance 4, got %v", bal)
	}
}
