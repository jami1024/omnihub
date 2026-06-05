package limits

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// BalanceSource returns the authoritative prepaid balance (lifetime
// credits minus lifetime request cost) for a portal user. Production
// wiring combines the wallet ledger and message_requests; the interface
// keeps the guard unit-testable without a DB.
type BalanceSource interface {
	Balance(ctx context.Context, userID int64) (float64, error)
}

// BalanceFunc adapts a plain function to BalanceSource.
type BalanceFunc func(ctx context.Context, userID int64) (float64, error)

// Balance implements BalanceSource.
func (f BalanceFunc) Balance(ctx context.Context, userID int64) (float64, error) {
	return f(ctx, userID)
}

// BalanceGuard memoises per-user prepaid balance with the same
// stale-while-revalidate discipline as SpendCache: a known user is served
// from memory and never blocks; a stale entry is returned immediately
// while a single background goroutine reloads it; only a cold user blocks
// once to seed an authoritative base. Charge folds a just-completed
// request cost in immediately; Credit folds a top-up in — so neither has
// to wait for the next DB refresh.
type BalanceGuard struct {
	src BalanceSource
	ttl time.Duration

	mu      sync.Mutex
	entries map[int64]*balanceEntry
	now     func() time.Time
}

type balanceEntry struct {
	usd         float64
	refreshedAt time.Time
	refreshing  bool
}

// NewBalanceGuard returns a guard backed by src.
func NewBalanceGuard(src BalanceSource, ttl time.Duration) *BalanceGuard {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &BalanceGuard{
		src:     src,
		ttl:     ttl,
		entries: make(map[int64]*balanceEntry),
		now:     time.Now,
	}
}

// Balance returns the prepaid balance for userID, serving a known user
// from memory (refreshing stale entries in the background) and blocking
// only on a cold user to seed the cache.
func (g *BalanceGuard) Balance(ctx context.Context, userID int64) (float64, error) {
	g.mu.Lock()
	if e, ok := g.entries[userID]; ok {
		usd := e.usd
		if g.now().Sub(e.refreshedAt) >= g.ttl && !e.refreshing {
			e.refreshing = true
			g.mu.Unlock()
			go g.refresh(userID)
			return usd, nil
		}
		g.mu.Unlock()
		return usd, nil
	}
	g.mu.Unlock()

	usd, err := g.src.Balance(ctx, userID)
	if err != nil {
		return 0, err
	}
	g.mu.Lock()
	if _, exists := g.entries[userID]; !exists {
		g.entries[userID] = &balanceEntry{usd: usd, refreshedAt: g.now()}
	}
	usd = g.entries[userID].usd
	g.mu.Unlock()
	return usd, nil
}

// refresh reloads one user's balance in the background. On error the
// entry is left intact (serving stale) and re-armed for a later retry.
func (g *BalanceGuard) refresh(userID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	usd, err := g.src.Balance(ctx, userID)

	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.entries[userID]
	if !ok {
		return
	}
	if err != nil {
		e.refreshing = false
		slog.Warn("balance guard background refresh failed; serving stale",
			"user", userID, "err", err.Error())
		return
	}
	e.usd = usd
	e.refreshedAt = g.now()
	e.refreshing = false
}

// Charge debits a completed request's cost from the cached balance so the
// next request from the same user sees up-to-date data. A no-op when the
// user has no cached entry (the next Balance call seeds it from the DB,
// which already reflects the cost via message_requests).
func (g *BalanceGuard) Charge(userID int64, usd float64) {
	if usd <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.entries[userID]; ok {
		e.usd -= usd
	}
}

// Credit folds a just-applied top-up into the cached balance so the
// effect is immediate rather than waiting for the next refresh.
func (g *BalanceGuard) Credit(userID int64, usd float64) {
	if usd == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.entries[userID]; ok {
		e.usd += usd
	}
}
