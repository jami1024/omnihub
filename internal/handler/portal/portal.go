// Package portal serves the end-user self-service surface at
// /portal/api/*: open signup + login, and (token-gated) the user's own
// profile, keys, and usage. It is deliberately separate from the admin
// console: a portal token can't reach /admin and vice versa (the JWT
// carries a "user" kind). Handlers only ever touch the authenticated
// user's own rows.
package portal

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/admin"
	"github.com/jami1024/omnihub/internal/service/guard"
)

// writeError emits the canonical {error:{message,type,code}} envelope,
// matching the admin API so the SPA renders one error shape everywhere.
func writeError(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"message": msg, "type": code, "code": code}})
}
func writeBadRequest(c *gin.Context, msg string) {
	writeError(c, http.StatusBadRequest, "bad_request", msg)
}
func writeInternal(c *gin.Context, msg string) {
	writeError(c, http.StatusInternalServerError, "internal_error", msg)
}

// userStore is the slice of repository.UserRepo the auth handlers need.
type userStore interface {
	GetByUsername(ctx context.Context, username string) (*repository.User, error)
	GetByID(ctx context.Context, id int64) (*repository.User, error)
	Insert(ctx context.Context, p repository.UserInsertParams) (int64, error)
}

// settingsProvider exposes the admin-controlled portal policy (signup
// toggle + per-key limit default/ceiling).
type settingsProvider interface {
	Get(ctx context.Context) (repository.PortalSettings, error)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// SignupHandler handles POST /portal/api/signup — self-registration,
// gated on the admin's signup_enabled policy. On success it logs the
// user straight in (returns a portal token).
func SignupHandler(store userStore, settings settingsProvider, issuer *admin.Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s, err := settings.Get(c.Request.Context()); err == nil && !s.SignupEnabled {
			writeError(c, http.StatusForbidden, "signup_disabled",
				"registration is closed; ask an administrator for an account")
			return
		}
		var in credentials
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Username = strings.TrimSpace(in.Username)
		if len(in.Username) < 3 {
			writeBadRequest(c, "username must be at least 3 characters")
			return
		}
		if len(in.Password) < 8 {
			writeBadRequest(c, "password must be at least 8 characters")
			return
		}
		hash, err := admin.HashPassword(in.Password)
		if err != nil {
			writeInternal(c, "could not create account")
			return
		}
		id, err := store.Insert(c.Request.Context(), repository.UserInsertParams{
			Username: in.Username, Email: strings.TrimSpace(in.Email), PasswordHash: hash,
		})
		if err != nil {
			if err == repository.ErrUsernameTaken {
				writeError(c, http.StatusConflict, "username_taken", "that username is taken")
				return
			}
			writeInternal(c, "could not create account")
			return
		}
		issueToken(c, issuer, in.Username, id)
	}
}

// LoginHandler handles POST /portal/api/login. A wrong username and a
// wrong password return the same 401 so accounts can't be enumerated.
func LoginHandler(store userStore, issuer *admin.Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in credentials
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Username = strings.TrimSpace(in.Username)
		if in.Username == "" || in.Password == "" {
			writeBadRequest(c, "username and password are required")
			return
		}
		u, err := store.GetByUsername(c.Request.Context(), in.Username)
		if err != nil || !u.Enabled || admin.VerifyPassword(u.PasswordHash, in.Password) != nil {
			writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
			return
		}
		issueToken(c, issuer, u.Username, u.ID)
	}
}

func issueToken(c *gin.Context, issuer *admin.Issuer, username string, uid int64) {
	token, exp, err := issuer.IssueKind(username, uid, admin.KindUser)
	if err != nil {
		writeInternal(c, "could not issue session")
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": exp.Unix(), "username": username})
}

// MeHandler handles GET /portal/api/me → the authenticated user's profile.
func MeHandler(store userStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, err := store.GetByID(c.Request.Context(), guard.UserID(c))
		if err != nil {
			writeError(c, http.StatusUnauthorized, "unauthorized", "account not found")
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": u.ID, "username": u.Username, "email": u.Email})
	}
}
