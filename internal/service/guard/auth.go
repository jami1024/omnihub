package guard

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

// KeyLookup resolves a submitted key (cleartext) into the matching
// apikey.Key record, or nil if no enabled key matches. The Auth
// Guard calls it once per request.
//
// Production wires this to apikey.Pool.LookupByHash(HashOf(key)).
// The interface keeps the guard testable without a pool.
type KeyLookup func(submitted string) *apikey.Key

// Authenticator validates inbound credentials against the configured
// KeyLookup. A nil lookup disables auth entirely and tags every
// request with the label "unauthenticated" — useful only for local
// development.
type Authenticator struct {
	lookup KeyLookup
}

// NewAuthenticator returns an Authenticator that delegates to the
// provided lookup. Pass nil to disable auth (with a loud warning at
// the call site).
func NewAuthenticator(lookup KeyLookup) *Authenticator {
	return &Authenticator{lookup: lookup}
}

// Disabled reports whether auth is turned off (no lookup wired).
func (a *Authenticator) Disabled() bool { return a.lookup == nil }

// Middleware enforces virtual key auth. Accepted credentials come
// from x-api-key (Anthropic SDK) or Authorization: Bearer (OpenAI
// SDK). On success the context gets:
//
//   - CtxKeyKeyName  : the human label
//   - CtxKeyAPIKeyID : the DB primary key, for the upcoming Limits Guard
//
// On failure the chain is aborted with a 401 in an Anthropic-shaped
// error envelope.
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.Disabled() {
			c.Set(CtxKeyKeyName, "unauthenticated")
			c.Next()
			return
		}

		raw := extractKey(c.Request)
		if raw == "" {
			abortUnauthorized(c, "missing api key (set x-api-key or Authorization: Bearer)")
			return
		}

		k := a.lookup(raw)
		if k == nil {
			abortUnauthorized(c, "invalid api key")
			return
		}

		label := k.Label
		if label == "" {
			label = k.Name
		}
		c.Set(CtxKeyKeyName, label)
		c.Set(CtxKeyAPIKeyID, k.ID)
		c.Next()
	}
}

// extractKey pulls the credential out of the two standard auth
// headers we accept.
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
