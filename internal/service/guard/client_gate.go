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

// signalCheck inspects the request and returns the empty string when
// the signal is present, or a human-readable reason when it is not.
// Reasons are surfaced in the 403 body so a misconfigured legitimate
// client can self-diagnose.
type signalCheck func(*gin.Context) string

// claudeCLISignals are the companion headers a request claiming to be
// Claude CLI must carry. They mirror what real Claude CLI 2.x emits,
// so simply forging "User-Agent: claude-cli/..." with curl no longer
// gets past the gate.
//
// We deliberately do NOT inspect the request body (e.g. for the
// metadata.user_id field claude-code-hub also checks): the gate runs
// before auth and must not drain/buffer the body that early in the
// chain. Header-level signals catch the overwhelming majority of
// spoofing attempts at zero body-IO cost.
var claudeCLISignals = []signalCheck{
	requireHeaderEquals("X-App", "cli"),
	requireHeaderPresent("Anthropic-Beta"),
}

// knownSignalRules attaches extra signals to specific UA prefixes.
// Prefixes not listed here pass the gate on the UA match alone — this
// lets operators allow-list custom clients (e.g. "codex-cli/") without
// forcing them to emit Anthropic-specific headers.
var knownSignalRules = map[string][]signalCheck{
	"claude-cli/": claudeCLISignals,
}

func requireHeaderPresent(name string) signalCheck {
	return func(c *gin.Context) string {
		if strings.TrimSpace(c.GetHeader(name)) == "" {
			return fmt.Sprintf("missing required header %q", name)
		}
		return ""
	}
}

func requireHeaderEquals(name, want string) signalCheck {
	return func(c *gin.Context) string {
		got := strings.TrimSpace(c.GetHeader(name))
		if got == want {
			return ""
		}
		if got == "" {
			return fmt.Sprintf("missing required header %q (expected %q)", name, want)
		}
		return fmt.Sprintf("header %q is %q, expected %q", name, got, want)
	}
}

// uaRule pairs an allowed prefix with optional companion signals. All
// signals must pass for the request to be accepted; a UA-prefix match
// with a failing signal is still a rejection.
type uaRule struct {
	prefix  string
	signals []signalCheck
}

// ClientGate enforces a User-Agent prefix allow-list at request
// entry, optionally backed by multi-signal verification per prefix.
// Requests whose User-Agent does not match any allowed prefix — or
// match the prefix but fail the companion signals — are rejected with
// 403 before authentication runs. This saves a DB hash lookup and
// gives a clean "go away" answer to scanners / curl one-liners.
//
// IsOpen() == true means the allow-list is empty and every client
// is accepted; this is the explicit "*" opt-out path. Operators
// flipping the gate off see a warning at startup.
type ClientGate struct {
	rules []uaRule
}

// NewClientGate parses the env-style spec into a gate.
//
//   - Empty / unset spec → DefaultAllowedUAPrefixes (Claude CLI only)
//   - "*"                → open gate (every client accepted)
//   - comma-separated    → exactly those prefixes
//
// Prefixes with entries in knownSignalRules get extra multi-signal
// checks attached automatically — operators do not opt in. Custom
// prefixes pass on the UA match alone. Prefix order is preserved: the
// first matching prefix wins, so put the strictest entry first when
// using overlapping prefixes (e.g. "claude-cli/" before "claude-").
func NewClientGate(spec string) *ClientGate {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return &ClientGate{rules: rulesFor(DefaultAllowedUAPrefixes)}
	}
	if spec == "*" {
		return &ClientGate{rules: nil}
	}
	var prefixes []string
	for _, p := range strings.Split(spec, ",") {
		if t := strings.TrimSpace(p); t != "" {
			prefixes = append(prefixes, t)
		}
	}
	return &ClientGate{rules: rulesFor(prefixes)}
}

func rulesFor(prefixes []string) []uaRule {
	out := make([]uaRule, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, uaRule{prefix: p, signals: knownSignalRules[p]})
	}
	return out
}

// Prefixes returns a snapshot of the active allow-list (in declaration
// order). Multi-signal companions are intentionally not exposed — they
// are an implementation detail of each prefix.
func (g *ClientGate) Prefixes() []string {
	out := make([]string, len(g.rules))
	for i, r := range g.rules {
		out[i] = r.prefix
	}
	return out
}

// IsOpen reports whether enforcement is off (empty allow-list).
func (g *ClientGate) IsOpen() bool { return len(g.rules) == 0 }

// Middleware enforces the gate. When IsOpen, the middleware is a
// pass-through.
func (g *ClientGate) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if g.IsOpen() {
			c.Next()
			return
		}
		ua := c.GetHeader("User-Agent")
		for _, r := range g.rules {
			if !strings.HasPrefix(ua, r.prefix) {
				continue
			}
			if reason := failingSignal(c, r.signals); reason != "" {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"type": "error",
					"error": gin.H{
						"type": "client_not_allowed",
						"message": fmt.Sprintf(
							"this gateway accepts %q clients but the request is missing required signals: %s. Use the real Claude CLI or another approved client.",
							r.prefix, reason,
						),
					},
				})
				return
			}
			c.Next()
			return
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

func failingSignal(c *gin.Context, checks []signalCheck) string {
	for _, ch := range checks {
		if reason := ch(c); reason != "" {
			return reason
		}
	}
	return ""
}
