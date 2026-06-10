package gateway

import (
	"context"
	"log/slog"

	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/billing"
	"github.com/jami1024/omnihub/internal/service/limits"
)

type BillingCharger interface {
	Charge(ctx context.Context, userID int64, cost, userRatio float64) (billing.Result, error)
}

// chargeBilling debits a completed request. cost is the raw upstream cost; the
// effective billed amount (cost × the governing plan or user ratio) and its
// plan/wallet split come back in the Result. When billing is disabled or the
// charge fails, it falls back to the owner's pay-as-you-go ratio against the
// wallet cache.
func chargeBilling(ctx context.Context, k *apikey.Key, cost *float64, charger BillingCharger, limiter *limits.Limiter) billing.Result {
	if k == nil || k.UserID == nil || cost == nil || *cost <= 0 {
		return billing.Result{}
	}
	if charger == nil {
		billed := *cost * k.PriceRatio
		limiter.RecordWalletSpend(k, billed)
		return billing.Result{BilledUSD: billed, WalletUSD: billed}
	}
	res, err := charger.Charge(ctx, *k.UserID, *cost, k.PriceRatio)
	if err != nil {
		// The response has already been committed by the time exact token
		// usage is known. Preserve the old pay-as-you-go behaviour as a
		// fallback, and let logs surface the billing inconsistency.
		billed := *cost * k.PriceRatio
		slog.Warn("request billing charge failed; falling back to wallet cache debit",
			"key", k.Name, "user", *k.UserID, "cost", *cost, "err", err.Error())
		limiter.RecordWalletSpend(k, billed)
		return billing.Result{BilledUSD: billed, WalletUSD: billed}
	}
	limiter.RecordWalletSpend(k, res.WalletUSD)
	return res
}

func floatPtrIfPositive(v float64) *float64 {
	if v <= 0 {
		return nil
	}
	return &v
}
