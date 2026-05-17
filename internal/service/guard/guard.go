// Package guard hosts the OmniHub Guard chain. Each Guard is a Gin
// middleware that owns one cross-cutting concern (authentication,
// rate limiting, quota, session stickiness, etc.) and can be ordered
// independently in the chain.
//
// Guards communicate downstream by setting well-known keys on the
// gin.Context. Helpers in this package (e.g. KeyName, Model) read
// those keys back so handlers and other guards do not depend on raw
// context key strings.
package guard

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/usage"
)

// Context key names. Exported as constants so other packages can
// inspect them in tests without importing private symbols.
const (
	CtxKeyKeyName = "omnihub.key_name" // virtual API key label
	CtxKeyModel   = "omnihub.model"    // model requested by the client
	CtxKeyStream  = "omnihub.stream"   // true when the client asked for SSE
	CtxKeyUsage   = "omnihub.usage"    // usage.Usage extracted from response
	CtxKeyTTFB    = "omnihub.ttfb"     // time.Duration from request to first byte
	CtxKeyCostUSD   = "omnihub.cost_usd"   // float64 USD cost from pricing.Calculate
	CtxKeyClientIP  = "omnihub.client_ip"  // string — immediate caller's IP
	CtxKeyUserAgent = "omnihub.user_agent" // string — User-Agent header verbatim
)

// KeyName returns the virtual API key label set by the Auth guard, or
// "" if the request was unauthenticated or auth was disabled.
func KeyName(c *gin.Context) string {
	return c.GetString(CtxKeyKeyName)
}

// Model returns the upstream model name set by the handler before
// forwarding, or "" if not set.
func Model(c *gin.Context) string {
	return c.GetString(CtxKeyModel)
}

// Stream returns whether the client asked for a streaming response.
func Stream(c *gin.Context) bool {
	return c.GetBool(CtxKeyStream)
}

// Usage returns the token usage extracted from the upstream response,
// or the zero value if nothing was parsed.
func Usage(c *gin.Context) usage.Usage {
	v, ok := c.Get(CtxKeyUsage)
	if !ok {
		return usage.Usage{}
	}
	u, ok := v.(usage.Usage)
	if !ok {
		return usage.Usage{}
	}
	return u
}

// TTFB returns the time-to-first-byte duration the Forwarder measured
// for a streaming response, or 0 when not set / not streaming.
func TTFB(c *gin.Context) time.Duration {
	v, ok := c.Get(CtxKeyTTFB)
	if !ok {
		return 0
	}
	d, ok := v.(time.Duration)
	if !ok {
		return 0
	}
	return d
}

// CostUSD returns the resolved USD cost for the request together with
// whether a price was found in the pricing table. (0, false) when the
// model is unknown.
func CostUSD(c *gin.Context) (float64, bool) {
	v, ok := c.Get(CtxKeyCostUSD)
	if !ok {
		return 0, false
	}
	cost, ok := v.(float64)
	return cost, ok
}

// ClientIP returns the immediate caller IP captured at handler entry,
// or "" if not set. Useful for the request log and persistence.
func ClientIP(c *gin.Context) string { return c.GetString(CtxKeyClientIP) }

// UserAgent returns the inbound User-Agent header verbatim.
func UserAgent(c *gin.Context) string { return c.GetString(CtxKeyUserAgent) }
