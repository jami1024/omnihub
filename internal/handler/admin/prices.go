package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// priceStore is the slice of repository.ModelPriceRepo the price
// handlers depend on, narrowed for testability.
type priceStore interface {
	ListAll(ctx context.Context) ([]repository.ModelPrice, error)
	GetByID(ctx context.Context, id int64) (repository.ModelPrice, error)
	InsertManual(ctx context.Context, model string, p repository.ModelPriceParams) (int64, error)
	UpdateManual(ctx context.Context, id int64, p repository.ModelPriceParams) error
	DeleteByID(ctx context.Context, id int64) error
}

// priceSyncer triggers a LiteLLM sync. nil when the gateway runs without
// a database.
type priceSyncer interface {
	SyncFromLiteLLM(ctx context.Context, url string) (repository.UpsertResult, error)
}

// priceDTO is the wire shape. Costs are USD per token (the canonical
// LiteLLM shape); the UI converts to per-million for display. `source`
// is "litellm" (synced) or "manual" (operator override).
type priceDTO struct {
	ID                                  int64     `json:"id"`
	Model                               string    `json:"model"`
	InputCostPerToken                   float64   `json:"input_cost_per_token"`
	OutputCostPerToken                  float64   `json:"output_cost_per_token"`
	CacheCreationInputTokenCost         float64   `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostAbove1Hr float64   `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             float64   `json:"cache_read_input_token_cost"`
	Source                              string    `json:"source"`
	UpdatedAt                           time.Time `json:"updated_at"`
}

func toPriceDTO(m repository.ModelPrice) priceDTO {
	return priceDTO{
		ID:                                  m.ID,
		Model:                               m.Model,
		InputCostPerToken:                   m.InputCostPerToken,
		OutputCostPerToken:                  m.OutputCostPerToken,
		CacheCreationInputTokenCost:         m.CacheCreationInputTokenCost,
		CacheCreationInputTokenCostAbove1Hr: m.CacheCreationInputTokenCostAbove1Hr,
		CacheReadInputTokenCost:             m.CacheReadInputTokenCost,
		Source:                              m.Source,
		UpdatedAt:                           m.UpdatedAt,
	}
}

// priceInput is the create/update body. `model` is required on create
// (ignored on update — the key is immutable). All costs are USD/token.
type priceInput struct {
	Model                               string  `json:"model"`
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostAbove1Hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
}

func (in *priceInput) params() repository.ModelPriceParams {
	return repository.ModelPriceParams{
		InputCostPerToken:                   in.InputCostPerToken,
		OutputCostPerToken:                  in.OutputCostPerToken,
		CacheCreationInputTokenCost:         in.CacheCreationInputTokenCost,
		CacheCreationInputTokenCostAbove1Hr: in.CacheCreationInputTokenCostAbove1Hr,
		CacheReadInputTokenCost:             in.CacheReadInputTokenCost,
	}
}

// negativeCost reports the first negative field (costs can't be < 0).
func (in *priceInput) negativeCost() bool {
	return in.InputCostPerToken < 0 || in.OutputCostPerToken < 0 ||
		in.CacheCreationInputTokenCost < 0 || in.CacheCreationInputTokenCostAbove1Hr < 0 ||
		in.CacheReadInputTokenCost < 0
}

// ListPricesHandler returns GET /admin/api/prices → {"prices":[…]}.
func ListPricesHandler(store priceStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := store.ListAll(c.Request.Context())
		if err != nil {
			slog.Error("admin: list prices failed", "err", err.Error())
			writeInternal(c, "could not list prices")
			return
		}
		out := make([]priceDTO, len(rows))
		for i, m := range rows {
			out[i] = toPriceDTO(m)
		}
		c.JSON(http.StatusOK, gin.H{"prices": out})
	}
}

// CreatePriceHandler handles POST /admin/api/prices (source = manual).
func CreatePriceHandler(store priceStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in priceInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Model = strings.TrimSpace(in.Model)
		if in.Model == "" {
			writeBadRequest(c, "model is required")
			return
		}
		if in.negativeCost() {
			writeBadRequest(c, "costs cannot be negative")
			return
		}
		id, err := store.InsertManual(c.Request.Context(), in.Model, in.params())
		if err != nil {
			if errors.Is(err, repository.ErrModelPriceExists) {
				writeError(c, http.StatusConflict, "already_exists",
					"a price for "+in.Model+" already exists; edit it instead")
				return
			}
			slog.Error("admin: create price failed", "err", err.Error())
			writeInternal(c, "could not create price")
			return
		}
		m, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusCreated, gin.H{"id": id})
			return
		}
		slog.Info("admin: price created", "id", id, "model", in.Model, "admin", adminActor(c))
		c.JSON(http.StatusCreated, toPriceDTO(m))
	}
}

// UpdatePriceHandler handles PATCH /admin/api/prices/:id. Editing a row
// stamps it 'manual', so a later sync won't overwrite it.
func UpdatePriceHandler(store priceStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in priceInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if in.negativeCost() {
			writeBadRequest(c, "costs cannot be negative")
			return
		}
		if err := store.UpdateManual(c.Request.Context(), id, in.params()); err != nil {
			if errors.Is(err, repository.ErrModelPriceNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "price not found")
				return
			}
			slog.Error("admin: update price failed", "id", id, "err", err.Error())
			writeInternal(c, "could not update price")
			return
		}
		m, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			writeInternal(c, "price updated but could not be re-read")
			return
		}
		slog.Info("admin: price updated", "id", id, "model", m.Model, "admin", adminActor(c))
		c.JSON(http.StatusOK, toPriceDTO(m))
	}
}

// DeletePriceHandler handles DELETE /admin/api/prices/:id → 204.
func DeletePriceHandler(store priceStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := store.DeleteByID(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrModelPriceNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "price not found")
				return
			}
			slog.Error("admin: delete price failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete price")
			return
		}
		slog.Info("admin: price deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// SyncPricesHandler handles POST /admin/api/prices/sync — pull the
// latest LiteLLM price list. Manual overrides are preserved.
func SyncPricesHandler(syncer priceSyncer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if syncer == nil {
			writeError(c, http.StatusServiceUnavailable, "unavailable",
				"price sync is unavailable (no database configured)")
			return
		}
		// The fetch + upsert can take a few seconds; give it its own
		// timeout independent of the request context.
		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()
		res, err := syncer.SyncFromLiteLLM(ctx, "")
		if err != nil {
			slog.Error("admin: price sync failed", "err", err.Error())
			writeError(c, http.StatusBadGateway, "sync_failed", "price sync failed: "+err.Error())
			return
		}
		slog.Info("admin: prices synced", "added", res.Added, "updated", res.Updated,
			"skipped", res.Skipped, "admin", adminActor(c))
		c.JSON(http.StatusOK, gin.H{
			"added": res.Added, "updated": res.Updated, "skipped": res.Skipped,
		})
	}
}
