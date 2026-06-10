package billing

import (
	"context"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

type fakeStore struct {
	grantCredit   float64
	walletBalance float64
	allowPayg     bool
	hasGrant      bool // when false, ActiveGrantForUser returns nil
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
	if !f.hasGrant {
		return nil, nil
	}
	return &ActiveGrant{
		ID: 1, UserID: 7, CreditRemainingUSD: f.grantCredit,
		PriceRatioSnapshot: f.planRatio(), AllowPaygOverage: f.allowPayg,
	}, nil
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

// --- plan mode --------------------------------------------------------------

func TestChargePlanModeUsesPlanFirstThenWallet(t *testing.T) {
	store := &fakeStore{hasGrant: true, grantCredit: 3, walletBalance: 10, allowPayg: true}
	res, err := New(store).Charge(context.Background(), 7, 5, 1, apikey.ModePlan)
	if err != nil {
		t.Fatal(err)
	}
	if res.BilledUSD != 5 || res.PlanUSD != 3 || res.WalletUSD != 2 {
		t.Fatalf("got %+v", res)
	}
}

func TestChargePlanModeRejectsWhenExhaustedAndNoOverage(t *testing.T) {
	store := &fakeStore{hasGrant: true, grantCredit: 1, walletBalance: 10, allowPayg: false}
	if _, err := New(store).Charge(context.Background(), 7, 2, 1, apikey.ModePlan); err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestChargePlanModeRejectsWithoutGrant(t *testing.T) {
	store := &fakeStore{hasGrant: false, walletBalance: 100}
	if _, err := New(store).Charge(context.Background(), 7, 2, 1, apikey.ModePlan); err != ErrInsufficientBalance {
		t.Fatalf("plan key with no grant must reject (no payg fallback), got %v", err)
	}
}

// The plan's snapshot ratio governs, not the user ratio: $2 cost at plan ratio
// 1.5 bills $3 from plan credit; userRatio 0.5 is deliberately ignored.
func TestChargePlanModeUsesPlanRatio(t *testing.T) {
	store := &fakeStore{hasGrant: true, grantCredit: 10, allowPayg: false, ratio: 1.5}
	res, err := New(store).Charge(context.Background(), 7, 2, 0.5, apikey.ModePlan)
	if err != nil {
		t.Fatal(err)
	}
	if res.BilledUSD != 3 || res.PlanUSD != 3 || res.WalletUSD != 0 {
		t.Fatalf("expected billed=3 plan=3 wallet=0, got %+v", res)
	}
}

// --- payg mode --------------------------------------------------------------

func TestChargePaygModeBillsWalletAtUserRatio(t *testing.T) {
	store := &fakeStore{walletBalance: 10}
	res, err := New(store).Charge(context.Background(), 7, 2, 1.5, apikey.ModePayg)
	if err != nil {
		t.Fatal(err)
	}
	if res.BilledUSD != 3 || res.PlanUSD != 0 || res.WalletUSD != 3 {
		t.Fatalf("expected billed=3 plan=0 wallet=3, got %+v", res)
	}
}

// A payg key must NEVER consume plan credit, even when the user holds an active
// grant.
func TestChargePaygModeIgnoresActiveGrant(t *testing.T) {
	store := &fakeStore{hasGrant: true, grantCredit: 10, walletBalance: 10, allowPayg: true}
	res, err := New(store).Charge(context.Background(), 7, 2, 1, apikey.ModePayg)
	if err != nil {
		t.Fatal(err)
	}
	if res.PlanUSD != 0 || res.WalletUSD != 2 || store.consumed != 0 {
		t.Fatalf("payg drew plan credit: res=%+v consumed=%v", res, store.consumed)
	}
}

func TestChargePaygModeRejectsWhenWalletShort(t *testing.T) {
	store := &fakeStore{walletBalance: 1}
	if _, err := New(store).Charge(context.Background(), 7, 2, 1, apikey.ModePayg); err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

// --- AvailableBalance -------------------------------------------------------

func TestAvailableBalancePlanModeIncludesPlanCreditForZeroWallet(t *testing.T) {
	store := &fakeStore{hasGrant: true, grantCredit: 0.5, walletBalance: 0, allowPayg: true}
	bal, err := New(store).AvailableBalance(context.Background(), 7, apikey.ModePlan)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 0.5 {
		t.Fatalf("expected 0.5, got %v", bal)
	}
}

func TestAvailableBalancePlanModeSumsPlanAndWalletWithOverage(t *testing.T) {
	store := &fakeStore{hasGrant: true, grantCredit: 3, walletBalance: 10, allowPayg: true}
	bal, err := New(store).AvailableBalance(context.Background(), 7, apikey.ModePlan)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 13 {
		t.Fatalf("expected 13, got %v", bal)
	}
}

func TestAvailableBalancePlanModeExcludesWalletWithoutOverage(t *testing.T) {
	store := &fakeStore{hasGrant: true, grantCredit: 3, walletBalance: 10, allowPayg: false}
	bal, err := New(store).AvailableBalance(context.Background(), 7, apikey.ModePlan)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 3 {
		t.Fatalf("no-overage plan must exclude wallet; expected 3, got %v", bal)
	}
}

func TestAvailableBalancePlanModeZeroWithoutGrant(t *testing.T) {
	store := &fakeStore{hasGrant: false, walletBalance: 100}
	bal, err := New(store).AvailableBalance(context.Background(), 7, apikey.ModePlan)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 0 {
		t.Fatalf("plan key with no grant must gate at 0, got %v", bal)
	}
}

func TestAvailableBalancePaygModeIsWalletOnly(t *testing.T) {
	// Active grant present, but a payg key sees only the wallet.
	store := &fakeStore{hasGrant: true, grantCredit: 100, walletBalance: 4, allowPayg: true}
	bal, err := New(store).AvailableBalance(context.Background(), 7, apikey.ModePayg)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 4 {
		t.Fatalf("payg available must be wallet-only; expected 4, got %v", bal)
	}
}
