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

// walletStore is the credit-side slice of repository.WalletRepo the admin
// wallet handlers depend on.
type walletStore interface {
	AddEntry(ctx context.Context, userID int64, kind string, amountUSD float64, note, createdBy string) error
	Credits(ctx context.Context, userID int64) (float64, error)
	ListEntries(ctx context.Context, userID int64, limit int) ([]repository.WalletEntry, error)
}

// userCostStore returns a user's lifetime billed amount (consumption side,
// after the price ratio).
type userCostStore interface {
	SumBilledByUser(ctx context.Context, userID int64) (float64, error)
}

type walletEntryDTO struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	AmountUSD float64   `json:"amount_usd"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// rechargeKinds is the set of credit kinds an admin may apply.
var rechargeKinds = map[string]bool{"topup": true, "adjust": true, "refund": true}

// userBalance computes credits, lifetime spend, and balance for one user.
func userBalance(ctx context.Context, wallet walletStore, cost userCostStore, userID int64) (credits, spent, balance float64, err error) {
	if credits, err = wallet.Credits(ctx, userID); err != nil {
		return
	}
	if spent, err = cost.SumBilledByUser(ctx, userID); err != nil {
		return
	}
	balance = credits - spent
	return
}

// RechargeUserHandler handles POST /admin/api/users/:id/recharge. Body:
// {amount_usd, note?, kind?}. kind defaults to "topup"; "adjust" may be
// negative. Returns the user's new credits/spent/balance.
func RechargeUserHandler(wallet walletStore, cost userCostStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in struct {
			AmountUSD float64 `json:"amount_usd"`
			Note      string  `json:"note"`
			Kind      string  `json:"kind"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if in.AmountUSD == 0 {
			writeBadRequest(c, "amount_usd must be non-zero")
			return
		}
		kind := in.Kind
		if kind == "" {
			kind = "topup"
		}
		if !rechargeKinds[kind] {
			writeBadRequest(c, "kind must be one of: topup, adjust, refund")
			return
		}
		if kind != "adjust" && in.AmountUSD < 0 {
			writeBadRequest(c, kind+" amount must be positive (use kind=adjust to deduct)")
			return
		}

		if err := wallet.AddEntry(c.Request.Context(), id, kind, in.AmountUSD, in.Note, adminActor(c)); err != nil {
			if errors.Is(err, repository.ErrWalletUserNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "user not found")
				return
			}
			slog.Error("admin: recharge failed", "id", id, "err", err.Error())
			writeInternal(c, "could not apply credit")
			return
		}

		credits, spent, balance, err := userBalance(c.Request.Context(), wallet, cost, id)
		if err != nil {
			slog.Error("admin: balance read after recharge failed", "id", id, "err", err.Error())
			writeInternal(c, "credit applied but balance read failed")
			return
		}
		slog.Info("admin: user recharged", "id", id, "amount", in.AmountUSD, "kind", kind, "admin", adminActor(c))
		c.JSON(http.StatusOK, gin.H{"credits": credits, "spent": spent, "balance": balance})
	}
}

// GetUserWalletHandler handles GET /admin/api/users/:id/wallet → balance +
// ledger history.
func GetUserWalletHandler(wallet walletStore, cost userCostStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		credits, spent, balance, err := userBalance(c.Request.Context(), wallet, cost, id)
		if err != nil {
			slog.Error("admin: wallet read failed", "id", id, "err", err.Error())
			writeInternal(c, "could not load wallet")
			return
		}
		entries, err := wallet.ListEntries(c.Request.Context(), id, 100)
		if err != nil {
			slog.Error("admin: wallet entries failed", "id", id, "err", err.Error())
			writeInternal(c, "could not load wallet")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"credits": credits, "spent": spent, "balance": balance,
			"entries": toWalletEntryDTOs(entries),
		})
	}
}

func toWalletEntryDTOs(entries []repository.WalletEntry) []walletEntryDTO {
	out := make([]walletEntryDTO, len(entries))
	for i, e := range entries {
		out[i] = walletEntryDTO{
			ID: e.ID, Kind: e.Kind, AmountUSD: e.AmountUSD,
			Note: e.Note, CreatedBy: e.CreatedBy, CreatedAt: e.CreatedAt,
		}
	}
	return out
}
