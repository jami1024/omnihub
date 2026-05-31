package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/guard"
)

// MeHandler returns the identity of the authenticated admin. The SPA
// calls this on boot to decide whether the locally-cached token still
// works; a 401 routes the user to /login, a 200 unlocks the protected
// layout.
//
// AdminAuthenticator must already have run, so AdminID/AdminUser are
// set on the context.
func MeHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id":       guard.AdminID(c),
			"username": guard.AdminUser(c),
		})
	}
}
