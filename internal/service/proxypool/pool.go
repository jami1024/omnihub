// Package proxypool owns the in-memory view of egress proxies and
// resolves an account's proxy binding into a dial URL for the
// forwarder, applying expiry fallback. The authoritative source is the
// proxies table (migration 0038); the pool refreshes from there on a
// timer and via the omnihub_proxies_changed NOTIFY channel.
package proxypool

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// NotifyChannel is the PostgreSQL NOTIFY channel the proxies trigger
// publishes to (migration 0038).
const NotifyChannel = "omnihub_proxies_changed"

// Source returns every proxy. Implemented by repository.ProxyRepo.
type Source interface {
	ListAll(ctx context.Context) ([]*provider.Proxy, error)
}

// Pool is the in-memory cache of proxies, keyed by id for O(1) lookup
// during proxy resolution.
type Pool struct {
	source Source
	now    func() time.Time

	mu   sync.RWMutex
	byID map[int64]*provider.Proxy
}

// NewPool returns an empty pool. Call Refresh / Start before serving.
func NewPool(source Source) *Pool {
	return &Pool{
		source: source,
		now:    time.Now,
		byID:   make(map[int64]*provider.Proxy),
	}
}

// Refresh re-queries the source and swaps the in-memory view. A failure
// leaves the previous view in place.
func (p *Pool) Refresh(ctx context.Context) error {
	proxies, err := p.source.ListAll(ctx)
	if err != nil {
		return err
	}
	byID := make(map[int64]*provider.Proxy, len(proxies))
	for _, pr := range proxies {
		byID[pr.ID] = pr
	}
	p.mu.Lock()
	p.byID = byID
	p.mu.Unlock()
	return nil
}

// Start runs Refresh once, then re-refreshes every interval until ctx
// is cancelled (the timer is a safety net behind the NOTIFY listener).
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
					slog.Error("proxy pool refresh failed", "err", err.Error())
				}
			}
		}
	}()
	return nil
}

// Size returns the number of proxies in the pool.
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.byID)
}

// Resolve turns an account's proxy binding into a dial URL for the
// forwarder (implements forward.ProxyResolver):
//
//   - no binding (ProxyID nil) → the legacy inline ProxyURL, so
//     existing accounts keep working;
//   - a disabled or vanished proxy → "" (direct);
//   - an unexpired active proxy → its URL;
//   - an expired proxy → per fallback_mode: "none" keeps using it
//     (expiry is advisory), "direct" routes direct, "proxy" follows the
//     backup chain to the first usable proxy (cycle-safe), "" if none.
func (p *Pool) Resolve(account *provider.Account) string {
	if account == nil {
		return ""
	}
	if account.ProxyID == nil {
		return account.ProxyURL
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := p.now()
	return p.resolveLocked(*account.ProxyID, now, map[int64]struct{}{})
}

// resolveLocked walks the proxy (and its backup chain). Caller holds
// the read lock. visited guards against cycles in backup_proxy_id.
func (p *Pool) resolveLocked(id int64, now time.Time, visited map[int64]struct{}) string {
	if _, seen := visited[id]; seen {
		return "" // cycle — give up, route direct
	}
	visited[id] = struct{}{}

	pr := p.byID[id]
	if pr == nil || !pr.Active() {
		return "" // vanished or disabled → direct
	}
	if !pr.IsExpired(now) {
		return pr.URL()
	}
	switch pr.FallbackMode {
	case "direct":
		return ""
	case "proxy":
		if pr.BackupProxyID != nil {
			return p.resolveLocked(*pr.BackupProxyID, now, visited)
		}
		return ""
	default: // "none" — expiry is advisory, keep using it
		return pr.URL()
	}
}
