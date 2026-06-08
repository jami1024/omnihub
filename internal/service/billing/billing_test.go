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
	consumed      float64
}

func (f *fakeStore) ActiveGrantForUser(context.Context, int64, time.Time) (*ActiveGrant, error) {
	if f.grantCredit <= 0 && !f.allowPayg {
		return &ActiveGrant{ID: 1, UserID: 7, CreditRemainingUSD: f.grantCredit, AllowPaygOverage: false}, nil
	}
	if f.grantCredit <= 0 {
		return nil, nil
	}
	return &ActiveGrant{ID: 1, UserID: 7, CreditRemainingUSD: f.grantCredit, AllowPaygOverage: f.allowPayg}, nil
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
	res, err := svc.Charge(context.Background(), 7, 5)
	if err != nil {
		t.Fatal(err)
	}
	if res.PlanUSD != 3 || res.WalletUSD != 2 {
		t.Fatalf("got %+v", res)
	}
}

func TestChargeRejectsWhenPlanExhaustedAndNoOverage(t *testing.T) {
	store := &fakeStore{grantCredit: 1, walletBalance: 10, allowPayg: false}
	svc := New(store)
	if _, err := svc.Charge(context.Background(), 7, 2); err == nil {
		t.Fatal("expected rejection")
	}
}
