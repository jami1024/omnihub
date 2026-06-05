package portal

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/guard"
)

// walletStore is the credit-side slice of repository.WalletRepo the portal
// wallet handler needs.
type walletStore interface {
	Credits(ctx context.Context, userID int64) (float64, error)
	ListEntries(ctx context.Context, userID int64, limit int) ([]repository.WalletEntry, error)
}

// costStore returns the user's lifetime request cost.
type costStore interface {
	SumCostByUser(ctx context.Context, userID int64) (float64, error)
}

type walletEntryDTO struct {
	Kind      string    `json:"kind"`
	AmountUSD float64   `json:"amount_usd"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// WalletHandler returns GET /portal/api/wallet — the authenticated user's
// prepaid balance (credits minus lifetime spend) plus recent ledger
// entries. Scoped to the caller's own user id, never another user's.
func WalletHandler(wallet walletStore, cost costStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := guard.UserID(c)

		credits, err := wallet.Credits(c.Request.Context(), uid)
		if err != nil {
			slog.Error("portal: wallet credits failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not load wallet")
			return
		}
		spent, err := cost.SumCostByUser(c.Request.Context(), uid)
		if err != nil {
			slog.Error("portal: wallet spend failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not load wallet")
			return
		}
		entries, err := wallet.ListEntries(c.Request.Context(), uid, 100)
		if err != nil {
			slog.Error("portal: wallet entries failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not load wallet")
			return
		}

		out := make([]walletEntryDTO, len(entries))
		for i, e := range entries {
			out[i] = walletEntryDTO{Kind: e.Kind, AmountUSD: e.AmountUSD, Note: e.Note, CreatedAt: e.CreatedAt}
		}
		c.JSON(http.StatusOK, gin.H{
			"balance": credits - spent,
			"credits": credits,
			"spent":   spent,
			"entries": out,
		})
	}
}
