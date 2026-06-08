package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/guard"
)

type planStore interface {
	ListEnabledPlans(ctx context.Context) ([]repository.Plan, error)
	ActiveGrantForUser(ctx context.Context, userID int64, now time.Time) (*repository.UserPlanGrant, error)
	GetPlan(ctx context.Context, planID int64) (*repository.Plan, error)
	GrantPlanToUser(ctx context.Context, userID, planID int64, startsAt time.Time) (int64, error)
}

func PlansHandler(store planStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := store.ListEnabledPlans(c.Request.Context())
		if err != nil {
			slog.Error("portal: list plans failed", "err", err.Error())
			writeInternal(c, "could not list plans")
			return
		}
		c.JSON(http.StatusOK, gin.H{"plans": rows})
	}
}

func CurrentPlanHandler(store planStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		grant, err := store.ActiveGrantForUser(c.Request.Context(), guard.UserID(c), time.Now().UTC())
		if err != nil {
			slog.Error("portal: current plan failed", "err", err.Error())
			writeInternal(c, "could not load current plan")
			return
		}
		c.JSON(http.StatusOK, gin.H{"grant": grant})
	}
}

func ClaimPlanHandler(store planStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		planID, ok := parseIDParam(c)
		if !ok {
			return
		}
		plan, err := store.GetPlan(c.Request.Context(), planID)
		if err != nil {
			if errors.Is(err, repository.ErrPlanNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "plan not found")
				return
			}
			slog.Error("portal: get plan failed", "id", planID, "err", err.Error())
			writeInternal(c, "could not load plan")
			return
		}
		if !plan.Enabled {
			writeError(c, http.StatusNotFound, "not_found", "plan not found")
			return
		}
		if plan.PriceUSD > 0 {
			writeBadRequest(c, "paid plans require admin assignment")
			return
		}
		id, err := store.GrantPlanToUser(c.Request.Context(), guard.UserID(c), plan.ID, time.Now().UTC())
		if err != nil {
			slog.Error("portal: claim plan failed", "plan", plan.ID, "err", err.Error())
			writeInternal(c, "could not claim plan")
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": id})
	}
}
