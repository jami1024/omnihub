package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/guard"
)

// redeemStore is the slice of repository.RedemptionRepo the portal redeem
// handler needs.
type redeemStore interface {
	Redeem(ctx context.Context, code string, userID int64) (float64, error)
}

// balanceCrediter folds a redeemed credit into the in-memory balance cache
// so the user is unblocked immediately. Satisfied by *limits.BalanceGuard;
// nil when billing is off.
type balanceCrediter interface {
	Credit(userID int64, usd float64)
}

// RedeemHandler handles POST /portal/api/redeem {code}: it credits the
// authenticated user's wallet with the code's value. An invalid / used /
// expired code returns 422 with a deliberately generic message.
func RedeemHandler(store redeemStore, crediter balanceCrediter) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := guard.UserID(c)
		var in struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(in.Code) == "" {
			writeBadRequest(c, "code is required")
			return
		}

		amount, err := store.Redeem(c.Request.Context(), in.Code, uid)
		if err != nil {
			if errors.Is(err, repository.ErrRedemptionInvalid) {
				writeError(c, http.StatusUnprocessableEntity, "invalid_code",
					"this code is invalid, already used, or expired")
				return
			}
			slog.Error("portal: redeem failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not redeem code")
			return
		}
		if crediter != nil {
			crediter.Credit(uid, amount)
		}
		slog.Info("portal: code redeemed", "uid", uid, "amount", amount)
		c.JSON(http.StatusOK, gin.H{"credited": amount})
	}
}
