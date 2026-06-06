package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
	adminsvc "github.com/jami1024/omnihub/internal/service/admin"
)

type userStore interface {
	GetByEmail(ctx context.Context, email string) (*repository.User, error)
}

func writeError(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"message": msg, "type": code, "code": code}})
}

// LoginHandler is the single web login endpoint. It decides role on the
// server: the configured admin email signs into /admin, every other
// email is checked against portal users and signs into /portal.
func LoginHandler(adminCreds admin.EnvAdminCredentials, users userStore, issuer *adminsvc.Issuer) gin.HandlerFunc {
	adminEmail := normalizeEmail(adminCreds.Email)
	return func(c *gin.Context) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			writeError(c, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
			return
		}
		email := normalizeEmail(in.Email)
		if email == "" || in.Password == "" {
			writeError(c, http.StatusBadRequest, "bad_request", "email and password are required")
			return
		}

		if adminEmail != "" && email == adminEmail {
			if adminsvc.VerifyPassword(adminCreds.PasswordHash, in.Password) != nil {
				writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
				return
			}
			token, exp, err := issuer.Issue(adminEmail, 0)
			if err != nil {
				slog.Error("unified login: admin token issue failed", "err", err.Error())
				writeError(c, http.StatusInternalServerError, "internal_error", "could not issue session")
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"token":       token,
				"expires_at":  exp.Unix(),
				"username":    adminEmail,
				"email":       adminEmail,
				"role":        "admin",
				"redirect_to": "/admin",
			})
			return
		}

		u, err := users.GetByEmail(c.Request.Context(), email)
		if err != nil || !u.Enabled || adminsvc.VerifyPassword(u.PasswordHash, in.Password) != nil {
			writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		identity := normalizeEmail(u.Email)
		if identity == "" {
			identity = normalizeEmail(u.Username)
		}
		token, exp, err := issuer.IssueKind(identity, u.ID, adminsvc.KindUser)
		if err != nil {
			slog.Error("unified login: user token issue failed", "err", err.Error())
			writeError(c, http.StatusInternalServerError, "internal_error", "could not issue session")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"token":       token,
			"expires_at":  exp.Unix(),
			"username":    identity,
			"email":       identity,
			"role":        "user",
			"redirect_to": "/portal",
		})
	}
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
