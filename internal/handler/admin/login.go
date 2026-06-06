package admin

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/admin"
)

// EnvAdminCredentials is the single operator identity configured from
// environment variables. Admins are not self-registered and are not
// created through the public portal.
type EnvAdminCredentials struct {
	Email        string
	PasswordHash string
}

// LoginHandler returns a gin.HandlerFunc for POST /admin/api/login.
//
// Body: {"email":"...", "password":"..."}
// Success 200:
// {"token":"...", "expires_at": <unix>, "username":"...", "email":"...", "role":"admin"}
// Failure 401: error envelope with type="invalid_credentials"
//
// Unknown emails and wrong passwords return the same error type so the
// configured admin email cannot be enumerated.
func LoginHandler(creds EnvAdminCredentials, issuer *admin.Issuer) gin.HandlerFunc {
	adminEmail := normalizeEmail(creds.Email)
	return func(c *gin.Context) {
		var body struct {
			Email    string `json:"email"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		email := normalizeEmail(body.Email)
		if email == "" {
			// Compatibility for older clients during the username → email
			// migration. The UI now sends email.
			email = normalizeEmail(body.Username)
		}
		if email == "" || body.Password == "" {
			writeBadRequest(c, "email and password are required")
			return
		}

		if adminEmail == "" || email != adminEmail || admin.VerifyPassword(creds.PasswordHash, body.Password) != nil {
			writeError(c, http.StatusUnauthorized, "invalid_credentials",
				"invalid email or password")
			return
		}

		token, exp, err := issuer.Issue(adminEmail, 0)
		if err != nil {
			slog.Error("admin login: token issue failed", "err", err.Error())
			writeInternal(c, "could not issue token")
			return
		}
		slog.Info("admin login", "email", adminEmail)
		c.JSON(http.StatusOK, gin.H{
			"token":       token,
			"expires_at":  exp.Unix(),
			"username":    adminEmail,
			"email":       adminEmail,
			"role":        "admin",
			"redirect_to": "/admin",
		})
	}
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
