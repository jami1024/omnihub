package blockedip

import (
	"sync"
	"time"
)

// TPMCache holds one rolling-60s token bucket per IP. Capacity is
// the configured TPM (fresh tokens / minute); the bucket refills at
// capacity / 60 tokens per second.
//
// The cache uses a "consume-after" model: requests are admitted when
// the bucket has any remaining budget (>0); Charge then deducts the
// actual fresh-token count from the just-completed response. Going
// negative is permitted — it means the next request will be rejected
// until the bucket has refilled past zero. This matches Anthropic's
// own behaviour (you can briefly burst, then the next request gets
// 429 until the rolling window clears).
type TPMCache struct {
	mu      sync.Mutex
	buckets map[string]*tpmBucket
}

type tpmBucket struct {
	capacity   float64
	refillRate float64 // tokens per second
	available  float64
	lastRefill time.Time
}

// NewTPMCache returns an empty cache.
func NewTPMCache() *TPMCache {
	return &TPMCache{buckets: make(map[string]*tpmBucket)}
}

// Allow reports whether ip has any remaining TPM budget against
// the given cap. tpm <= 0 disables the check.
func (c *TPMCache) Allow(ip string, tpm int64) bool {
	if tpm <= 0 || ip == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.getOrCreateLocked(ip, tpm)
	b.refill(time.Now())
	// Require ≥1 whole token of budget so the fractional refill in
	// the microseconds between Charge and the next Allow doesn't
	// admit a request a freshly-drained bucket couldn't really pay
	// for. Anthropic's own bucket has the same "no partial credit"
	// shape.
	return b.available >= 1.0
}

// Charge deducts n tokens from ip's bucket. Called after a request
// completes so the bucket reflects real upstream consumption.
// tpm <= 0 or n <= 0 is a no-op.
func (c *TPMCache) Charge(ip string, tpm int64, n int64) {
	if tpm <= 0 || n <= 0 || ip == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.getOrCreateLocked(ip, tpm)
	b.refill(time.Now())
	b.available -= float64(n)
}

// getOrCreateLocked must be called with c.mu held. Resets the
// bucket when the operator-supplied tpm changes, mirroring the
// RPMCache's "policy change wipes goodwill" semantics.
func (c *TPMCache) getOrCreateLocked(ip string, tpm int64) *tpmBucket {
	b, ok := c.buckets[ip]
	if !ok || b.capacity != float64(tpm) {
		b = &tpmBucket{
			capacity:   float64(tpm),
			refillRate: float64(tpm) / 60.0,
			available:  float64(tpm),
			lastRefill: time.Now(),
		}
		c.buckets[ip] = b
	}
	return b
}

// refill advances the bucket to now and tops it up to the cap.
// Caller must hold c.mu.
func (b *tpmBucket) refill(now time.Time) {
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	b.available += elapsed * b.refillRate
	if b.available > b.capacity {
		b.available = b.capacity
	}
	b.lastRefill = now
}
