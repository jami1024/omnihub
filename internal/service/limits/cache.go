package limits

import (
	"context"
	"sync"
	"time"
)

// SpendSource returns authoritative rolling-24h USD spend for one
// virtual key. Production wiring uses repository.MessageRequestRepo;
// the interface keeps the cache unit-testable without a DB.
type SpendSource interface {
	SumCostByKey(ctx context.Context, keyName string) (float64, error)
}

// SpendCache memoises per-key rolling-24h USD spend. Two refresh paths
// keep entries in sync with reality:
//
//   - Spend() lazily reloads from the SpendSource when the cached entry
//     is older than TTL (or absent). This is the authoritative refresh.
//   - Add() folds the current request's cost into the cached entry the
//     instant it's known, so back-to-back requests against the same
//     key see up-to-date totals without waiting for TTL or the
//     WriteBuffer flush.
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

// Spend returns the rolling-24h USD spend for keyName, reloading from
// the source when the cached entry is stale. Concurrent callers for
// the same cold key may both query the source; that wasted query is
// bounded to one extra round-trip and is preferred over singleflight
// complexity at this scale.
func (c *SpendCache) Spend(ctx context.Context, keyName string) (float64, error) {
	c.mu.Lock()
	e, ok := c.entries[keyName]
	fresh := ok && time.Since(e.refreshedAt) < c.ttl
	c.mu.Unlock()

	if fresh {
		return e.usd, nil
	}

	usd, err := c.src.SumCostByKey(ctx, keyName)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.entries[keyName] = &spendEntry{usd: usd, refreshedAt: time.Now()}
	c.mu.Unlock()
	return usd, nil
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
