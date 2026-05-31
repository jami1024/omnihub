package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/admin"
)

// userLookup is the narrow slice of repository.AdminUserRepo the login
// handler needs. Letting the parameter be an interface lets the unit
// test stub it without standing up a Postgres connection.
type userLookup interface {
	GetByUsername(ctx context.Context, username string) (*admin.User, error)
}

// LoginHandler returns a gin.HandlerFunc for POST /admin/api/login.
//
// Body: {"username":"...", "password":"..."}
// Success 200: {"token":"...", "expires_at": <unix>, "username":"..."}
// Failure 401: error envelope with type="invalid_credentials"
//
// Disabled users are rejected with the same error type as a wrong
// password so an attacker cannot enumerate which admins exist.
func LoginHandler(repo userLookup, issuer *admin.Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		body.Username = strings.TrimSpace(body.Username)
		if body.Username == "" || body.Password == "" {
			writeBadRequest(c, "username and password are required")
			return
		}

		user, err := repo.GetByUsername(c.Request.Context(), body.Username)
		if err != nil {
			if errors.Is(err, repository.ErrAdminUserNotFound) {
				writeError(c, http.StatusUnauthorized, "invalid_credentials",
					"invalid username or password")
				return
			}
			slog.Error("admin login: user lookup failed", "err", err.Error())
			writeInternal(c, "user lookup failed")
			return
		}
		if !user.Enabled {
			writeError(c, http.StatusUnauthorized, "invalid_credentials",
				"invalid username or password")
			return
		}
		if err := admin.VerifyPassword(user.PasswordHash, body.Password); err != nil {
			writeError(c, http.StatusUnauthorized, "invalid_credentials",
				"invalid username or password")
			return
		}

		token, exp, err := issuer.Issue(user.Username, user.ID)
		if err != nil {
			slog.Error("admin login: token issue failed", "err", err.Error())
			writeInternal(c, "could not issue token")
			return
		}
		slog.Info("admin login", "username", user.Username, "uid", user.ID)
		c.JSON(http.StatusOK, gin.H{
			"token":      token,
			"expires_at": exp.Unix(),
			"username":   user.Username,
		})
	}
}
