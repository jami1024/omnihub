package limits

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// refreshTimeout bounds a background spend refresh. The request that
// triggered the refresh has already been served the cached value, so
// this only stops the goroutine from leaking against a hung DB.
const refreshTimeout = 5 * time.Second

// SpendSource returns authoritative rolling-24h USD spend for one
// virtual key. Production wiring uses repository.MessageRequestRepo;
// the interface keeps the cache unit-testable without a DB.
type SpendSource interface {
	SumCostByKey(ctx context.Context, keyName string) (float64, error)
}

// SpendCache memoises per-key rolling-24h USD spend. Three paths keep
// entries in sync with reality:
//
//   - Spend() serves a known key from memory and NEVER blocks on the
//     source for it: a stale entry is returned immediately while a single
//     background goroutine reloads it (stale-while-revalidate). Only a
//     cold key — unseen since process start — blocks once to seed an
//     authoritative base.
//   - Add() folds the current request's cost into the cached entry the
//     instant it's known, so back-to-back requests against the same
//     key see up-to-date totals without waiting for TTL or the
//     WriteBuffer flush.
//
// Serving stale is safe for this guard: because Add() keeps a single
// gateway's own running total accurate between refreshes, the background
// reload only reconciles out-of-band changes (other instances, manual DB
// edits). Keeping the DB round-trip off the request path removes it from
// the first-token critical path — a stale entry never delays a response.
//
// Add() does nothing when the entry is absent: without an authoritative
// base from Spend(), a lone increment would mis-represent the running
// total. The first Spend() seeds the entry from the DB; subsequent
// Adds() accumulate on top.
type SpendCache struct {
	src SpendSource
	ttl time.Duration

	mu      sync.Mutex
	entries map[string]*spendEntry
}

type spendEntry struct {
	usd         float64
	refreshedAt time.Time
	refreshing  bool // a background reload is in flight; don't start another
}

// NewSpendCache returns a fresh cache. A typical TTL is in the
// single-digit-seconds range: long enough to keep hot keys cheap,
// short enough that an operator-side adjustment (e.g. deleting test
// rows) reflects quickly.
func NewSpendCache(src SpendSource, ttl time.Duration) *SpendCache {
	return &SpendCache{
		src:     src,
		ttl:     ttl,
		entries: make(map[string]*spendEntry),
	}
}

// Spend returns the rolling-24h USD spend for keyName. A known key is
// served from memory without blocking: when the entry is older than TTL
// it is returned as-is and a single background goroutine refreshes it.
// Only a cold key blocks, querying the source once to seed the entry
// (Add() cannot accumulate without an authoritative base).
func (c *SpendCache) Spend(ctx context.Context, keyName string) (float64, error) {
	c.mu.Lock()
	if e, ok := c.entries[keyName]; ok {
		usd := e.usd
		if time.Since(e.refreshedAt) >= c.ttl && !e.refreshing {
			e.refreshing = true
			c.mu.Unlock()
			go c.refresh(keyName)
			return usd, nil
		}
		c.mu.Unlock()
		return usd, nil
	}
	c.mu.Unlock()

	// Cold key: block once on the source to seed an authoritative base.
	usd, err := c.src.SumCostByKey(ctx, keyName)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	// Only seed if still absent — a concurrent cold Spend (or an Add that
	// raced in after the first seed) must not be clobbered.
	if _, exists := c.entries[keyName]; !exists {
		c.entries[keyName] = &spendEntry{usd: usd, refreshedAt: time.Now()}
	}
	usd = c.entries[keyName].usd
	c.mu.Unlock()
	return usd, nil
}

// refresh reloads keyName from the source in the background and writes
// the authoritative value back. It runs in its own goroutine with a
// bounded context independent of any request. On error the entry is left
// intact — we keep serving the last known value — and re-armed so a
// later Spend() can retry.
func (c *SpendCache) refresh(keyName string) {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	usd, err := c.src.SumCostByKey(ctx, keyName)

	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[keyName]
	if !ok {
		return
	}
	if err != nil {
		e.refreshing = false
		slog.Warn("spend cache background refresh failed; serving stale",
			"key", keyName, "err", err.Error())
		return
	}
	e.usd = usd
	e.refreshedAt = time.Now()
	e.refreshing = false
}

// Add increments the cached entry for keyName. A no-op when the entry
// is absent — see the type comment for why.
func (c *SpendCache) Add(keyName string, usd float64) {
	if usd <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[keyName]; ok {
		e.usd += usd
	}
}
