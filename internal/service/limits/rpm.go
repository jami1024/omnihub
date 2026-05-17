package limits

import (
	"sync"

	"golang.org/x/time/rate"
)

// RPMCache holds one token-bucket limiter per virtual key. A bucket
// refills at rpm/60 tokens per second with burst = rpm — i.e. a key
// with rpm=60 may fire 60 requests instantly and must then wait one
// second per subsequent request until the bucket replenishes.
//
// Buckets are keyed by the api_key name. When an operator updates a
// key's rpm_limit, NewListener / Pool.Refresh propagates the new
// value through *apikey.Key; the next Allow() observes the change
// and rebuilds the bucket from scratch so the new policy takes
// effect immediately (any goodwill left in the old bucket is
// discarded — that's the price of switching policies mid-stream).
//
// Memory note: entries are not GC'd on key deletion. Each entry is
// O(120 bytes); a deployment with thousands of historical keys
// still consumes well under one MB. If that ever stops being true,
// add a periodic sweep keyed off pool membership.
type RPMCache struct {
	mu       sync.Mutex
	limiters map[string]*rpmEntry
}

type rpmEntry struct {
	rpm     int
	limiter *rate.Limiter
}

// NewRPMCache returns an empty cache.
func NewRPMCache() *RPMCache {
	return &RPMCache{limiters: make(map[string]*rpmEntry)}
}

// Allow attempts to consume one token from keyName's bucket. Returns
// true when the request is permitted, false when the rate limit
// would be exceeded. A miss creates the bucket using the supplied rpm.
//
// rpm <= 0 is treated as "no limit" and Allow returns true without
// touching the cache.
func (c *RPMCache) Allow(keyName string, rpm int) bool {
	if rpm <= 0 {
		return true
	}
	c.mu.Lock()
	e, ok := c.limiters[keyName]
	if !ok || e.rpm != rpm {
		e = &rpmEntry{
			rpm:     rpm,
			limiter: rate.NewLimiter(rate.Limit(float64(rpm)/60.0), rpm),
		}
		c.limiters[keyName] = e
	}
	c.mu.Unlock()
	return e.limiter.Allow()
}
