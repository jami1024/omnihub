package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/guard"
)

// keyStore is the slice of repository.ApiKeyRepo the portal needs. Every
// method is scoped to the authenticated user (ListByUser / *OwnedBy) so
// one user can never see or touch another's keys.
type keyStore interface {
	ListByUser(ctx context.Context, userID int64) ([]*apikey.Key, error)
	Insert(ctx context.Context, p repository.ApiKeyInsertParams) (int64, error)
	GetByID(ctx context.Context, id int64) (*apikey.Key, error)
	DeleteByIDOwnedBy(ctx context.Context, id, userID int64) error
}

// spendStore reports the rolling 24h USD spend for one key (quota view).
type spendStore interface {
	SumCostByKey(ctx context.Context, keyName string) (float64, error)
}

// keyDTO is the portal wire shape. The cleartext and hash are never
// present; `name` is the unique handle the user chose (portal keys carry
// no separate label, so usage attributes cleanly to the name).
type keyDTO struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	DailyUSDLimit *float64 `json:"daily_usd_limit"`
	RPMLimit      *int     `json:"rpm_limit"`
	AllowedModels []string `json:"allowed_models"`
	Spend24h      float64  `json:"spend_24h"`
}

type createKeyResponse struct {
	keyDTO
	Key string `json:"key"`
}

func toKeyDTO(k *apikey.Key, spend float64) keyDTO {
	models := k.AllowedModels
	if models == nil {
		models = []string{}
	}
	return keyDTO{
		ID: k.ID, Name: k.Name, Enabled: k.Enabled,
		DailyUSDLimit: k.DailyUSDLimit, RPMLimit: k.RPMLimit,
		AllowedModels: models, Spend24h: spend,
	}
}

// ListKeysHandler returns GET /portal/api/keys → the user's own keys,
// each with its rolling 24h spend.
func ListKeysHandler(store keyStore, spend spendStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := guard.UserID(c)
		keys, err := store.ListByUser(c.Request.Context(), uid)
		if err != nil {
			slog.Error("portal: list keys failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not list keys")
			return
		}
		out := make([]keyDTO, len(keys))
		for i, k := range keys {
			s, _ := spend.SumCostByKey(c.Request.Context(), k.Name) // best-effort
			out[i] = toKeyDTO(k, s)
		}
		c.JSON(http.StatusOK, gin.H{"keys": out})
	}
}

type keyInput struct {
	Name          string   `json:"name"`
	DailyUSDLimit *float64 `json:"daily_usd_limit"`
	RPMLimit      *int     `json:"rpm_limit"`
	AllowedModels []string `json:"allowed_models"`
}

// CreateKeyHandler handles POST /portal/api/keys. The key is generated
// server-side and the cleartext returned exactly once; only the hash is
// stored. The new key is owned by the authenticated user.
func CreateKeyHandler(store keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := guard.UserID(c)
		var in keyInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			writeBadRequest(c, "name is required")
			return
		}
		if in.RPMLimit != nil && *in.RPMLimit <= 0 {
			writeBadRequest(c, "rpm_limit must be greater than 0 (omit for no limit)")
			return
		}
		if in.DailyUSDLimit != nil && *in.DailyUSDLimit < 0 {
			writeBadRequest(c, "daily_usd_limit cannot be negative")
			return
		}

		cleartext, err := apikey.Generate()
		if err != nil {
			writeInternal(c, "could not generate key")
			return
		}
		var models []string
		for _, m := range in.AllowedModels {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}
		id, err := store.Insert(c.Request.Context(), repository.ApiKeyInsertParams{
			Name:          in.Name,
			Hash:          apikey.HashOf(cleartext),
			Enabled:       true,
			DailyUSDLimit: in.DailyUSDLimit,
			RPMLimit:      in.RPMLimit,
			AllowedModels: models,
			UserID:        &uid,
		})
		if err != nil {
			if errors.Is(err, repository.ErrApiKeyNameTaken) {
				writeError(c, http.StatusConflict, "name_taken", "that key name is taken; choose another")
				return
			}
			slog.Error("portal: create key failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not create key")
			return
		}
		slog.Info("portal: key created", "uid", uid, "id", id, "name", in.Name)
		k, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusCreated, createKeyResponse{keyDTO: keyDTO{ID: id, Name: in.Name, Enabled: true, AllowedModels: []string{}}, Key: cleartext})
			return
		}
		c.JSON(http.StatusCreated, createKeyResponse{keyDTO: toKeyDTO(k, 0), Key: cleartext})
	}
}

// DeleteKeyHandler handles DELETE /portal/api/keys/:id — only the user's
// own key. A foreign or missing id returns 404.
func DeleteKeyHandler(store keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			writeBadRequest(c, "invalid key id")
			return
		}
		uid := guard.UserID(c)
		if err := store.DeleteByIDOwnedBy(c.Request.Context(), id, uid); err != nil {
			if errors.Is(err, repository.ErrApiKeyNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "key not found")
				return
			}
			slog.Error("portal: delete key failed", "uid", uid, "id", id, "err", err.Error())
			writeInternal(c, "could not delete key")
			return
		}
		slog.Info("portal: key deleted", "uid", uid, "id", id)
		c.Status(http.StatusNoContent)
	}
}
