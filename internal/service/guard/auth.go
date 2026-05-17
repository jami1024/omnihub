package guard

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Authenticator holds the set of allowed virtual API keys for the
// gateway. Keys are loaded once at startup; later mutation (rotation,
// revocation) lives outside this MVP and will arrive with the database
// layer.
type Authenticator struct {
	// keys maps the raw key value to a human-readable label used in
	// request logs (e.g. "alice", "ci-bot", "default"). Comparison is
	// done in constant time, so the value of the map is the *label*,
	// not a precomputed hash.
	keys map[string]string
}

// NewAuthenticator parses a comma-separated key specification into an
// Authenticator. Each entry may be either a raw key or "name:key".
// Empty / whitespace entries are skipped.
//
// Examples (env value of OMNIHUB_API_KEYS):
//
//	"omni-abc"                            // one anonymous key
//	"alice:omni-abc, bob:omni-def"        // two labelled keys
//	""                                    // auth disabled
//
// An empty spec produces an Authenticator with no keys; its Middleware
// becomes a pass-through. The caller is expected to log a warning so
// the operator is aware the gateway is unprotected.
func NewAuthenticator(spec string) *Authenticator {
	keys := make(map[string]string)
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		label, value := "default", entry
		if i := strings.Index(entry, ":"); i > 0 {
			label = strings.TrimSpace(entry[:i])
			value = strings.TrimSpace(entry[i+1:])
		}
		if value != "" {
			keys[value] = label
		}
	}
	return &Authenticator{keys: keys}
}

// Disabled reports whether the Authenticator has zero keys. A
// disabled Authenticator lets every request through; this is intended
// only for local development.
func (a *Authenticator) Disabled() bool {
	return len(a.keys) == 0
}

// KeyCount returns the number of registered keys, useful for logging.
func (a *Authenticator) KeyCount() int { return len(a.keys) }

// Middleware returns a gin.HandlerFunc that enforces virtual key auth.
//
// Accepted credentials are read in order from:
//
//  1. The "x-api-key" header (Anthropic SDK convention)
//  2. The "Authorization: Bearer <key>" header (OpenAI SDK convention)
//
// On success the gin.Context gets the matched key label under
// CtxKeyKeyName. On failure the chain is aborted with 401 in an
// Anthropic-shaped error envelope.
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.Disabled() {
			c.Set(CtxKeyKeyName, "unauthenticated")
			c.Next()
			return
		}

		key := extractKey(c.Request)
		if key == "" {
			abortUnauthorized(c, "missing api key (set x-api-key or Authorization: Bearer)")
			return
		}

		label, ok := a.validate(key)
		if !ok {
			abortUnauthorized(c, "invalid api key")
			return
		}

		c.Set(CtxKeyKeyName, label)
		c.Next()
	}
}

// validate returns (label, true) if key matches one of the registered
// keys via constant-time comparison.
func (a *Authenticator) validate(key string) (string, bool) {
	keyBytes := []byte(key)
	for k, label := range a.keys {
		if subtle.ConstantTimeCompare([]byte(k), keyBytes) == 1 {
			return label, true
		}
	}
	return "", false
}

// extractKey pulls the credential out of the standard auth headers.
func extractKey(r *http.Request) string {
	if k := r.Header.Get("x-api-key"); k != "" {
		return k
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func abortUnauthorized(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "authentication_error",
			"message": msg,
		},
	})
}
