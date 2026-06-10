package billing

import (
	"context"
	"errors"
	"time"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

var ErrInsufficientBalance = errors.New("insufficient plan credit or wallet balance")

type ActiveGrant struct {
	ID                 int64
	UserID             int64
	CreditRemainingUSD float64
	PriceRatioSnapshot float64
	AllowPaygOverage   bool
}

type Store interface {
	ActiveGrantForUser(ctx context.Context, userID int64, now time.Time) (*ActiveGrant, error)
	ConsumeGrantCredit(ctx context.Context, grantID, userID int64, amount float64, requestCreatedAt *time.Time) (float64, error)
	WalletBalance(ctx context.Context, userID int64) (float64, error)
}

type Result struct {
	// BilledUSD is the effective amount charged: cost × the governing ratio
	// (the active plan's snapshot ratio when the user holds a grant, else the
	// owner's pay-as-you-go ratio). It always equals PlanUSD + WalletUSD on
	// the success path.
	BilledUSD   float64
	PlanUSD     float64
	WalletUSD   float64
	PlanGrantID *int64
}

type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

// AvailableBalance reports a key's spendable balance for request admission,
// scoped to the key's billing mode so the gate matches exactly what Charge
// will draw down:
//
//   - payg: the pay-as-you-go wallet balance only. Plan credit is invisible.
//   - plan: the active grant's remaining credit, plus the wallet only when the
//     grant allows overage. No active grant ⇒ 0, so a plan key with no plan is
//     rejected at admission rather than silently falling back to the wallet.
func (s *Service) AvailableBalance(ctx context.Context, userID int64, mode apikey.BillingMode) (float64, error) {
	if mode == apikey.ModePlan {
		grant, err := s.store.ActiveGrantForUser(ctx, userID, s.now())
		if err != nil {
			return 0, err
		}
		if grant == nil {
			return 0, nil
		}
		avail := grant.CreditRemainingUSD
		if grant.AllowPaygOverage {
			wallet, err := s.store.WalletBalance(ctx, userID)
			if err != nil {
				return 0, err
			}
			avail += wallet
		}
		return avail, nil
	}
	return s.store.WalletBalance(ctx, userID)
}

// Charge bills a completed request according to the key's billing mode.
//
//   - payg: billed = cost × userRatio, drawn from the wallet only. The active
//     plan grant (if any) is never consulted, so a wallet key never spends
//     plan credit.
//   - plan: no active grant ⇒ ErrInsufficientBalance (never falls back to the
//     wallet at the user ratio). Otherwise billed = cost × the grant's snapshot
//     ratio, drawn plan-credit first, then wallet overage only when the grant
//     allows it.
func (s *Service) Charge(ctx context.Context, userID int64, cost, userRatio float64, mode apikey.BillingMode) (Result, error) {
	if cost <= 0 {
		return Result{}, nil
	}
	if mode == apikey.ModePlan {
		return s.chargePlan(ctx, userID, cost)
	}
	return s.chargePayg(ctx, userID, cost, userRatio)
}

// chargePayg draws the whole billed amount from the wallet at the user ratio.
func (s *Service) chargePayg(ctx context.Context, userID int64, cost, userRatio float64) (Result, error) {
	amount := cost * userRatio
	out := Result{BilledUSD: amount}
	if amount <= 0 {
		return out, nil
	}
	wallet, err := s.store.WalletBalance(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if wallet+1e-9 < amount {
		return Result{}, ErrInsufficientBalance
	}
	out.WalletUSD = amount
	return out, nil
}

// chargePlan draws plan credit first (at the grant's snapshot ratio), then
// wallet overage when the grant allows it. No active grant is a hard reject.
func (s *Service) chargePlan(ctx context.Context, userID int64, cost float64) (Result, error) {
	grant, err := s.store.ActiveGrantForUser(ctx, userID, s.now())
	if err != nil {
		return Result{}, err
	}
	if grant == nil {
		return Result{}, ErrInsufficientBalance
	}
	amount := cost * grant.PriceRatioSnapshot
	out := Result{BilledUSD: amount}
	if amount <= 0 {
		return out, nil
	}
	remaining := amount
	if grant.CreditRemainingUSD > 0 {
		used, err := s.store.ConsumeGrantCredit(ctx, grant.ID, userID, remaining, nil)
		if err != nil {
			return Result{}, err
		}
		out.PlanUSD = used
		out.PlanGrantID = &grant.ID
		remaining -= used
		if remaining <= 1e-9 {
			return out, nil
		}
	}
	if !grant.AllowPaygOverage {
		return Result{}, ErrInsufficientBalance
	}
	wallet, err := s.store.WalletBalance(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if wallet+1e-9 < remaining {
		return Result{}, ErrInsufficientBalance
	}
	out.WalletUSD = remaining
	return out, nil
}
