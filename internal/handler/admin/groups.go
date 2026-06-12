package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// groupStore is the slice of repository.ProviderGroupRepo the group
// handlers depend on, narrowed for testability.
type groupStore interface {
	List(ctx context.Context) ([]repository.ProviderGroup, error)
	GetByID(ctx context.Context, id int64) (*repository.ProviderGroup, error)
	Insert(ctx context.Context, p repository.GroupInsertParams) (int64, error)
	Update(ctx context.Context, id int64, p repository.GroupUpdateParams) error
	Delete(ctx context.Context, id int64) error
}

// groupDTO is the JSON shape returned to the SPA.
type groupDTO struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	CostMultiplier float64 `json:"cost_multiplier"`
	Description    string  `json:"description"`
	RoutingPolicy  string  `json:"routing_policy"`
	AccountCount   int     `json:"account_count"`
}

func groupToDTO(g repository.ProviderGroup) groupDTO {
	return groupDTO{
		ID:             g.ID,
		Name:           g.Name,
		CostMultiplier: g.CostMultiplier,
		Description:    g.Description,
		RoutingPolicy:  g.RoutingPolicy,
		AccountCount:   g.AccountCount,
	}
}

// groupInput is the create/update body. CostMultiplier defaults to 1.0
// when omitted.
type groupInput struct {
	Name           string   `json:"name"`
	CostMultiplier *float64 `json:"cost_multiplier"`
	Description    string   `json:"description"`
	RoutingPolicy  string   `json:"routing_policy"`
}

// normalizeRoutingPolicy validates the requested routing policy,
// defaulting empty to weighted_random (mirrors the CHECK constraint in
// migration 0037). Returns the cleaned value or an error message.
func normalizeRoutingPolicy(raw string) (string, string) {
	p := strings.TrimSpace(raw)
	switch p {
	case "":
		return "weighted_random", ""
	case "weighted_random", "round_robin":
		return p, ""
	}
	return "", "routing_policy must be weighted_random or round_robin"
}

// ListGroupsHandler returns GET /admin/api/groups → {"groups":[…]}.
func ListGroupsHandler(store groupStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		groups, err := store.List(c.Request.Context())
		if err != nil {
			slog.Error("admin: list groups failed", "err", err.Error())
			writeInternal(c, "could not list groups")
			return
		}
		out := make([]groupDTO, len(groups))
		for i, g := range groups {
			out[i] = groupToDTO(g)
		}
		c.JSON(http.StatusOK, gin.H{"groups": out})
	}
}

// CreateGroupHandler handles POST /admin/api/groups.
func CreateGroupHandler(store groupStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in groupInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			writeBadRequest(c, "name is required")
			return
		}
		mult := valueOr(in.CostMultiplier, 1.0)
		if mult < 0 {
			writeBadRequest(c, "cost_multiplier cannot be negative")
			return
		}
		policy, perr := normalizeRoutingPolicy(in.RoutingPolicy)
		if perr != "" {
			writeBadRequest(c, perr)
			return
		}
		id, err := store.Insert(c.Request.Context(), repository.GroupInsertParams{
			Name:           in.Name,
			CostMultiplier: mult,
			Description:    strings.TrimSpace(in.Description),
			RoutingPolicy:  policy,
		})
		if err != nil {
			if errors.Is(err, repository.ErrGroupNameTaken) {
				writeError(c, http.StatusConflict, "name_taken",
					"a group named "+in.Name+" already exists")
				return
			}
			slog.Error("admin: create group failed", "err", err.Error())
			writeInternal(c, "could not create group")
			return
		}
		g, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusCreated, gin.H{"id": id})
			return
		}
		slog.Info("admin: group created", "id", id, "name", in.Name, "admin", adminActor(c))
		c.JSON(http.StatusCreated, groupToDTO(*g))
	}
}

// UpdateGroupHandler handles PATCH /admin/api/groups/:id.
func UpdateGroupHandler(store groupStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in groupInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			writeBadRequest(c, "name is required")
			return
		}
		mult := valueOr(in.CostMultiplier, 1.0)
		if mult < 0 {
			writeBadRequest(c, "cost_multiplier cannot be negative")
			return
		}
		policy, perr := normalizeRoutingPolicy(in.RoutingPolicy)
		if perr != "" {
			writeBadRequest(c, perr)
			return
		}
		err := store.Update(c.Request.Context(), id, repository.GroupUpdateParams{
			Name:           in.Name,
			CostMultiplier: mult,
			Description:    strings.TrimSpace(in.Description),
			RoutingPolicy:  policy,
		})
		if err != nil {
			switch {
			case errors.Is(err, repository.ErrGroupNotFound):
				writeError(c, http.StatusNotFound, "not_found", "group not found")
			case errors.Is(err, repository.ErrGroupNameTaken):
				writeError(c, http.StatusConflict, "name_taken",
					"a group named "+in.Name+" already exists")
			default:
				slog.Error("admin: update group failed", "id", id, "err", err.Error())
				writeInternal(c, "could not update group")
			}
			return
		}
		g, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			writeInternal(c, "group updated but could not be re-read")
			return
		}
		slog.Info("admin: group updated", "id", id, "name", in.Name, "admin", adminActor(c))
		c.JSON(http.StatusOK, groupToDTO(*g))
	}
}

// DeleteGroupHandler handles DELETE /admin/api/groups/:id → 204.
func DeleteGroupHandler(store groupStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := store.Delete(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrGroupNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "group not found")
				return
			}
			slog.Error("admin: delete group failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete group")
			return
		}
		slog.Info("admin: group deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}
