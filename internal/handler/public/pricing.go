// Package public serves unauthenticated read-only endpoints used by the
// marketing landing page. Keep the payloads intentionally small: these routes
// are not admin APIs and must not expose management-only fields.
package public

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

type planStore interface {
	ListEnabledPlans(ctx context.Context) ([]repository.Plan, error)
}

type priceStore interface {
	ListAll(ctx context.Context) ([]repository.ModelPrice, error)
}

type publicPriceDTO struct {
	Model                               string    `json:"model"`
	InputCostPerToken                   float64   `json:"input_cost_per_token"`
	OutputCostPerToken                  float64   `json:"output_cost_per_token"`
	CacheCreationInputTokenCost         float64   `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostAbove1Hr float64   `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             float64   `json:"cache_read_input_token_cost"`
	Source                              string    `json:"source"`
	UpdatedAt                           time.Time `json:"updated_at"`
}

var preferredPublicPriceModels = []string{
	"claude-sonnet-4-5",
	"claude-opus-4-1",
	"claude-haiku-4-5",
	"gpt-4o",
	"gpt-4o-mini",
	"gpt-4.1",
	"gpt-4.1-mini",
}

const publicPriceFallbackLimit = 8

// PricingHandler returns GET /public/api/pricing → {"plans":[…],"prices":[…]}.
// Plans are enabled plans only. Prices are a compact set of common official
// models when available, otherwise the first few usable rows from the synced
// price table so the landing page still has honest data to render.
func PricingHandler(plans planStore, prices priceStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		planRows, err := plans.ListEnabledPlans(c.Request.Context())
		if err != nil {
			slog.Error("public: list plans failed", "err", err.Error())
			writeInternal(c, "could not load pricing")
			return
		}
		priceRows, err := prices.ListAll(c.Request.Context())
		if err != nil {
			slog.Error("public: list prices failed", "err", err.Error())
			writeInternal(c, "could not load pricing")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"plans":  planRows,
			"prices": selectPublicPrices(priceRows),
		})
	}
}

func selectPublicPrices(rows []repository.ModelPrice) []publicPriceDTO {
	usable := make(map[string]repository.ModelPrice, len(rows))
	for _, row := range rows {
		if row.Model == "" || row.InputCostPerToken <= 0 || row.OutputCostPerToken <= 0 {
			continue
		}
		usable[row.Model] = row
	}

	out := make([]publicPriceDTO, 0, len(preferredPublicPriceModels))
	for _, model := range preferredPublicPriceModels {
		if row, ok := usable[model]; ok {
			out = append(out, toPublicPriceDTO(row))
		}
	}
	if len(out) > 0 {
		return out
	}

	fallback := make([]repository.ModelPrice, 0, len(usable))
	for _, row := range usable {
		fallback = append(fallback, row)
	}
	sort.Slice(fallback, func(i, j int) bool { return fallback[i].Model < fallback[j].Model })
	if len(fallback) > publicPriceFallbackLimit {
		fallback = fallback[:publicPriceFallbackLimit]
	}
	for _, row := range fallback {
		out = append(out, toPublicPriceDTO(row))
	}
	return out
}

func toPublicPriceDTO(m repository.ModelPrice) publicPriceDTO {
	return publicPriceDTO{
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

func writeInternal(c *gin.Context, msg string) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": msg,
			"type":    "internal_error",
			"code":    "internal_error",
		},
	})
}
