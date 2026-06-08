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

type planStore interface {
	ListPlans(ctx context.Context) ([]repository.Plan, error)
	CreatePlan(ctx context.Context, p repository.Plan) (int64, error)
	UpdatePlan(ctx context.Context, id int64, p repository.Plan) error
	GrantPlanToUser(ctx context.Context, userID, planID int64, startsAt time.Time) (int64, error)
}

func ListPlansHandler(store planStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := store.ListPlans(c.Request.Context())
		if err != nil {
			slog.Error("admin: list plans failed", "err", err.Error())
			writeInternal(c, "could not list plans")
			return
		}
		c.JSON(http.StatusOK, gin.H{"plans": rows})
	}
}

func CreatePlanHandler(store planStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in repository.Plan
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if err := repository.ValidatePlan(in); err != nil {
			writeBadRequest(c, err.Error())
			return
		}
		id, err := store.CreatePlan(c.Request.Context(), in)
		if err != nil {
			slog.Error("admin: create plan failed", "err", err.Error())
			writeInternal(c, "could not create plan")
			return
		}
		slog.Info("admin: plan created", "id", id, "admin", adminActor(c))
		c.JSON(http.StatusCreated, gin.H{"id": id})
	}
}

func UpdatePlanHandler(store planStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in repository.Plan
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if err := repository.ValidatePlan(in); err != nil {
			writeBadRequest(c, err.Error())
			return
		}
		if err := store.UpdatePlan(c.Request.Context(), id, in); err != nil {
			if errors.Is(err, repository.ErrPlanNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "plan not found")
				return
			}
			slog.Error("admin: update plan failed", "id", id, "err", err.Error())
			writeInternal(c, "could not update plan")
			return
		}
		slog.Info("admin: plan updated", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

func GrantPlanToUserHandler(store planStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in struct {
			PlanID   int64      `json:"plan_id"`
			StartsAt *time.Time `json:"starts_at"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if in.PlanID <= 0 {
			writeBadRequest(c, "plan_id is required")
			return
		}
		startsAt := time.Now().UTC()
		if in.StartsAt != nil {
			startsAt = *in.StartsAt
		}
		id, err := store.GrantPlanToUser(c.Request.Context(), userID, in.PlanID, startsAt)
		if err != nil {
			switch {
			case errors.Is(err, repository.ErrPlanNotFound):
				writeError(c, http.StatusNotFound, "not_found", "plan not found")
			case errors.Is(err, repository.ErrUserNotFound):
				writeError(c, http.StatusNotFound, "not_found", "user not found")
			default:
				slog.Error("admin: grant plan failed", "user", userID, "plan", in.PlanID, "err", err.Error())
				writeInternal(c, "could not grant plan")
			}
			return
		}
		slog.Info("admin: plan granted", "grant", id, "user", userID, "plan", in.PlanID, "admin", adminActor(c))
		c.JSON(http.StatusCreated, gin.H{"id": id})
	}
}
