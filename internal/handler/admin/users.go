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
	SetPriceRatio(ctx context.Context, id int64, ratio float64) error
	DeleteByID(ctx context.Context, id int64) error
}

// userDTO is the admin wire shape: profile + aggregates. No password hash.
type userDTO struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	Email      string    `json:"email"`
	Enabled    bool      `json:"enabled"`
	KeyCount   int       `json:"key_count"`
	Spend30d   float64   `json:"spend_30d"`
	PriceRatio float64   `json:"price_ratio"`
	CreatedAt  time.Time `json:"created_at"`
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
				KeyCount: u.KeyCount, Spend30d: u.Spend30d, PriceRatio: u.PriceRatio, CreatedAt: u.CreatedAt,
			}
		}
		c.JSON(http.StatusOK, gin.H{"users": out})
	}
}

// UpdateUserHandler handles PATCH /admin/api/users/:id — set the enabled
// flag and/or the billing price ratio. Either field may be present.
func UpdateUserHandler(store userMgmtStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in struct {
			Enabled    *bool    `json:"enabled"`
			PriceRatio *float64 `json:"price_ratio"`
		}
		if err := c.ShouldBindJSON(&in); err != nil || (in.Enabled == nil && in.PriceRatio == nil) {
			writeBadRequest(c, "provide enabled (true/false) and/or price_ratio (>= 0)")
			return
		}
		if in.PriceRatio != nil && (*in.PriceRatio < 0 || *in.PriceRatio > 1000) {
			writeBadRequest(c, "price_ratio must be between 0 and 1000")
			return
		}

		if in.Enabled != nil {
			if err := store.SetEnabled(c.Request.Context(), id, *in.Enabled); err != nil {
				writeUserUpdateError(c, id, err)
				return
			}
			slog.Info("admin: user enabled set", "id", id, "enabled", *in.Enabled, "admin", adminActor(c))
		}
		if in.PriceRatio != nil {
			if err := store.SetPriceRatio(c.Request.Context(), id, *in.PriceRatio); err != nil {
				writeUserUpdateError(c, id, err)
				return
			}
			slog.Info("admin: user price_ratio set", "id", id, "ratio", *in.PriceRatio, "admin", adminActor(c))
		}
		c.Status(http.StatusNoContent)
	}
}

// writeUserUpdateError maps a user update failure to the right response.
func writeUserUpdateError(c *gin.Context, id int64, err error) {
	if errors.Is(err, repository.ErrUserNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "user not found")
		return
	}
	slog.Error("admin: update user failed", "id", id, "err", err.Error())
	writeInternal(c, "could not update user")
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
