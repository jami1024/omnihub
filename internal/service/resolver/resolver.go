// Package resolver picks one upstream account (and its driver) per
// request from the in-memory pool.
//
// The MVP resolver implements priority-tiered weighted-random
// selection with health-aware filtering and optional session
// stickiness:
//
//  1. If a session key is present and the previous binding is still
//     routable, return that account (cache-hit-friendly).
//  2. Filter to accounts whose provider is in the allowed set for
//     this request kind.
//  3. Drop accounts in the excluded set (already tried this request)
//     and accounts whose circuit breaker is open.
//  4. Bucket by priority. Lower numeric priority = preferred tier.
//  5. Inside the top bucket, do a weighted random pick.
//  6. Bind the chosen account to the session key for future turns.
package resolver

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/session"
)

// ErrNoUpstream is returned when no enabled account satisfies the
// request constraints.
var ErrNoUpstream = errors.New("no upstream account available")

// Resolver maps a request kind to a concrete (driver, account) pair.
type Resolver interface {
	// ResolveForProviders picks an account.
	//
	//   - sessionKey "" disables stickiness for this call.
	//   - allowedProviders empty means "any provider".
	//   - excludedAccountIDs nil means "no exclusions". A retry loop
	//     fills it with account IDs already attempted on the same
	//     request.
	ResolveForProviders(
		sessionKey string,
		allowedProviders []string,
		excludedAccountIDs []int64,
	) (*provider.Account, provider.Driver, error)
}

// SpendFilter reports whether an account has exhausted a configured
// per-account USD spend cap and should be skipped during selection. It
// is an in-memory check (no I/O on the hot path), mirroring the health
// tracker. Implemented by limits.AccountGuard.
type SpendFilter interface {
	OverLimit(a *provider.Account) bool
}

// ConcurrencyFilter reports whether an account has no free in-flight
// slot (max_concurrency reached) and should be skipped during
// selection. Implemented by limits.ConcurrencyGuard.
type ConcurrencyFilter interface {
	AtCap(a *provider.Account) bool
}

// WeightedResolver implements Resolver with priority + weighted-random
// selection, health-aware filtering, and session stickiness.
type WeightedResolver struct {
	pool     *account.Pool
	registry *provider.Registry
	tracker  *health.Tracker
	sessions *session.Store
	spend    SpendFilter
	conc     ConcurrencyFilter

	mu  sync.Mutex
	rng *rand.Rand
	// rrCounters drives round_robin groups: a monotonically increasing
	// cursor per group id, indexing into the (ID-sorted) top bucket.
	rrCounters map[int64]uint64
}

// SetSpendFilter installs an optional per-account spend-cap filter. Nil
// (the default) disables spend filtering. Returns the resolver for
// chaining.
func (r *WeightedResolver) SetSpendFilter(f SpendFilter) *WeightedResolver {
	r.spend = f
	return r
}

// SetConcurrencyFilter installs an optional per-account concurrency
// filter. Nil (the default) disables it. Returns the resolver for
// chaining.
func (r *WeightedResolver) SetConcurrencyFilter(f ConcurrencyFilter) *WeightedResolver {
	r.conc = f
	return r
}

// New returns a resolver wired against the given dependencies.
// Both tracker and sessions are optional; nil disables that feature.
func New(
	pool *account.Pool,
	registry *provider.Registry,
	tracker *health.Tracker,
	sessions *session.Store,
) *WeightedResolver {
	src := rand.NewPCG(rand.Uint64(), rand.Uint64())
	return &WeightedResolver{
		pool:       pool,
		registry:   registry,
		tracker:    tracker,
		sessions:   sessions,
		rng:        rand.New(src),
		rrCounters: make(map[int64]uint64),
	}
}

// ResolveForProviders selects one account whose provider is in the
// allowed list, skipping any in excluded and any with an open
// circuit breaker. When sessionKey is non-empty and a binding exists,
// the bound account is returned if still routable. On a fresh
// selection the result is bound to sessionKey.
func (r *WeightedResolver) ResolveForProviders(
	sessionKey string,
	allowedProviders []string,
	excludedAccountIDs []int64,
) (*provider.Account, provider.Driver, error) {
	// 1) Sticky binding takes priority over fresh selection.
	if sticky := r.resolveSticky(sessionKey, allowedProviders, excludedAccountIDs); sticky != nil {
		drv, ok := r.registry.Get(sticky.Provider)
		if ok {
			return sticky, drv, nil
		}
		// Bound to a provider whose driver vanished mid-flight.
		// Drop the binding so future attempts re-select.
		if r.sessions != nil {
			r.sessions.Drop(sessionKey)
		}
	}

	candidates := r.gather(allowedProviders, excludedAccountIDs)
	if len(candidates) == 0 {
		return nil, nil, ErrNoUpstream
	}

	// 2) Priority bucketing: lowest Priority is the preferred tier.
	topPriority := candidates[0].Priority
	for _, a := range candidates[1:] {
		if a.Priority < topPriority {
			topPriority = a.Priority
		}
	}
	var top []*provider.Account
	for _, a := range candidates {
		if a.Priority == topPriority {
			top = append(top, a)
		}
	}

	// 3) Weighted random inside the top bucket.
	chosen := r.pick(top)
	driver, ok := r.registry.Get(chosen.Provider)
	if !ok {
		return nil, nil, fmt.Errorf("resolver: account %q references unregistered driver %q",
			chosen.Name, chosen.Provider)
	}

	// 4) Bind for future turns of this session.
	if sessionKey != "" && r.sessions != nil && len(excludedAccountIDs) == 0 {
		// Only bind on a "clean" first attempt — bindings made
		// during a retry loop would freeze the session onto a
		// fallback that may not really be the best fit.
		r.sessions.Bind(sessionKey, chosen.ID)
	}

	return chosen, driver, nil
}

// resolveSticky returns the previously bound account if still
// routable for this request, otherwise nil. The check honours
// allowedProviders, excludedAccountIDs, and the health tracker —
// stickiness must never resurrect a sick or wrong-provider account.
func (r *WeightedResolver) resolveSticky(
	sessionKey string,
	allowed []string,
	excluded []int64,
) *provider.Account {
	if sessionKey == "" || r.sessions == nil {
		return nil
	}
	accountID, ok := r.sessions.Get(sessionKey)
	if !ok {
		return nil
	}
	// Excluded means "we already tried this in the same request".
	for _, e := range excluded {
		if e == accountID {
			return nil
		}
	}
	if r.tracker != nil && !r.tracker.IsAvailable(accountID) {
		return nil
	}
	// Pool lookup. Iterating is fine: pools are small (< 100 in
	// realistic deployments). A map index can land later if pools grow.
	for _, a := range r.pool.All() {
		if a.ID != accountID {
			continue
		}
		if r.spend != nil && r.spend.OverLimit(a) {
			return nil
		}
		if !a.IsActiveAt(time.Now()) {
			return nil
		}
		if !a.AuthRoutable() {
			return nil
		}
		if r.conc != nil && r.conc.AtCap(a) {
			return nil
		}
		if len(allowed) == 0 {
			return a
		}
		for _, p := range allowed {
			if a.Provider == p {
				return a
			}
		}
		return nil
	}
	return nil
}

// gather returns the subset of pool accounts that:
//   - have a provider in allowed (empty allow-list passes everything);
//   - are not in the excluded set;
//   - pass the health tracker's IsAvailable check.
func (r *WeightedResolver) gather(allowed []string, excluded []int64) []*provider.Account {
	now := time.Now()
	excludedSet := make(map[int64]struct{}, len(excluded))
	for _, id := range excluded {
		excludedSet[id] = struct{}{}
	}

	var raw []*provider.Account
	if len(allowed) == 0 {
		raw = r.pool.All()
	} else {
		for _, p := range allowed {
			raw = append(raw, r.pool.ByProvider(p)...)
		}
	}

	out := raw[:0]
	for _, a := range raw {
		if _, skip := excludedSet[a.ID]; skip {
			continue
		}
		if r.tracker != nil && !r.tracker.IsAvailable(a.ID) {
			continue
		}
		if r.spend != nil && r.spend.OverLimit(a) {
			continue
		}
		if !a.IsActiveAt(now) {
			continue
		}
		// Auth lifecycle gate: refresh_failed / login_required /
		// quota_exhausted / ... accounts are parked until the
		// TokenManager or an admin recovers them.
		if !a.AuthRoutable() {
			continue
		}
		// Saturated accounts (max_concurrency reached) are skipped so
		// failover attempts are not wasted on them.
		if r.conc != nil && r.conc.AtCap(a) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// pick selects one account from the top priority bucket according to
// the bucket's routing policy. round_robin applies only when EVERY
// candidate belongs to the same group and that group asks for it;
// any mixed or ungrouped bucket falls back to the historical
// weighted-random behaviour (predictable and safe under composition).
func (r *WeightedResolver) pick(accounts []*provider.Account) *provider.Account {
	if gid, ok := roundRobinGroup(accounts); ok {
		return r.roundRobinPick(gid, accounts)
	}
	return r.weightedPick(accounts)
}

// roundRobinGroup reports the single group id shared by every account
// when that group's routing policy is round_robin.
func roundRobinGroup(accounts []*provider.Account) (int64, bool) {
	if len(accounts) == 0 {
		return 0, false
	}
	first := accounts[0]
	if first.GroupID == nil || first.GroupRoutingPolicy != "round_robin" {
		return 0, false
	}
	gid := *first.GroupID
	for _, a := range accounts[1:] {
		if a.GroupID == nil || *a.GroupID != gid {
			return 0, false
		}
	}
	return gid, true
}

// roundRobinPick cycles through the bucket in stable (account-ID)
// order using a per-group cursor. The candidate set may shrink and
// grow between calls (health/cooldown filtering); indexing the sorted
// remainder keeps the rotation fair regardless.
func (r *WeightedResolver) roundRobinPick(groupID int64, accounts []*provider.Account) *provider.Account {
	sorted := make([]*provider.Account, len(accounts))
	copy(sorted, accounts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	r.mu.Lock()
	cursor := r.rrCounters[groupID]
	r.rrCounters[groupID] = cursor + 1
	r.mu.Unlock()

	return sorted[cursor%uint64(len(sorted))]
}

// weightedPick chooses one account proportional to Weight. A weight
// of zero is treated as 1 so freshly inserted rows can still be
// selected (just rarely).
func (r *WeightedResolver) weightedPick(accounts []*provider.Account) *provider.Account {
	if len(accounts) == 1 {
		return accounts[0]
	}

	totalWeight := 0
	for _, a := range accounts {
		w := a.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}
	if totalWeight <= 0 {
		r.mu.Lock()
		idx := r.rng.IntN(len(accounts))
		r.mu.Unlock()
		return accounts[idx]
	}

	r.mu.Lock()
	pick := r.rng.IntN(totalWeight)
	r.mu.Unlock()

	for _, a := range accounts {
		w := a.Weight
		if w <= 0 {
			w = 1
		}
		if pick < w {
			return a
		}
		pick -= w
	}
	return accounts[len(accounts)-1]
}
