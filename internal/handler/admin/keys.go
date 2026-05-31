package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/apikey"
)

// keyStore is the slice of repository.ApiKeyRepo the key handlers depend
// on. Narrowing it to an interface lets the unit tests stand in a fake
// without a live Postgres connection.
type keyStore interface {
	ListAll(ctx context.Context) ([]*apikey.Key, error)
	GetByID(ctx context.Context, id int64) (*apikey.Key, error)
	Insert(ctx context.Context, p repository.ApiKeyInsertParams) (int64, error)
	UpdateMeta(ctx context.Context, id int64, p repository.ApiKeyUpdateParams) error
	DeleteByID(ctx context.Context, id int64) error
}

// keyDTO is the JSON shape returned to the SPA. The cleartext value and
// its hash are never present: a key is shown exactly once, at creation,
// via createKeyResponse.Key. Every later read is metadata-only.
type keyDTO struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Label         string   `json:"label"`
	Enabled       bool     `json:"enabled"`
	DailyUSDLimit *float64 `json:"daily_usd_limit"`
	RPMLimit      *int     `json:"rpm_limit"`
	AllowedModels []string `json:"allowed_models"`
}

// createKeyResponse is the 201 body for a freshly minted key: the
// metadata plus the one-and-only-time cleartext. The browser must
// surface `key` to the operator immediately; it is unrecoverable after.
type createKeyResponse struct {
	keyDTO
	Key string `json:"key"`
}

// toKeyDTO projects an apikey.Key onto the redacted wire shape.
func toKeyDTO(k *apikey.Key) keyDTO {
	models := k.AllowedModels
	if models == nil {
		models = []string{} // marshal as [] not null, so the UI can map() freely
	}
	return keyDTO{
		ID:            k.ID,
		Name:          k.Name,
		Label:         k.Label,
		Enabled:       k.Enabled,
		DailyUSDLimit: k.DailyUSDLimit,
		RPMLimit:      k.RPMLimit,
		AllowedModels: models,
	}
}

// keyInput is the create/update request body. Update is a full-metadata
// replace (the SPA re-submits every field), so nil limits clear the
// limit. The key VALUE is never part of the body — it is always
// generated server-side so a weak operator-chosen secret can't slip in.
type keyInput struct {
	Name          string   `json:"name"`
	Label         string   `json:"label"`
	Enabled       *bool    `json:"enabled"`
	DailyUSDLimit *float64 `json:"daily_usd_limit"`
	RPMLimit      *int     `json:"rpm_limit"`
	AllowedModels []string `json:"allowed_models"`
}

// normalize trims and validates the shared fields, writing a 400 and
// returning ok=false on a bad request.
func (in *keyInput) normalize(c *gin.Context) bool {
	in.Name = strings.TrimSpace(in.Name)
	in.Label = strings.TrimSpace(in.Label)
	if in.Name == "" {
		writeBadRequest(c, "name is required")
		return false
	}
	if in.RPMLimit != nil && *in.RPMLimit <= 0 {
		writeBadRequest(c, "rpm_limit must be greater than 0 (omit it for no limit)")
		return false
	}
	if in.DailyUSDLimit != nil && *in.DailyUSDLimit < 0 {
		writeBadRequest(c, "daily_usd_limit cannot be negative (omit it for no limit)")
		return false
	}
	// Drop blank model entries so an accidental trailing comma in the UI
	// doesn't persist an empty allow-list member.
	in.AllowedModels = cleanModels(in.AllowedModels)
	return true
}

// ListKeysHandler returns GET /admin/api/keys → {"keys":[…]}.
func ListKeysHandler(store keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys, err := store.ListAll(c.Request.Context())
		if err != nil {
			slog.Error("admin: list keys failed", "err", err.Error())
			writeInternal(c, "could not list keys")
			return
		}
		out := make([]keyDTO, len(keys))
		for i, k := range keys {
			out[i] = toKeyDTO(k)
		}
		c.JSON(http.StatusOK, gin.H{"keys": out})
	}
}

// CreateKeyHandler handles POST /admin/api/keys. It mints a random key,
// stores only its hash, and returns 201 with the cleartext exactly once.
func CreateKeyHandler(store keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in keyInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if !in.normalize(c) {
			return
		}

		cleartext, err := apikey.Generate()
		if err != nil {
			slog.Error("admin: generate key failed", "err", err.Error())
			writeInternal(c, "could not generate key")
			return
		}

		params := repository.ApiKeyInsertParams{
			Name:          in.Name,
			Hash:          apikey.HashOf(cleartext),
			Label:         in.Label,
			Enabled:       valueOr(in.Enabled, true),
			DailyUSDLimit: in.DailyUSDLimit,
			RPMLimit:      in.RPMLimit,
			AllowedModels: in.AllowedModels,
		}

		id, err := store.Insert(c.Request.Context(), params)
		if err != nil {
			if errors.Is(err, repository.ErrApiKeyNameTaken) {
				writeError(c, http.StatusConflict, "name_taken",
					"a key named "+in.Name+" already exists")
				return
			}
			slog.Error("admin: create key failed", "err", err.Error())
			writeInternal(c, "could not create key")
			return
		}

		slog.Info("admin: key created", "id", id, "name", in.Name, "admin", adminActor(c))
		key, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			// The row exists and the cleartext must still reach the
			// operator — fall back to the input metadata rather than
			// dropping the one-time secret on a cosmetic read-back miss.
			slog.Error("admin: read-back after key create failed", "id", id, "err", err.Error())
			c.JSON(http.StatusCreated, createKeyResponse{
				keyDTO: keyDTO{
					ID:            id,
					Name:          in.Name,
					Label:         in.Label,
					Enabled:       params.Enabled,
					DailyUSDLimit: in.DailyUSDLimit,
					RPMLimit:      in.RPMLimit,
					AllowedModels: cleanModels(in.AllowedModels),
				},
				Key: cleartext,
			})
			return
		}
		c.JSON(http.StatusCreated, createKeyResponse{keyDTO: toKeyDTO(key), Key: cleartext})
	}
}

// UpdateKeyHandler handles PATCH /admin/api/keys/:id. The body is a full
// metadata replace; the key value itself is immutable.
func UpdateKeyHandler(store keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in keyInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if !in.normalize(c) {
			return
		}

		params := repository.ApiKeyUpdateParams{
			Name:          in.Name,
			Label:         in.Label,
			Enabled:       valueOr(in.Enabled, true),
			DailyUSDLimit: in.DailyUSDLimit,
			RPMLimit:      in.RPMLimit,
			AllowedModels: in.AllowedModels,
		}

		if err := store.UpdateMeta(c.Request.Context(), id, params); err != nil {
			switch {
			case errors.Is(err, repository.ErrApiKeyNotFound):
				writeError(c, http.StatusNotFound, "not_found", "key not found")
			case errors.Is(err, repository.ErrApiKeyNameTaken):
				writeError(c, http.StatusConflict, "name_taken",
					"a key named "+in.Name+" already exists")
			default:
				slog.Error("admin: update key failed", "id", id, "err", err.Error())
				writeInternal(c, "could not update key")
			}
			return
		}

		key, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			slog.Error("admin: read-back after key update failed", "id", id, "err", err.Error())
			writeInternal(c, "key updated but could not be re-read")
			return
		}
		slog.Info("admin: key updated", "id", id, "name", in.Name, "admin", adminActor(c))
		c.JSON(http.StatusOK, toKeyDTO(key))
	}
}

// DeleteKeyHandler handles DELETE /admin/api/keys/:id → 204.
func DeleteKeyHandler(store keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := store.DeleteByID(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrApiKeyNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "key not found")
				return
			}
			slog.Error("admin: delete key failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete key")
			return
		}
		slog.Info("admin: key deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// cleanModels trims each entry and drops the empties, returning nil when
// nothing survives so the repository stores NULL ("all models").
func cleanModels(in []string) []string {
	var out []string
	for _, m := range in {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}
