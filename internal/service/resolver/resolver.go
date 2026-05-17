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
	"sync"

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

// WeightedResolver implements Resolver with priority + weighted-random
// selection, health-aware filtering, and session stickiness.
type WeightedResolver struct {
	pool     *account.Pool
	registry *provider.Registry
	tracker  *health.Tracker
	sessions *session.Store

	mu  sync.Mutex
	rng *rand.Rand
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
		pool:     pool,
		registry: registry,
		tracker:  tracker,
		sessions: sessions,
		rng:      rand.New(src),
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
	chosen := r.weightedPick(top)
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
		out = append(out, a)
	}
	return out
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
