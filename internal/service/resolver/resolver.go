// Package resolver picks one upstream account (and its driver) per
// request from the in-memory pool.
//
// The MVP resolver implements priority-tiered weighted-random
// selection with health-aware filtering:
//
//  1. Filter to accounts whose provider is in the allowed set for
//     this request kind (currently: any provider that accepts the
//     Anthropic Messages format).
//  2. Drop accounts in the excluded set (already tried this request)
//     and accounts whose circuit breaker is open.
//  3. Bucket by priority. Lower numeric priority = preferred tier.
//  4. Inside the top bucket, do a weighted random pick.
//
// Session stickiness lives in a follow-up commit and will compose with
// this resolver via a session-binding decorator.
package resolver

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"

	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// ErrNoUpstream is returned when no enabled account satisfies the
// request constraints.
var ErrNoUpstream = errors.New("no upstream account available")

// Resolver maps a request kind to a concrete (driver, account) pair.
type Resolver interface {
	// ResolveForProviders picks an account whose provider is in
	// allowedProviders. excludedAccountIDs lets a retry loop skip
	// accounts already attempted on the same request. Empty
	// allowedProviders means "any provider"; nil excludedAccountIDs
	// means "no exclusions".
	ResolveForProviders(
		allowedProviders []string,
		excludedAccountIDs []int64,
	) (*provider.Account, provider.Driver, error)
}

// WeightedResolver implements Resolver with priority + weighted-random
// selection and health-aware filtering.
type WeightedResolver struct {
	pool     *account.Pool
	registry *provider.Registry
	tracker  *health.Tracker

	mu  sync.Mutex
	rng *rand.Rand
}

// New returns a resolver wired against the given pool, driver
// registry, and health tracker. A nil tracker disables health-based
// filtering (every enabled account is considered routable).
func New(pool *account.Pool, registry *provider.Registry, tracker *health.Tracker) *WeightedResolver {
	src := rand.NewPCG(rand.Uint64(), rand.Uint64())
	return &WeightedResolver{
		pool:     pool,
		registry: registry,
		tracker:  tracker,
		rng:      rand.New(src),
	}
}

// ResolveForProviders selects one account whose provider is in the
// allowed list, skipping any in excluded and any with an open
// circuit breaker. Returns ErrNoUpstream when nothing routable remains.
func (r *WeightedResolver) ResolveForProviders(
	allowedProviders []string,
	excludedAccountIDs []int64,
) (*provider.Account, provider.Driver, error) {
	candidates := r.gather(allowedProviders, excludedAccountIDs)
	if len(candidates) == 0 {
		return nil, nil, ErrNoUpstream
	}

	// Top priority bucket = the lowest Priority value among candidates.
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

	chosen := r.weightedPick(top)
	driver, ok := r.registry.Get(chosen.Provider)
	if !ok {
		return nil, nil, fmt.Errorf("resolver: account %q references unregistered driver %q",
			chosen.Name, chosen.Provider)
	}
	return chosen, driver, nil
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
