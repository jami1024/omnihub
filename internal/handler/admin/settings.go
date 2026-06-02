package admin

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// settingsStore is the read/write port for the portal policy.
type settingsStore interface {
	Get(ctx context.Context) (repository.PortalSettings, error)
	Update(ctx context.Context, s repository.PortalSettings) error
}

// GetSettingsHandler returns GET /admin/api/settings.
func GetSettingsHandler(store settingsStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		s, err := store.Get(c.Request.Context())
		if err != nil {
			slog.Error("admin: get settings failed", "err", err.Error())
			writeInternal(c, "could not load settings")
			return
		}
		c.JSON(http.StatusOK, s)
	}
}

// UpdateSettingsHandler handles PUT /admin/api/settings — the portal
// policy (signup toggle + per-key limit default/ceiling).
func UpdateSettingsHandler(store settingsStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in repository.PortalSettings
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if in.KeyDailyUSDMax != nil && *in.KeyDailyUSDMax < 0 {
			writeBadRequest(c, "key_daily_usd_max cannot be negative")
			return
		}
		if in.KeyDailyUSDDefault != nil && *in.KeyDailyUSDDefault < 0 {
			writeBadRequest(c, "key_daily_usd_default cannot be negative")
			return
		}
		if in.KeyRPMMax != nil && *in.KeyRPMMax <= 0 {
			writeBadRequest(c, "key_rpm_max must be greater than 0 (omit for no cap)")
			return
		}
		if err := store.Update(c.Request.Context(), in); err != nil {
			slog.Error("admin: update settings failed", "err", err.Error())
			writeInternal(c, "could not save settings")
			return
		}
		slog.Info("admin: portal settings updated", "signup", in.SignupEnabled, "admin", adminActor(c))
		c.JSON(http.StatusOK, in)
	}
}
