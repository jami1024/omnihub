package gateway

import (
	"context"
	"log/slog"

	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/billing"
	"github.com/jami1024/omnihub/internal/service/limits"
)

type BillingCharger interface {
	Charge(ctx context.Context, userID int64, cost, userRatio float64, mode apikey.BillingMode) (billing.Result, error)
}

// chargeBilling debits a completed request according to the key's billing mode
// (derived from k). cost is the raw upstream cost; the effective billed amount
// and its plan/wallet split come back in the Result, and the per-(user,mode)
// balance caches are folded via RecordBillingSpend.
func chargeBilling(ctx context.Context, k *apikey.Key, cost *float64, charger BillingCharger, limiter *limits.Limiter) billing.Result {
	if k == nil || k.UserID == nil || cost == nil || *cost <= 0 {
		return billing.Result{}
	}
	mode := apikey.ModePayg
	if k.BillingMode == apikey.ModePlan {
		mode = apikey.ModePlan
	}
	if charger == nil {
		// Billing disabled globally: no plan store exists and no grants can be
		// active, so every key bills the wallet at the user ratio.
		billed := *cost * k.PriceRatio
		limiter.RecordBillingSpend(k, 0, billed)
		return billing.Result{BilledUSD: billed, WalletUSD: billed}
	}
	res, err := charger.Charge(ctx, *k.UserID, *cost, k.PriceRatio, mode)
	if err != nil {
		// The response is already committed by the time exact token usage is
		// known, so the charge cannot be unwound. For a PAYG key, preserve the
		// pay-as-you-go fallback (debit the wallet so the spend is not lost).
		// For a PLAN key, do NOT silently debit the wallet — a plan key must
		// never spend wallet credit outside its grant's overage rules; record
		// the billed amount for the row but push no wallet decrement. This is a
		// genuine, logged billing-inconsistency window.
		billed := *cost * k.PriceRatio
		slog.Warn("request billing charge failed; billing fallback applied",
			"key", k.Name, "user", *k.UserID, "mode", mode, "cost", *cost, "err", err.Error())
		if mode == apikey.ModePlan {
			return billing.Result{BilledUSD: billed}
		}
		limiter.RecordBillingSpend(k, 0, billed)
		return billing.Result{BilledUSD: billed, WalletUSD: billed}
	}
	limiter.RecordBillingSpend(k, res.PlanUSD, res.WalletUSD)
	return res
}

func floatPtrIfPositive(v float64) *float64 {
	if v <= 0 {
		return nil
	}
	return &v
}
