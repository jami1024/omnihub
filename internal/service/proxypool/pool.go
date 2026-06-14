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

// healthFailThreshold is how many CONSECUTIVE failed probes mark a
// proxy unhealthy (de-bounce so one transient blip doesn't degrade it).
// One success restores it.
const healthFailThreshold = 3

// proxyHealth is the prober's per-proxy runtime state. An absent entry
// means "not yet probed" and is treated as healthy (optimistic), so the
// resolver never degrades a proxy the prober hasn't evaluated.
type proxyHealth struct {
	consecFails int
	healthy     bool
	latencyMs   int64
	checkedUnix int64 // unix seconds; 0 = never
}

// Source returns every proxy. Implemented by repository.ProxyRepo.
type Source interface {
	ListAll(ctx context.Context) ([]*provider.Proxy, error)
}

// Pool is the in-memory cache of proxies, keyed by id for O(1) lookup
// during proxy resolution.
type Pool struct {
	source Source
	now    func() time.Time

	mu     sync.RWMutex
	byID   map[int64]*provider.Proxy
	health map[int64]*proxyHealth
}

// NewPool returns an empty pool. Call Refresh / Start before serving.
func NewPool(source Source) *Pool {
	return &Pool{
		source: source,
		now:    time.Now,
		byID:   make(map[int64]*provider.Proxy),
		health: make(map[int64]*proxyHealth),
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
	// GC health entries for proxies that left the pool.
	for id := range p.health {
		if _, ok := byID[id]; !ok {
			delete(p.health, id)
		}
	}
	p.mu.Unlock()
	return nil
}

// AllActive returns a snapshot of the active proxies, for the prober.
func (p *Pool) AllActive() []*provider.Proxy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*provider.Proxy, 0, len(p.byID))
	for _, pr := range p.byID {
		if pr.Active() {
			out = append(out, pr)
		}
	}
	return out
}

// RecordProbe folds one probe outcome into a proxy's health. A failure
// increments the consecutive-fail counter and marks the proxy unhealthy
// once it crosses the threshold; a success restores it immediately.
func (p *Pool) RecordProbe(id int64, ok bool, latencyMs int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := p.health[id]
	if h == nil {
		h = &proxyHealth{healthy: true}
		p.health[id] = h
	}
	h.checkedUnix = p.now().Unix()
	h.latencyMs = latencyMs
	if ok {
		h.consecFails = 0
		h.healthy = true
		return
	}
	h.consecFails++
	if h.consecFails >= healthFailThreshold {
		h.healthy = false
	}
}

// ProxyHealth returns the live health of a proxy as primitives (for the
// admin layer, which must not import this package's types). ok is false
// when the proxy has never been probed. checkedUnix is unix seconds.
func (p *Pool) ProxyHealth(id int64) (healthy bool, latencyMs int64, checkedUnix int64, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h := p.health[id]
	if h == nil || h.checkedUnix == 0 {
		return false, 0, 0, false
	}
	return h.healthy, h.latencyMs, h.checkedUnix, true
}

// healthyLocked reports whether a proxy is currently routable health-
// wise. An un-probed proxy (no entry) is optimistically healthy.
func (p *Pool) healthyLocked(id int64) bool {
	h := p.health[id]
	return h == nil || h.healthy
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
//   - a healthy, unexpired active proxy → its URL;
//   - an expired but healthy proxy → per fallback_mode: "none" keeps
//     using it (expiry is advisory), "direct" routes direct, "proxy"
//     follows the backup chain to the first usable proxy (cycle-safe);
//   - an UNHEALTHY proxy (the prober's probes are failing) → always
//     degrade: follow the backup chain when configured, else direct —
//     "none" is ignored because there is no point keeping a dead proxy.
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
	healthy := p.healthyLocked(id)
	fresh := !pr.IsExpired(now)
	if healthy && fresh {
		return pr.URL()
	}
	if healthy && !fresh {
		// Expired but reachable: fallback_mode decides.
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
	// Unhealthy: degrade regardless of fallback_mode — try the backup
	// chain, else direct. Keeping a dead proxy ("none") is pointless.
	if pr.FallbackMode == "proxy" && pr.BackupProxyID != nil {
		return p.resolveLocked(*pr.BackupProxyID, now, visited)
	}
	return ""
}
