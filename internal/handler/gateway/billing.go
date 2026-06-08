package gateway

import (
	"context"
	"log/slog"

	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/billing"
	"github.com/jami1024/omnihub/internal/service/limits"
)

type BillingCharger interface {
	Charge(ctx context.Context, userID int64, amount float64) (billing.Result, error)
}

func chargeBilling(ctx context.Context, k *apikey.Key, billed *float64, charger BillingCharger, limiter *limits.Limiter) billing.Result {
	if k == nil || k.UserID == nil || billed == nil || *billed <= 0 {
		return billing.Result{}
	}
	if charger == nil {
		limiter.RecordWalletSpend(k, *billed)
		return billing.Result{WalletUSD: *billed}
	}
	res, err := charger.Charge(ctx, *k.UserID, *billed)
	if err != nil {
		// The response has already been committed by the time exact token
		// usage is known. Preserve the old pay-as-you-go behaviour as a
		// fallback, and let logs surface the billing inconsistency.
		slog.Warn("request billing charge failed; falling back to wallet cache debit",
			"key", k.Name, "user", *k.UserID, "amount", *billed, "err", err.Error())
		limiter.RecordWalletSpend(k, *billed)
		return billing.Result{WalletUSD: *billed}
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
