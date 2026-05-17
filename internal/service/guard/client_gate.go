package guard

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// DefaultAllowedUAPrefixes is the fallback when no env override is
// set: only Claude CLI passes. Operators who need to broaden the
// allow-list (e.g. permit Codex CLI or the official Anthropic SDKs)
// set OMNIHUB_ALLOWED_CLIENT_UA_PREFIXES instead.
var DefaultAllowedUAPrefixes = []string{"claude-cli/"}

// ClientGate enforces a User-Agent prefix allow-list at request
// entry. Requests whose User-Agent does not match any allowed prefix
// are rejected with 403 before authentication runs — saving a DB
// hash lookup and giving a clean "go away" answer to scanners /
// curl one-liners.
//
// IsOpen() == true means the allow-list is empty and every client
// is accepted; this is the explicit "*" opt-out path. Operators
// flipping the gate off see a warning at startup.
type ClientGate struct {
	prefixes []string // empty = open (no enforcement)
}

// NewClientGate parses the env-style spec into a gate.
//
//   - Empty / unset spec → DefaultAllowedUAPrefixes (Claude CLI only)
//   - "*"                → open gate (every client accepted)
//   - comma-separated    → exactly those prefixes
//
// Whitespace around entries is trimmed; empty entries skipped.
func NewClientGate(spec string) *ClientGate {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		// Copy so callers cannot mutate the package-level default.
		cp := make([]string, len(DefaultAllowedUAPrefixes))
		copy(cp, DefaultAllowedUAPrefixes)
		return &ClientGate{prefixes: cp}
	}
	if spec == "*" {
		return &ClientGate{prefixes: nil}
	}
	var out []string
	for _, p := range strings.Split(spec, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return &ClientGate{prefixes: out}
}

// Prefixes returns a snapshot of the active allow-list.
func (g *ClientGate) Prefixes() []string {
	out := make([]string, len(g.prefixes))
	copy(out, g.prefixes)
	return out
}

// IsOpen reports whether enforcement is off (empty allow-list).
func (g *ClientGate) IsOpen() bool { return len(g.prefixes) == 0 }

// Middleware enforces the gate. When IsOpen, the middleware is a
// pass-through.
func (g *ClientGate) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if g.IsOpen() {
			c.Next()
			return
		}
		ua := c.GetHeader("User-Agent")
		for _, p := range g.prefixes {
			if strings.HasPrefix(ua, p) {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"type": "error",
			"error": gin.H{
				"type": "client_not_allowed",
				"message": fmt.Sprintf(
					"this gateway does not accept this client (User-Agent: %q). Use Claude CLI or another approved client.",
					ua,
				),
			},
		})
	}
}
