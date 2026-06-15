// Package authguard parks accounts that persistently fail upstream
// authentication and brings them back automatically once they recover.
//
// Why this exists: the circuit breaker (internal/service/health)
// deliberately ignores 4xx — a 401/403 is usually a client problem, not
// a sick upstream — so a revoked/expired api_key account would 401 on
// every request forever and never be taken out of rotation. authguard
// closes that gap: after N consecutive auth failures it flips the
// account's auth_status to "revoked" (which the resolver already
// skips), then a background sweeper probes parked accounts and restores
// any that start passing again.
//
// OAuth-backed accounts are NOT handled here — their 401s are recovered
// by the TokenManager (refresh / login_required), so authguard skips
// them to avoid double-parking.
package authguard

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

const (
	// DefaultThreshold is how many CONSECUTIVE auth failures park an
	// account. A single 401 (e.g. a transient blip) must not eject it.
	DefaultThreshold = 5
	// DefaultRecoveryInterval is how often parked accounts are re-probed.
	DefaultRecoveryInterval = 5 * time.Minute
)

// Parker writes the account auth_status: Park takes an account out of
// rotation (auth_status=revoked), Restore returns it (auth_status=ok).
// Implemented by an adapter over repository.AccountRepo.UpdateAuthRuntime.
type Parker interface {
	Park(ctx context.Context, accountID int64, reason string) error
	Restore(ctx context.Context, accountID int64) error
}

// Tester probes whether a parked account's credentials work again.
// Implemented by an adapter over the driver registry's connectivity test.
type Tester interface {
	Test(ctx context.Context, a *provider.Account) bool
}

// Guard tracks consecutive auth failures per account and parks/restores
// them. Safe for concurrent use.
type Guard struct {
	threshold int
	parker    Parker

	mu      sync.Mutex
	fails   map[int64]int  // consecutive 401/403 count
	tripped map[int64]bool // accounts THIS guard parked (for recovery)
}

// New returns a guard. threshold <= 0 selects the default.
func New(threshold int, parker Parker) *Guard {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	return &Guard{
		threshold: threshold,
		parker:    parker,
		fails:     make(map[int64]int),
		tripped:   make(map[int64]bool),
	}
}

// Record folds one request outcome into an account's auth-failure state.
// A 401/403 increments the streak (and parks the account once it crosses
// the threshold); any 2xx clears it. OAuth accounts are ignored (the
// TokenManager owns their auth lifecycle).
func (g *Guard) Record(ctx context.Context, account *provider.Account, status int) {
	if g == nil || account == nil || account.UsesUpstreamOAuth() {
		return
	}
	switch {
	case status == 401 || status == 403:
		g.mu.Lock()
		g.fails[account.ID]++
		n := g.fails[account.ID]
		trip := n >= g.threshold && !g.tripped[account.ID]
		if trip {
			g.tripped[account.ID] = true
		}
		g.mu.Unlock()
		if trip {
			reason := fmt.Sprintf("parked after %d consecutive auth failures (HTTP %d)", n, status)
			if err := g.parker.Park(ctx, account.ID, reason); err != nil {
				slog.Error("authguard: park failed", "account", account.Name, "id", account.ID, "err", err.Error())
				// Undo the trip flag so a later failure retries the park.
				g.mu.Lock()
				delete(g.tripped, account.ID)
				g.mu.Unlock()
				return
			}
			slog.Warn("account parked: persistent auth failure",
				"account", account.Name, "id", account.ID, "consecutive", n, "status", status)
		}
	case status >= 200 && status < 300:
		g.mu.Lock()
		delete(g.fails, account.ID)
		g.mu.Unlock()
	}
}

// StartRecovery launches the background sweeper that re-probes parked
// accounts and restores those that pass. list typically is
// accountPool.All; tester probes one account.
func (g *Guard) StartRecovery(ctx context.Context, list func() []*provider.Account, tester Tester, interval time.Duration) {
	if g == nil || tester == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultRecoveryInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.sweep(ctx, list(), tester)
			}
		}
	}()
}

func (g *Guard) sweep(ctx context.Context, accounts []*provider.Account, tester Tester) {
	g.mu.Lock()
	ids := make([]int64, 0, len(g.tripped))
	for id := range g.tripped {
		ids = append(ids, id)
	}
	g.mu.Unlock()
	if len(ids) == 0 {
		return
	}

	byID := make(map[int64]*provider.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}

	for _, id := range ids {
		a := byID[id]
		if a == nil {
			// Account left the pool (deleted/disabled) — forget it.
			g.clear(id)
			continue
		}
		if tester.Test(ctx, a) {
			if err := g.parker.Restore(ctx, id); err != nil {
				slog.Error("authguard: restore failed", "account", a.Name, "id", id, "err", err.Error())
				continue
			}
			g.clear(id)
			slog.Info("account auth recovered; restored to rotation", "account", a.Name, "id", id)
		}
	}
}

func (g *Guard) clear(id int64) {
	g.mu.Lock()
	delete(g.tripped, id)
	delete(g.fails, id)
	g.mu.Unlock()
}
