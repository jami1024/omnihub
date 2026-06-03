// Package blockedip owns the per-IP policy table that fronts the
// gateway. Each row encodes either a hard block (all limit columns
// NULL → 403) or a set of soft caps (rpm / tpm / concurrent → 429
// when exceeded). Operators write the blocked_ips table; the
// LISTEN/NOTIFY listener refreshes this pool within a network
// round-trip and the guard middleware enforces the policy before
// any other work runs.
package blockedip

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Policy describes one row of the blocked_ips table in memory.
//
//   - When all three limit fields are zero, the IP is hard-blocked.
//   - When any limit is non-zero, the IP is allowed but capped.
//
// Limits are stored as int (not *int) because the zero value
// already encodes "no cap" cleanly and avoids a nil-deref in the
// hot path.
type Policy struct {
	IP              string
	Reason          string
	RPMLimit        int   // 0 = unlimited
	TPMLimit        int64 // 0 = unlimited; counts input + cache_creation tokens
	ConcurrentLimit int   // 0 = unlimited
}

// Blocked reports whether this policy is a hard block.
func (p *Policy) Blocked() bool {
	return p != nil && p.RPMLimit == 0 && p.TPMLimit == 0 && p.ConcurrentLimit == 0
}

// Source is the read-only port the pool needs. Mirrors the
// account/apikey Source pattern so tests can swap a stub in
// without a real Postgres dependency.
type Source interface {
	ListAll(ctx context.Context) ([]Policy, error)
}

// Pool is the in-memory blocklist + soft-cap index, keyed by IP
// string for O(1) lookups on the request hot path.
type Pool struct {
	source Source

	mu         sync.RWMutex
	policies   map[string]*Policy
	concurrent map[string]int // current in-flight count per IP

	tpm *TPMCache // per-IP fresh-input token buckets
}

// NewPool returns an empty pool. Call Refresh once or Start to
// kick off the periodic refresher.
func NewPool(source Source) *Pool {
	return &Pool{
		source:     source,
		policies:   make(map[string]*Policy),
		concurrent: make(map[string]int),
		tpm:        NewTPMCache(),
	}
}

// Refresh re-reads the source and atomically swaps the index. A
// failure leaves the previous view in place so a transient DB blip
// cannot lift the policies.
func (p *Pool) Refresh(ctx context.Context) error {
	policies, err := p.source.ListAll(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]*Policy, len(policies))
	for _, e := range policies {
		policy := e
		next[e.IP] = &policy
	}
	p.mu.Lock()
	p.policies = next
	p.mu.Unlock()
	return nil
}

// Start runs Refresh once synchronously then re-refreshes on the
// given interval until ctx is cancelled. The initial error is
// returned; subsequent failures are logged only.
func (p *Pool) Start(ctx context.Context, interval time.Duration) error {
	if err := p.Refresh(ctx); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := p.Refresh(ctx); err != nil {
					slog.Error("blocked_ips pool refresh failed", "err", err.Error())
				}
			}
		}
	}()
	return nil
}

// Lookup returns the policy for ip, or nil when the IP has no row.
func (p *Pool) Lookup(ip string) *Policy {
	if ip == "" {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.policies[ip]
}

// TryAcquireConcurrency atomically reserves one in-flight slot for
// ip when the policy's concurrent_limit allows it. Returns true on
// success; the caller must call ReleaseConcurrency once the request
// finishes. A nil policy or zero limit short-circuits to allow.
func (p *Pool) TryAcquireConcurrency(ip string, max int) bool {
	if ip == "" || max <= 0 {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cur := p.concurrent[ip]
	if cur >= max {
		return false
	}
	p.concurrent[ip] = cur + 1
	return true
}

// ReleaseConcurrency decrements the in-flight slot count. Always
// safe to call (clamped at zero) so deferred releases stay simple.
func (p *Pool) ReleaseConcurrency(ip string) {
	if ip == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if v := p.concurrent[ip]; v > 0 {
		p.concurrent[ip] = v - 1
	}
}

// TPMBucket exposes the per-IP TPM bucket so the middleware /
// handler can peek (entry-time check) and charge (after-response).
func (p *Pool) TPMBucket() *TPMCache { return p.tpm }

// Size returns the current policy count.
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.policies)
}
