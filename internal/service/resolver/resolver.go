// Package resolver picks one upstream account (and its driver) per
// request from the in-memory pool.
//
// The MVP resolver implements priority-tiered weighted-random
// selection:
//
//  1. Filter to accounts whose provider is in the allowed set for
//     this request kind (currently: any provider that accepts the
//     Anthropic Messages format).
//  2. Bucket by priority. Lower numeric priority = preferred tier.
//  3. Inside the top bucket, do a weighted random pick.
//
// Health-aware filtering (skip cooled-down accounts), session
// stickiness, and failover live in a follow-up commit — those need
// the per-account health tracker that is not yet built.
package resolver

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"

	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// ErrNoUpstream is returned when no enabled account satisfies the
// request constraints.
var ErrNoUpstream = errors.New("no upstream account available")

// Resolver maps a request kind to a concrete (driver, account) pair.
// The interface keeps the handler decoupled from selection strategy:
// today it's WeightedResolver; tomorrow it could be cost-aware,
// latency-aware, or sticky.
type Resolver interface {
	// ResolveForProviders picks an account whose provider is in
	// allowedProviders. The empty list means "any provider".
	ResolveForProviders(allowedProviders []string) (*provider.Account, provider.Driver, error)
}

// WeightedResolver implements Resolver with priority + weighted-random
// selection.
type WeightedResolver struct {
	pool     *account.Pool
	registry *provider.Registry

	// mu guards rng, the only mutable state.
	mu  sync.Mutex
	rng *rand.Rand
}

// New returns a resolver wired against the given pool and driver
// registry. The registry is consulted to translate a chosen
// account.Provider string into the Driver implementation.
func New(pool *account.Pool, registry *provider.Registry) *WeightedResolver {
	// Use a per-resolver rng so tests can substitute a deterministic
	// source if needed in the future.
	src := rand.NewPCG(rand.Uint64(), rand.Uint64())
	return &WeightedResolver{
		pool:     pool,
		registry: registry,
		rng:      rand.New(src),
	}
}

// ResolveForProviders selects one account whose provider is in the
// allowed list. An empty list accepts any provider.
func (r *WeightedResolver) ResolveForProviders(
	allowedProviders []string,
) (*provider.Account, provider.Driver, error) {
	candidates := r.gather(allowedProviders)
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

// gather returns the subset of pool accounts whose provider is in
// allowed. An empty / nil allow-list passes every account through.
func (r *WeightedResolver) gather(allowed []string) []*provider.Account {
	if len(allowed) == 0 {
		return r.pool.All()
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, p := range allowed {
		allow[p] = struct{}{}
	}
	var out []*provider.Account
	for _, p := range allowed {
		for _, a := range r.pool.ByProvider(p) {
			if _, ok := allow[a.Provider]; ok {
				out = append(out, a)
			}
		}
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
		// All weights non-positive: degenerate to uniform.
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
	// Mathematically unreachable, but the compiler insists.
	return accounts[len(accounts)-1]
}
