package guard

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/admin"
)

// Context keys set by UserAuthenticator for downstream portal handlers.
const (
	CtxKeyUserID   = "omnihub.user_id"   // int64 — users.id
	CtxKeyUserName = "omnihub.user_name" // string — username
)

// UserAuthenticator gates /portal/api/* (except signup/login) on a valid
// HS256 JWT of kind "user", issued by the same admin.Issuer (shared
// secret, distinct audience). An admin-console token is rejected here,
// just as a portal token is rejected by the admin authenticator.
type UserAuthenticator struct {
	issuer *admin.Issuer
}

// NewUserAuthenticator wires a verifier around the issuer.
func NewUserAuthenticator(issuer *admin.Issuer) *UserAuthenticator {
	return &UserAuthenticator{issuer: issuer}
}

// Middleware returns the gin handler. Uses the same JSON-error envelope
// as the rest of the API.
func (a *UserAuthenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerFromHeader(c.GetHeader("Authorization"))
		if token == "" {
			abortAdminError(c, http.StatusUnauthorized, "unauthorized",
				"missing Authorization: Bearer <token>")
			return
		}
		claims, err := a.issuer.Verify(token)
		if err != nil {
			if errors.Is(err, admin.ErrTokenExpired) {
				abortAdminError(c, http.StatusUnauthorized, "token_expired",
					"session expired, please log in again")
			} else {
				abortAdminError(c, http.StatusUnauthorized, "invalid_token",
					"token signature did not verify")
			}
			return
		}
		if claims.Kind != admin.KindUser {
			abortAdminError(c, http.StatusForbidden, "forbidden",
				"this token is not valid for the user portal")
			return
		}
		c.Set(CtxKeyUserID, claims.UID)
		c.Set(CtxKeyUserName, claims.Sub)
		c.Next()
	}
}

// UserID returns the authenticated portal user's id, or 0 if absent.
func UserID(c *gin.Context) int64 {
	if v, ok := c.Get(CtxKeyUserID); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}
