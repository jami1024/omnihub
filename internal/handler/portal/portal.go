// Package portal serves the end-user self-service surface at
// /portal/api/*: open signup + login, and (token-gated) the user's own
// profile, keys, and usage. It is deliberately separate from the admin
// console: a portal token can't reach /admin and vice versa (the JWT
// carries a "user" kind). Handlers only ever touch the authenticated
// user's own rows.
package portal

import (
	"context"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
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
	GetByEmail(ctx context.Context, email string) (*repository.User, error)
	GetByID(ctx context.Context, id int64) (*repository.User, error)
	Insert(ctx context.Context, p repository.UserInsertParams) (int64, error)
}

// settingsProvider exposes the admin-controlled portal policy (signup
// toggle + per-key limit default/ceiling + signup bonus).
type settingsProvider interface {
	Get(ctx context.Context) (repository.PortalSettings, error)
}

// signupBonusStore credits a new user's wallet with the configured signup
// bonus. Satisfied by *repository.WalletRepo.
type signupBonusStore interface {
	AddEntry(ctx context.Context, userID int64, kind string, amountUSD float64, note, createdBy string) error
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// SignupHandler handles POST /portal/api/signup — self-registration,
// gated on the admin's signup_enabled policy. On success it logs the
// user straight in (returns a portal token).
func SignupHandler(store userStore, settings settingsProvider, wallet signupBonusStore, issuer *admin.Issuer) gin.HandlerFunc {
	return SignupHandlerWithReservedEmail(store, settings, wallet, issuer, "")
}

// SignupHandlerWithReservedEmail is SignupHandler plus an optional
// reserved admin email that cannot be self-registered as a portal user.
func SignupHandlerWithReservedEmail(store userStore, settings settingsProvider, wallet signupBonusStore, issuer *admin.Issuer, reservedAdminEmail string) gin.HandlerFunc {
	reservedAdminEmail = normalizeEmail(reservedAdminEmail)
	return func(c *gin.Context) {
		s, sErr := settings.Get(c.Request.Context())
		if sErr == nil && !s.SignupEnabled {
			writeError(c, http.StatusForbidden, "signup_disabled",
				"registration is closed; ask an administrator for an account")
			return
		}
		var in credentials
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		email := normalizeEmail(in.Email)
		if email == "" && strings.Contains(in.Username, "@") {
			email = normalizeEmail(in.Username)
		}
		if !validEmail(email) {
			writeBadRequest(c, "valid email is required")
			return
		}
		if reservedAdminEmail != "" && email == reservedAdminEmail {
			writeError(c, http.StatusConflict, "email_taken", "that email is already registered")
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
			Username: email, Email: email, PasswordHash: hash,
		})
		if err != nil {
			if err == repository.ErrUsernameTaken || err == repository.ErrEmailTaken {
				writeError(c, http.StatusConflict, "email_taken", "that email is already registered")
				return
			}
			writeInternal(c, "could not create account")
			return
		}

		// Signup bonus: grant the configured starting credit so a new user
		// can use their keys immediately under prepaid billing. Best-effort
		// — a bonus failure must not fail an otherwise-successful signup.
		if sErr == nil && s.SignupBonusUSD > 0 && wallet != nil {
			if err := wallet.AddEntry(c.Request.Context(), id, "bonus", s.SignupBonusUSD, "signup bonus", "system"); err != nil {
				slog.Error("portal: signup bonus failed", "uid", id, "err", err.Error())
			} else {
				slog.Info("portal: signup bonus granted", "uid", id, "amount", s.SignupBonusUSD)
			}
		}

		issueToken(c, issuer, email, id)
	}
}

// LoginHandler handles POST /portal/api/login. A wrong email and a
// wrong password return the same 401 so accounts can't be enumerated.
func LoginHandler(store userStore, issuer *admin.Issuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in credentials
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		email := normalizeEmail(in.Email)
		if email == "" && strings.Contains(in.Username, "@") {
			email = normalizeEmail(in.Username)
		}
		if email == "" || in.Password == "" {
			writeBadRequest(c, "email and password are required")
			return
		}
		u, err := store.GetByEmail(c.Request.Context(), email)
		if err != nil || !u.Enabled || admin.VerifyPassword(u.PasswordHash, in.Password) != nil {
			writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		identity := normalizeEmail(u.Email)
		if identity == "" {
			identity = normalizeEmail(u.Username)
		}
		issueToken(c, issuer, identity, u.ID)
	}
}

func issueToken(c *gin.Context, issuer *admin.Issuer, username string, uid int64) {
	token, exp, err := issuer.IssueKind(username, uid, admin.KindUser)
	if err != nil {
		writeInternal(c, "could not issue session")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":       token,
		"expires_at":  exp.Unix(),
		"username":    username,
		"email":       username,
		"role":        "user",
		"redirect_to": "/portal",
	})
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

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validEmail(s string) bool {
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && normalizeEmail(addr.Address) == s
}

func parseIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeBadRequest(c, "invalid id")
		return 0, false
	}
	return id, true
}
