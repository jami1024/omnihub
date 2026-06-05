package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/alert"
	"github.com/jami1024/omnihub/internal/service/alertchannel"
)

// alertChannelStore is the slice of repository.AlertChannelRepo the
// alert-channel handlers depend on, narrowed for testability.
type alertChannelStore interface {
	ListRecords(ctx context.Context) ([]repository.AlertChannelRecord, error)
	Insert(ctx context.Context, p repository.AlertChannelParams, createdBy string) (int64, error)
	Update(ctx context.Context, id int64, p repository.AlertChannelParams) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (alertchannel.Channel, error)
}

// alertChannelTypes is the set of delivery types the UI may create. Kept
// in sync with alert.NotifierFor.
var alertChannelTypes = map[string]bool{"webhook": true, "feishu": true, "dingtalk": true}

// alertChannelDTO is the JSON shape returned to the SPA. The url is never
// returned — it is a write-only secret.
type alertChannelDTO struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

func toAlertChannelDTO(rec repository.AlertChannelRecord) alertChannelDTO {
	return alertChannelDTO{
		ID:        rec.ID,
		Type:      rec.Type,
		Name:      rec.Name,
		Enabled:   rec.Enabled,
		CreatedAt: rec.CreatedAt,
		CreatedBy: rec.CreatedBy,
	}
}

// alertChannelInput is the create/update body. On create the url is
// required; on update an empty url leaves the stored secret untouched.
type alertChannelInput struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled *bool  `json:"enabled"`
}

// validated normalizes the input and validates it. urlRequired is true on
// create. Returns the repository params and false (after writing a 400)
// on any validation error.
func (in *alertChannelInput) validated(c *gin.Context, urlRequired bool) (repository.AlertChannelParams, bool) {
	kind := strings.TrimSpace(in.Type)
	if !alertChannelTypes[kind] {
		writeBadRequest(c, "type must be one of: webhook, feishu, dingtalk")
		return repository.AlertChannelParams{}, false
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeBadRequest(c, "name is required")
		return repository.AlertChannelParams{}, false
	}
	url := strings.TrimSpace(in.URL)
	if urlRequired && url == "" {
		writeBadRequest(c, "url is required")
		return repository.AlertChannelParams{}, false
	}
	if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		writeBadRequest(c, "url must start with http:// or https://")
		return repository.AlertChannelParams{}, false
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	return repository.AlertChannelParams{Type: kind, Name: name, URL: url, Enabled: enabled}, true
}

// ListAlertChannelsHandler returns GET /admin/api/alert-channels.
func ListAlertChannelsHandler(store alertChannelStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		recs, err := store.ListRecords(c.Request.Context())
		if err != nil {
			slog.Error("admin: list alert channels failed", "err", err.Error())
			writeInternal(c, "could not list alert channels")
			return
		}
		out := make([]alertChannelDTO, len(recs))
		for i, rec := range recs {
			out[i] = toAlertChannelDTO(rec)
		}
		c.JSON(http.StatusOK, gin.H{"alert_channels": out})
	}
}

// CreateAlertChannelHandler handles POST /admin/api/alert-channels.
func CreateAlertChannelHandler(store alertChannelStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in alertChannelInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		p, ok := in.validated(c, true)
		if !ok {
			return
		}
		id, err := store.Insert(c.Request.Context(), p, adminActor(c))
		if err != nil {
			slog.Error("admin: create alert channel failed", "err", err.Error())
			writeInternal(c, "could not create alert channel")
			return
		}
		slog.Info("admin: alert channel created", "id", id, "type", p.Type, "admin", adminActor(c))
		c.JSON(http.StatusCreated, gin.H{"id": id})
	}
}

// UpdateAlertChannelHandler handles PATCH /admin/api/alert-channels/:id.
func UpdateAlertChannelHandler(store alertChannelStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseInt64Param(c, "id")
		if !ok {
			return
		}
		var in alertChannelInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		p, ok := in.validated(c, false)
		if !ok {
			return
		}
		if err := store.Update(c.Request.Context(), id, p); err != nil {
			if errors.Is(err, repository.ErrAlertChannelNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "alert channel not found")
				return
			}
			slog.Error("admin: update alert channel failed", "id", id, "err", err.Error())
			writeInternal(c, "could not update alert channel")
			return
		}
		slog.Info("admin: alert channel updated", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// DeleteAlertChannelHandler handles DELETE /admin/api/alert-channels/:id → 204.
func DeleteAlertChannelHandler(store alertChannelStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseInt64Param(c, "id")
		if !ok {
			return
		}
		if err := store.Delete(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrAlertChannelNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "alert channel not found")
				return
			}
			slog.Error("admin: delete alert channel failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete alert channel")
			return
		}
		slog.Info("admin: alert channel deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// TestAlertChannelHandler handles POST /admin/api/alert-channels/:id/test:
// it delivers a synthetic alert through the one channel so an operator can
// confirm the webhook works before relying on it.
func TestAlertChannelHandler(store alertChannelStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseInt64Param(c, "id")
		if !ok {
			return
		}
		ch, err := store.Get(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, repository.ErrAlertChannelNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "alert channel not found")
				return
			}
			slog.Error("admin: get alert channel failed", "id", id, "err", err.Error())
			writeInternal(c, "could not load alert channel")
			return
		}
		notifier, ok := alert.NotifierFor(ch.Type, ch.URL)
		if !ok {
			writeBadRequest(c, "unsupported channel type: "+ch.Type)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if err := notifier.Send(ctx, alert.TestEvent(ch.Name)); err != nil {
			writeError(c, http.StatusBadGateway, "delivery_failed",
				"test delivery failed: "+err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"delivered": true})
	}
}

// parseInt64Param reads a positive integer path segment, writing a 400 on
// a malformed value.
func parseInt64Param(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil || id <= 0 {
		writeBadRequest(c, "invalid "+name+" in path")
		return 0, false
	}
	return id, true
}
