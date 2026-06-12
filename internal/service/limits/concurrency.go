package limits

import (
	"sync"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// ConcurrencyGuard tracks in-flight requests per account and enforces
// the per-account max_concurrency cap (migration 0037). Purely
// in-process: each gateway instance counts its own traffic, which is
// the correct scope for the single-writer deployments OmniHub targets
// today.
type ConcurrencyGuard struct {
	mu       sync.Mutex
	inflight map[int64]int
}

// NewConcurrencyGuard returns an empty guard.
func NewConcurrencyGuard() *ConcurrencyGuard {
	return &ConcurrencyGuard{inflight: make(map[int64]int)}
}

// TryAcquire reserves one in-flight slot for the account. max <= 0
// means unlimited (the slot is still counted so AtCap/observability
// stay accurate). Returns false when the account is at capacity.
func (g *ConcurrencyGuard) TryAcquire(accountID int64, max int) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if max > 0 && g.inflight[accountID] >= max {
		return false
	}
	g.inflight[accountID]++
	return true
}

// Release returns a slot taken by TryAcquire. Releasing below zero is
// clamped (defensive against double-release bugs).
func (g *ConcurrencyGuard) Release(accountID int64) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight[accountID] > 0 {
		g.inflight[accountID]--
	}
	if g.inflight[accountID] == 0 {
		delete(g.inflight, accountID)
	}
}

// AtCap reports whether the account has no free slot. Used by the
// resolver so sticky bindings and fresh selection skip saturated
// accounts instead of burning a failover attempt on them.
func (g *ConcurrencyGuard) AtCap(a *provider.Account) bool {
	if g == nil || a == nil || a.MaxConcurrency <= 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight[a.ID] >= a.MaxConcurrency
}

// InFlight returns the current in-flight count for an account
// (observability).
func (g *ConcurrencyGuard) InFlight(accountID int64) int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight[accountID]
}
