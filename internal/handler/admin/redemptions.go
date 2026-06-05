package admin

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// redemptionStore is the slice of repository.RedemptionRepo the admin
// redemption handlers depend on.
type redemptionStore interface {
	GenerateBatch(ctx context.Context, count int, amountUSD float64, expiresAt *time.Time, createdBy string) ([]string, string, error)
	ListBatches(ctx context.Context, limit int) ([]repository.RedemptionBatch, error)
}

type redemptionBatchDTO struct {
	BatchID   string     `json:"batch_id"`
	AmountUSD float64    `json:"amount_usd"`
	Total     int        `json:"total"`
	Redeemed  int        `json:"redeemed"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
}

// ListRedemptionsHandler returns GET /admin/api/redemptions → batch summaries.
func ListRedemptionsHandler(store redemptionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		batches, err := store.ListBatches(c.Request.Context(), 50)
		if err != nil {
			slog.Error("admin: list redemptions failed", "err", err.Error())
			writeInternal(c, "could not list redemption batches")
			return
		}
		out := make([]redemptionBatchDTO, len(batches))
		for i, b := range batches {
			out[i] = redemptionBatchDTO{
				BatchID: b.BatchID, AmountUSD: b.AmountUSD, Total: b.Total,
				Redeemed: b.Redeemed, ExpiresAt: b.ExpiresAt,
				CreatedBy: b.CreatedBy, CreatedAt: b.CreatedAt,
			}
		}
		c.JSON(http.StatusOK, gin.H{"batches": out})
	}
}

// GenerateRedemptionsHandler handles POST /admin/api/redemptions. Body:
// {count, amount_usd, expires_in_days?}. Returns the cleartext codes once.
func GenerateRedemptionsHandler(store redemptionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			Count         int     `json:"count"`
			AmountUSD     float64 `json:"amount_usd"`
			ExpiresInDays int     `json:"expires_in_days"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if in.Count < 1 || in.Count > 1000 {
			writeBadRequest(c, "count must be between 1 and 1000")
			return
		}
		if in.AmountUSD <= 0 {
			writeBadRequest(c, "amount_usd must be greater than 0")
			return
		}
		var expiresAt *time.Time
		if in.ExpiresInDays > 0 {
			t := time.Now().Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)
			expiresAt = &t
		}

		codes, batchID, err := store.GenerateBatch(c.Request.Context(), in.Count, in.AmountUSD, expiresAt, adminActor(c))
		if err != nil {
			slog.Error("admin: generate redemptions failed", "err", err.Error())
			writeInternal(c, "could not generate codes")
			return
		}
		slog.Info("admin: redemption batch generated",
			"batch", batchID, "count", in.Count, "amount", in.AmountUSD, "admin", adminActor(c))
		c.JSON(http.StatusCreated, gin.H{"batch_id": batchID, "amount_usd": in.AmountUSD, "codes": codes})
	}
}
