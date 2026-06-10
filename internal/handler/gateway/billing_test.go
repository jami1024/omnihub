package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/billing"
	"github.com/jami1024/omnihub/internal/service/limits"
)

type fakeBillingCharger struct {
	gotUserID int64
	gotCost   float64
	gotRatio  float64
	gotMode   apikey.BillingMode
	res       billing.Result
	err       error
}

func (f *fakeBillingCharger) Charge(_ context.Context, userID int64, cost, userRatio float64, mode apikey.BillingMode) (billing.Result, error) {
	f.gotUserID = userID
	f.gotCost = cost
	f.gotRatio = userRatio
	f.gotMode = mode
	return f.res, f.err
}

func TestChargeBillingRecordsSplitAndDebitsBothPortions(t *testing.T) {
	uid := int64(7)
	grantID := int64(11)
	cost := 5.0
	charger := &fakeBillingCharger{res: billing.Result{BilledUSD: 5, PlanUSD: 3, WalletUSD: 2, PlanGrantID: &grantID}}

	balSrc := limits.BalanceFunc(func(context.Context, int64, apikey.BillingMode) (float64, error) { return 10, nil })
	balance := limits.NewBalanceGuard(balSrc, time.Hour)
	limiter := limits.New(nil, nil)
	limiter.SetBalanceGuard(balance)
	// A plan key: seed the plan-mode cache entry so the spend split is visible.
	k := &apikey.Key{Name: "alice", UserID: &uid, PriceRatio: 1.25, BillingMode: apikey.ModePlan}
	_, _ = balance.Balance(context.Background(), uid, apikey.ModePlan)

	res := chargeBilling(context.Background(), k, &cost, charger, limiter)
	if res.PlanUSD != 3 || res.WalletUSD != 2 || res.PlanGrantID == nil || *res.PlanGrantID != grantID {
		t.Fatalf("split = %+v", res)
	}
	if charger.gotUserID != uid || charger.gotCost != cost || charger.gotRatio != 1.25 || charger.gotMode != apikey.ModePlan {
		t.Fatalf("charger got user=%d cost=%.2f ratio=%.2f mode=%s", charger.gotUserID, charger.gotCost, charger.gotRatio, charger.gotMode)
	}
	// plan entry drops by plan(3)+wallet(2) = 5 → 10-5 = 5.
	if got, _ := balance.Balance(context.Background(), uid, apikey.ModePlan); got != 5 {
		t.Fatalf("plan cache balance = %.2f, want 5.00", got)
	}
}

// A plan key whose charge fails (e.g. grant exhausted, no overage — already
// committed) must NOT silently debit the wallet cache.
func TestChargeBillingPlanKeyErrorDoesNotDebitWallet(t *testing.T) {
	uid := int64(7)
	cost := 5.0
	charger := &fakeBillingCharger{err: billing.ErrInsufficientBalance}

	balSrc := limits.BalanceFunc(func(context.Context, int64, apikey.BillingMode) (float64, error) { return 10, nil })
	balance := limits.NewBalanceGuard(balSrc, time.Hour)
	limiter := limits.New(nil, nil)
	limiter.SetBalanceGuard(balance)
	k := &apikey.Key{Name: "planuser", UserID: &uid, PriceRatio: 1, BillingMode: apikey.ModePlan}
	_, _ = balance.Balance(context.Background(), uid, apikey.ModePlan)
	_, _ = balance.Balance(context.Background(), uid, apikey.ModePayg)

	res := chargeBilling(context.Background(), k, &cost, charger, limiter)
	if res.WalletUSD != 0 {
		t.Fatalf("plan-key error fallback must not bill wallet, got WalletUSD=%.2f", res.WalletUSD)
	}
	if got, _ := balance.Balance(context.Background(), uid, apikey.ModePlan); got != 10 {
		t.Fatalf("plan cache must be untouched, got %.2f", got)
	}
	if got, _ := balance.Balance(context.Background(), uid, apikey.ModePayg); got != 10 {
		t.Fatalf("wallet cache must be untouched, got %.2f", got)
	}
}
