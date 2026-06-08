package billing

import (
	"context"
	"errors"
	"time"
)

var ErrInsufficientBalance = errors.New("insufficient plan credit or wallet balance")

type ActiveGrant struct {
	ID                 int64
	UserID             int64
	CreditRemainingUSD float64
	AllowPaygOverage   bool
}

type Store interface {
	ActiveGrantForUser(ctx context.Context, userID int64, now time.Time) (*ActiveGrant, error)
	ConsumeGrantCredit(ctx context.Context, grantID, userID int64, amount float64, requestCreatedAt *time.Time) (float64, error)
	WalletBalance(ctx context.Context, userID int64) (float64, error)
}

type Result struct {
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

func (s *Service) Charge(ctx context.Context, userID int64, amount float64) (Result, error) {
	if amount <= 0 {
		return Result{}, nil
	}
	grant, err := s.store.ActiveGrantForUser(ctx, userID, s.now())
	if err != nil {
		return Result{}, err
	}
	remaining := amount
	var out Result
	if grant != nil && grant.CreditRemainingUSD > 0 {
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
		if !grant.AllowPaygOverage {
			return Result{}, ErrInsufficientBalance
		}
	} else if grant != nil && !grant.AllowPaygOverage {
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
