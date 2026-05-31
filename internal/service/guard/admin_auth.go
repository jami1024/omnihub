package guard

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/admin"
)

// AdminAuthenticator gates /admin/api/* (except /admin/api/login) on a
// valid HS256 JWT issued by admin.Issuer. The token must arrive in the
// Authorization header as `Bearer <jwt>`.
//
// On success the middleware sets CtxKeyAdminID (int64) and
// CtxKeyAdminUser (string) so downstream handlers and audit logs can
// reference the authenticated admin without re-parsing the token.
//
// The middleware never touches the database — token validity is
// checked locally against the secret. Disabling an admin is therefore
// effective at most one JWT TTL after the password is rotated; for the
// MVP that is acceptable, and the issuer's default TTL is short (24h).
type AdminAuthenticator struct {
	issuer *admin.Issuer
}

// NewAdminAuthenticator wires a verifier around the issuer.
func NewAdminAuthenticator(issuer *admin.Issuer) *AdminAuthenticator {
	return &AdminAuthenticator{issuer: issuer}
}

// Middleware returns the gin handler. Rejections use the same
// JSON-error envelope as the admin handlers ({error:{message,type,code}})
// so the SPA can render a single error type across the API surface.
func (a *AdminAuthenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerFromHeader(c.GetHeader("Authorization"))
		if token == "" {
			abortAdminError(c, http.StatusUnauthorized, "unauthorized",
				"missing Authorization: Bearer <token>")
			return
		}
		claims, err := a.issuer.Verify(token)
		if err != nil {
			switch {
			case errors.Is(err, admin.ErrTokenExpired):
				abortAdminError(c, http.StatusUnauthorized, "token_expired",
					"session expired, please log in again")
			default:
				abortAdminError(c, http.StatusUnauthorized, "invalid_token",
					"token signature did not verify")
			}
			return
		}
		c.Set(CtxKeyAdminID, claims.UID)
		c.Set(CtxKeyAdminUser, claims.Sub)
		c.Next()
	}
}

func bearerFromHeader(h string) string {
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func abortAdminError(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message": msg,
			"type":    code,
			"code":    code,
		},
	})
}
