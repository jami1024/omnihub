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
	res       billing.Result
}

func (f *fakeBillingCharger) Charge(_ context.Context, userID int64, cost, userRatio float64) (billing.Result, error) {
	f.gotUserID = userID
	f.gotCost = cost
	f.gotRatio = userRatio
	return f.res, nil
}

func TestChargeBillingRecordsSplitAndDebitsOnlyWalletAmount(t *testing.T) {
	uid := int64(7)
	grantID := int64(11)
	cost := 5.0
	charger := &fakeBillingCharger{res: billing.Result{BilledUSD: 5, PlanUSD: 3, WalletUSD: 2, PlanGrantID: &grantID}}

	balSrc := limits.BalanceFunc(func(context.Context, int64) (float64, error) { return 10, nil })
	balance := limits.NewBalanceGuard(balSrc, time.Hour)
	limiter := limits.New(nil, nil)
	limiter.SetBalanceGuard(balance)
	_, _ = balance.Balance(context.Background(), uid)

	res := chargeBilling(context.Background(), &apikey.Key{Name: "alice", UserID: &uid, PriceRatio: 1.25}, &cost, charger, limiter)
	if res.PlanUSD != 3 || res.WalletUSD != 2 || res.PlanGrantID == nil || *res.PlanGrantID != grantID {
		t.Fatalf("split = %+v", res)
	}
	if charger.gotUserID != uid || charger.gotCost != cost || charger.gotRatio != 1.25 {
		t.Fatalf("charger got user=%d cost=%.2f ratio=%.2f", charger.gotUserID, charger.gotCost, charger.gotRatio)
	}
	if got, _ := balance.Balance(context.Background(), uid); got != 8 {
		t.Fatalf("wallet cache balance = %.2f, want 8.00", got)
	}
}
