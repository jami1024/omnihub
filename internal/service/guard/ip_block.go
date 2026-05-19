package guard

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/blockedip"
	"github.com/jami1024/omnihub/internal/service/limits"
)

// ctxKeyIPPolicy is the gin context key for the resolved per-IP
// policy, set by IPBlockMiddleware so handlers / post-response
// hooks can fetch it without re-walking the pool.
const ctxKeyIPPolicy = "guard.ipPolicy"

// IPBlockMiddleware returns a gin middleware that enforces the
// per-IP policy stored in the blocked_ips table. It runs as early
// as possible in the chain so a hard-blocked or over-limit IP
// burns no auth or downstream cycles.
//
// Policy semantics (a single row per IP):
//
//   - All limits zero → hard block, 403 (no information leaked).
//   - concurrent_limit > 0 → reject when in-flight count >= cap, 429.
//   - rpm_limit > 0 → reject when token bucket exhausted, 429.
//   - tpm_limit > 0 → reject when fresh-input bucket exhausted, 429.
//
// A nil pool or unknown IP short-circuits to allow. On a successful
// acquire, the middleware registers a c.Next-aware release so the
// concurrency counter decrements when the handler returns even on
// panic / early abort.
func IPBlockMiddleware(pool *blockedip.Pool, rpm *limits.RPMCache) gin.HandlerFunc {
	return func(c *gin.Context) {
		if pool == nil {
			c.Next()
			return
		}
		ip := c.ClientIP()
		policy := pool.Lookup(ip)
		if policy == nil {
			c.Next()
			return
		}

		if policy.Blocked() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "forbidden",
					"message": "this client is not permitted to use the gateway",
				},
			})
			return
		}

		// Concurrency check first — cheapest and binary.
		if policy.ConcurrentLimit > 0 {
			if !pool.TryAcquireConcurrency(ip, policy.ConcurrentLimit) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"type": "error",
					"error": gin.H{
						"type":    "rate_limit_error",
						"message": "per-IP concurrent limit reached",
					},
				})
				return
			}
			defer pool.ReleaseConcurrency(ip)
		}

		// RPM bucket — keyed by "ip:"+addr so we don't collide with
		// per-key buckets in the same RPMCache.
		if policy.RPMLimit > 0 && rpm != nil {
			if !rpm.Allow("ip:"+ip, policy.RPMLimit) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"type": "error",
					"error": gin.H{
						"type":    "rate_limit_error",
						"message": "per-IP request-rate limit reached",
					},
				})
				return
			}
		}

		// TPM check is entry-time only (we don't know the precise
		// fresh-token cost of THIS request yet). Allow if the bucket
		// has any remaining budget; the post-response hook charges
		// the actual cost.
		if policy.TPMLimit > 0 {
			if !pool.TPMBucket().Allow(ip, policy.TPMLimit) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"type": "error",
					"error": gin.H{
						"type":    "rate_limit_error",
						"message": "per-IP token-rate limit reached",
					},
				})
				return
			}
		}

		c.Set(ctxKeyIPPolicy, policy)
		c.Next()
	}
}

// IPPolicy returns the per-IP policy resolved earlier in the chain,
// or nil when the request bypassed the middleware (e.g. nil pool).
func IPPolicy(c *gin.Context) *blockedip.Policy {
	if v, ok := c.Get(ctxKeyIPPolicy); ok {
		if p, ok := v.(*blockedip.Policy); ok {
			return p
		}
	}
	return nil
}
