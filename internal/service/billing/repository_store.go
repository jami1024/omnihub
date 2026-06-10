package billing

import (
	"context"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
)

type planRepository interface {
	ActiveGrantForUser(ctx context.Context, userID int64, now time.Time) (*repository.UserPlanGrant, error)
	ConsumeGrantCredit(ctx context.Context, grantID, userID int64, amount float64, requestCreatedAt *time.Time) (float64, error)
}

type walletRepository interface {
	Credits(ctx context.Context, userID int64) (float64, error)
}

type billedRepository interface {
	SumBilledByUser(ctx context.Context, userID int64) (float64, error)
}

// RepositoryStore adapts the repository layer to the billing service.
// Wallet balance is wallet credits minus wallet-paid request usage; plan-paid
// usage is intentionally excluded by MessageRequestRepo.SumBilledByUser.
type RepositoryStore struct {
	plans  planRepository
	wallet walletRepository
	billed billedRepository
}

func NewRepositoryStore(plans planRepository, wallet walletRepository, billed billedRepository) *RepositoryStore {
	return &RepositoryStore{plans: plans, wallet: wallet, billed: billed}
}

func (s *RepositoryStore) ActiveGrantForUser(ctx context.Context, userID int64, now time.Time) (*ActiveGrant, error) {
	if s == nil || s.plans == nil {
		return nil, nil
	}
	grant, err := s.plans.ActiveGrantForUser(ctx, userID, now)
	if err != nil || grant == nil {
		return nil, err
	}
	return &ActiveGrant{
		ID:                 grant.ID,
		UserID:             grant.UserID,
		CreditRemainingUSD: grant.CreditRemainingUSD,
		PriceRatioSnapshot: grant.PriceRatioSnapshot,
		AllowPaygOverage:   grant.AllowPaygOverageSnapshot,
	}, nil
}

func (s *RepositoryStore) ConsumeGrantCredit(ctx context.Context, grantID, userID int64, amount float64, requestCreatedAt *time.Time) (float64, error) {
	if s == nil || s.plans == nil {
		return 0, nil
	}
	return s.plans.ConsumeGrantCredit(ctx, grantID, userID, amount, requestCreatedAt)
}

func (s *RepositoryStore) WalletBalance(ctx context.Context, userID int64) (float64, error) {
	if s == nil || s.wallet == nil || s.billed == nil {
		return 0, nil
	}
	credits, err := s.wallet.Credits(ctx, userID)
	if err != nil {
		return 0, err
	}
	spent, err := s.billed.SumBilledByUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	return credits - spent, nil
}
