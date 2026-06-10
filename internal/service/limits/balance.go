package limits

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

// BalanceSource returns the authoritative spendable balance for a portal
// user in a given billing mode (payg → wallet; plan → grant credit, plus
// wallet when overage is allowed). Production wiring is
// billing.Service.AvailableBalance; the interface keeps the guard
// unit-testable without a DB.
type BalanceSource interface {
	Balance(ctx context.Context, userID int64, mode apikey.BillingMode) (float64, error)
}

// BalanceFunc adapts a plain function to BalanceSource.
type BalanceFunc func(ctx context.Context, userID int64, mode apikey.BillingMode) (float64, error)

// Balance implements BalanceSource.
func (f BalanceFunc) Balance(ctx context.Context, userID int64, mode apikey.BillingMode) (float64, error) {
	return f(ctx, userID, mode)
}

// balanceKey scopes a cached balance to (user, billing mode). The same user
// holding both a payg key and a plan key needs two different available
// balances — payg sees the wallet, plan sees grant credit (+ wallet overage)
// — which a single per-user entry cannot represent.
type balanceKey struct {
	userID int64
	mode   apikey.BillingMode
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
	entries map[balanceKey]*balanceEntry
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
		entries: make(map[balanceKey]*balanceEntry),
		now:     time.Now,
	}
}

// Balance returns the spendable balance for (userID, mode), serving a known
// entry from memory (refreshing stale entries in the background) and blocking
// only on a cold entry to seed the cache.
func (g *BalanceGuard) Balance(ctx context.Context, userID int64, mode apikey.BillingMode) (float64, error) {
	key := balanceKey{userID: userID, mode: mode}
	g.mu.Lock()
	if e, ok := g.entries[key]; ok {
		usd := e.usd
		if g.now().Sub(e.refreshedAt) >= g.ttl && !e.refreshing {
			e.refreshing = true
			g.mu.Unlock()
			go g.refresh(key)
			return usd, nil
		}
		g.mu.Unlock()
		return usd, nil
	}
	g.mu.Unlock()

	usd, err := g.src.Balance(ctx, userID, mode)
	if err != nil {
		return 0, err
	}
	g.mu.Lock()
	if _, exists := g.entries[key]; !exists {
		g.entries[key] = &balanceEntry{usd: usd, refreshedAt: g.now()}
	}
	usd = g.entries[key].usd
	g.mu.Unlock()
	return usd, nil
}

// refresh reloads one user's balance in the background. On error the
// entry is left intact (serving stale) and re-armed for a later retry.
//
// Known, bounded imprecision (matches SpendCache): the fresh DB value is
// an absolute assignment, so a Charge that lands during the read window
// — before its message_requests row is flushed — can be overwritten and
// briefly under-counted. It self-corrects on the next refresh, so the
// error is at most one refresh window (TTL) of one user's spend. Together
// with the bal<=0 gate's TOCTOU (concurrent requests may each pass before
// any Charge lands), this allows small, bounded overdraft — an accepted
// trade-off for keeping the hot path lock-free and DB-free.
func (g *BalanceGuard) refresh(key balanceKey) {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	usd, err := g.src.Balance(ctx, key.userID, key.mode)

	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.entries[key]
	if !ok {
		return
	}
	if err != nil {
		e.refreshing = false
		slog.Warn("balance guard background refresh failed; serving stale",
			"user", key.userID, "mode", key.mode, "err", err.Error())
		return
	}
	e.usd = usd
	e.refreshedAt = g.now()
	e.refreshing = false
}

// allModes is the fixed set of billing modes a user can hold simultaneously.
var allModes = [...]apikey.BillingMode{apikey.ModePayg, apikey.ModePlan}

// ChargeWallet debits a wallet-paid amount. The wallet is shared across both
// billing modes, so it lowers BOTH the (user,payg) and (user,plan) cached
// entries — a payg-key spend reduces a plan key's overage headroom and vice
// versa. A no-op for entries not yet cached (seeded fresh from the DB later).
func (g *BalanceGuard) ChargeWallet(userID int64, usd float64) {
	if g == nil || usd <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, mode := range allModes {
		if e, ok := g.entries[balanceKey{userID, mode}]; ok {
			e.usd -= usd
		}
	}
}

// ChargePlan debits a plan-credit-paid amount. Plan credit is visible only to
// plan keys, so it lowers ONLY the (user,plan) entry; the payg entry (wallet)
// is untouched.
func (g *BalanceGuard) ChargePlan(userID int64, usd float64) {
	if g == nil || usd <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.entries[balanceKey{userID, apikey.ModePlan}]; ok {
		e.usd -= usd
	}
}

// Credit folds a just-applied wallet top-up into the cached balance. The
// wallet feeds both modes, so it bumps BOTH entries.
func (g *BalanceGuard) Credit(userID int64, usd float64) {
	if g == nil || usd == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, mode := range allModes {
		if e, ok := g.entries[balanceKey{userID, mode}]; ok {
			e.usd += usd
		}
	}
}
