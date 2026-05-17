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

import "github.com/gin-gonic/gin"

// Context key names. Exported as constants so other packages can
// inspect them in tests without importing private symbols.
const (
	CtxKeyKeyName = "omnihub.key_name" // virtual API key label
	CtxKeyModel   = "omnihub.model"    // model requested by the client
	CtxKeyStream  = "omnihub.stream"   // true when the client asked for SSE
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
