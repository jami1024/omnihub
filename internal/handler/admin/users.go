package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// userMgmtStore is the slice of repository.UserRepo the admin user-
// management handlers depend on.
type userMgmtStore interface {
	ListWithStats(ctx context.Context) ([]repository.UserStat, error)
	SetEnabled(ctx context.Context, id int64, enabled bool) error
	DeleteByID(ctx context.Context, id int64) error
}

// userDTO is the admin wire shape: profile + aggregates. No password hash.
type userDTO struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Enabled   bool      `json:"enabled"`
	KeyCount  int       `json:"key_count"`
	Spend30d  float64   `json:"spend_30d"`
	CreatedAt time.Time `json:"created_at"`
}

// ListUsersHandler returns GET /admin/api/users → {"users":[…]}.
func ListUsersHandler(store userMgmtStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := store.ListWithStats(c.Request.Context())
		if err != nil {
			slog.Error("admin: list users failed", "err", err.Error())
			writeInternal(c, "could not list users")
			return
		}
		out := make([]userDTO, len(rows))
		for i, u := range rows {
			out[i] = userDTO{
				ID: u.ID, Username: u.Username, Email: u.Email, Enabled: u.Enabled,
				KeyCount: u.KeyCount, Spend30d: u.Spend30d, CreatedAt: u.CreatedAt,
			}
		}
		c.JSON(http.StatusOK, gin.H{"users": out})
	}
}

// UpdateUserHandler handles PATCH /admin/api/users/:id — toggle enabled.
func UpdateUserHandler(store userMgmtStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&in); err != nil || in.Enabled == nil {
			writeBadRequest(c, "enabled (true/false) is required")
			return
		}
		if err := store.SetEnabled(c.Request.Context(), id, *in.Enabled); err != nil {
			if errors.Is(err, repository.ErrUserNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "user not found")
				return
			}
			slog.Error("admin: update user failed", "id", id, "err", err.Error())
			writeInternal(c, "could not update user")
			return
		}
		slog.Info("admin: user enabled set", "id", id, "enabled", *in.Enabled, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// DeleteUserHandler handles DELETE /admin/api/users/:id → 204. The
// user's keys survive but become unowned.
func DeleteUserHandler(store userMgmtStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := store.DeleteByID(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrUserNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "user not found")
				return
			}
			slog.Error("admin: delete user failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete user")
			return
		}
		slog.Info("admin: user deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}
