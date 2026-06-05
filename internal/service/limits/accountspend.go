package limits

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// AccountSpendSource returns authoritative per-account USD spend. The
// production implementation is repository.MessageRequestRepo; the
// interface keeps the guard unit-testable without a DB.
type AccountSpendSource interface {
	// SumCostByAccount is the rolling 24h spend for one account.
	SumCostByAccount(ctx context.Context, accountName string) (float64, error)
	// TotalCostByAccount is the lifetime spend for one account.
	TotalCostByAccount(ctx context.Context, accountName string) (float64, error)
}

// AccountGuard enforces per-account daily / total USD spend caps. Spend
// is reloaded in the background on a fixed interval so the resolver
// hot-path stays a pure in-memory lookup, exactly like the health
// tracker. It is FAIL-OPEN: an account whose spend has not been measured
// yet, or whose limits are unset, is always routable. Only an account
// whose measured spend meets or exceeds a configured cap is skipped.
type AccountGuard struct {
	src AccountSpendSource

	mu    sync.RWMutex
	spend map[int64]accountSpend // keyed by account id
}

type accountSpend struct {
	daily float64
	total float64
}

// NewAccountGuard returns a guard backed by src. A nil src yields a
// guard that never blocks (every OverLimit call returns false).
func NewAccountGuard(src AccountSpendSource) *AccountGuard {
	return &AccountGuard{src: src, spend: make(map[int64]accountSpend)}
}

// OverLimit reports whether the account has reached a configured daily
// or total USD cap. Accounts with no cap, or not yet measured, return
// false.
func (g *AccountGuard) OverLimit(a *provider.Account) bool {
	if g == nil || g.src == nil || a == nil {
		return false
	}
	if a.DailyUSDLimit == nil && a.TotalUSDLimit == nil {
		return false
	}
	g.mu.RLock()
	s, ok := g.spend[a.ID]
	g.mu.RUnlock()
	if !ok {
		return false // not yet measured → allow
	}
	if a.DailyUSDLimit != nil && s.daily >= *a.DailyUSDLimit {
		return true
	}
	if a.TotalUSDLimit != nil && s.total >= *a.TotalUSDLimit {
		return true
	}
	return false
}

// DailySpend returns the last-measured rolling-24h USD spend for an
// account id. The second result is false when the account has no measured
// spend yet (no cap configured, or not refreshed). Intended for
// observability (metrics gauges), not enforcement.
func (g *AccountGuard) DailySpend(id int64) (float64, bool) {
	if g == nil {
		return 0, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.spend[id]
	if !ok {
		return 0, false
	}
	return s.daily, true
}

// Refresh reloads spend for every account that has at least one cap
// configured. Accounts without caps are skipped (no query) and dropped
// from the map. A per-account query error is logged and leaves that
// account unmeasured (fail-open).
func (g *AccountGuard) Refresh(ctx context.Context, accounts []*provider.Account) {
	if g == nil || g.src == nil {
		return
	}
	next := make(map[int64]accountSpend, len(accounts))
	for _, a := range accounts {
		if a.DailyUSDLimit == nil && a.TotalUSDLimit == nil {
			continue
		}
		var s accountSpend
		if a.DailyUSDLimit != nil {
			if v, err := g.src.SumCostByAccount(ctx, a.Name); err == nil {
				s.daily = v
			} else {
				slog.Warn("account spend refresh (daily) failed; account left unmeasured",
					"account", a.Name, "err", err.Error())
				continue
			}
		}
		if a.TotalUSDLimit != nil {
			if v, err := g.src.TotalCostByAccount(ctx, a.Name); err == nil {
				s.total = v
			} else {
				slog.Warn("account spend refresh (total) failed; account left unmeasured",
					"account", a.Name, "err", err.Error())
				continue
			}
		}
		next[a.ID] = s
	}
	g.mu.Lock()
	g.spend = next
	g.mu.Unlock()
}

// Start performs one synchronous Refresh, then re-refreshes every
// interval until ctx is cancelled. snapshot supplies the current account
// set each tick (typically account.Pool.All), so newly added accounts
// and limit edits are picked up automatically.
func (g *AccountGuard) Start(ctx context.Context, snapshot func() []*provider.Account, interval time.Duration) {
	if g == nil || g.src == nil {
		return
	}
	g.Refresh(ctx, snapshot())
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				g.Refresh(ctx, snapshot())
			}
		}
	}()
}
