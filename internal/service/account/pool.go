// Package account owns the in-memory view of upstream accounts. The
// authoritative source is the database (see internal/repository); the
// pool refreshes from there at a fixed interval so request-hot-path
// account lookups stay lock-light and never touch SQL.
//
// Refresh strategy is intentionally simple: a background goroutine
// re-queries every RefreshInterval. A future commit will swap this
// for PostgreSQL LISTEN/NOTIFY so account edits propagate to running
// instances within milliseconds instead of seconds.
package account

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// Source returns the current set of enabled accounts. Implemented by
// repository.AccountRepo; defined here so the Pool can be tested
// without a Postgres dependency.
type Source interface {
	ListEnabled(ctx context.Context) ([]*provider.Account, error)
}

// Pool is the in-memory cache of enabled upstream accounts, grouped
// by provider name for O(1) lookup on the request hot path.
type Pool struct {
	source Source

	mu         sync.RWMutex
	byProvider map[string][]*provider.Account
	all        []*provider.Account

	// lastRefresh records the most recent successful refresh. Reading
	// it does not need the main mutex thanks to atomic.
	lastRefresh atomic.Int64 // unix nanos
}

// NewPool returns an empty pool. Call Refresh once before serving
// traffic; Start spawns a background refresher.
func NewPool(source Source) *Pool {
	return &Pool{
		source:     source,
		byProvider: make(map[string][]*provider.Account),
	}
}

// Refresh re-queries the source and atomically swaps the in-memory
// view. A refresh failure leaves the previous view in place so a
// transient DB blip cannot drain the pool.
func (p *Pool) Refresh(ctx context.Context) error {
	accounts, err := p.source.ListEnabled(ctx)
	if err != nil {
		return err
	}

	grouped := make(map[string][]*provider.Account, 4)
	for _, a := range accounts {
		grouped[a.Provider] = append(grouped[a.Provider], a)
	}

	p.mu.Lock()
	p.byProvider = grouped
	p.all = accounts
	p.mu.Unlock()

	p.lastRefresh.Store(time.Now().UnixNano())
	return nil
}

// Start runs Refresh once synchronously, then spawns a goroutine that
// re-refreshes every interval until ctx is cancelled. Returns the
// error from the initial refresh; subsequent failures are logged but
// do not abort the loop.
func (p *Pool) Start(ctx context.Context, interval time.Duration) error {
	if err := p.Refresh(ctx); err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := p.Refresh(ctx); err != nil {
					slog.Error("account pool refresh failed", "err", err.Error())
				}
			}
		}
	}()
	return nil
}

// ByProvider returns the enabled accounts for one provider. The
// returned slice must NOT be mutated by callers; the resolver treats
// it as read-only.
func (p *Pool) ByProvider(name string) []*provider.Account {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byProvider[name]
}

// All returns every enabled account across providers. Useful for the
// resolver when the request can be served by any compatible provider
// (e.g. Anthropic Messages requests accept both "anthropic" and
// "claude-platform" accounts).
func (p *Pool) All() []*provider.Account {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// Return a snapshot so the caller can sort / shuffle without
	// racing the refresher.
	out := make([]*provider.Account, len(p.all))
	copy(out, p.all)
	return out
}

// Size returns the number of enabled accounts in the pool.
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.all)
}

// LastRefresh returns the time of the most recent successful refresh.
func (p *Pool) LastRefresh() time.Time {
	ns := p.lastRefresh.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}
