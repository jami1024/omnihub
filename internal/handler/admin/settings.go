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

// gatewaySettingsStore is the read/write port for gateway runtime policy.
type gatewaySettingsStore interface {
	Get(ctx context.Context) (repository.GatewaySettings, error)
	Update(ctx context.Context, s repository.GatewaySettings) error
}

// gatewaySettingsRefresher reloads persisted gateway settings into the
// process-local hot-path snapshot after an admin save.
type gatewaySettingsRefresher interface {
	Refresh(ctx context.Context) error
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
		if in.SignupBonusUSD < 0 || in.SignupBonusUSD > 1000 {
			writeBadRequest(c, "signup_bonus_usd must be between 0 and 1000 (0 disables it)")
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

// GetGatewaySettingsHandler returns GET /admin/api/gateway-settings.
func GetGatewaySettingsHandler(store gatewaySettingsStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		s, err := store.Get(c.Request.Context())
		if err != nil {
			slog.Error("admin: get gateway settings failed", "err", err.Error())
			writeInternal(c, "could not load gateway settings")
			return
		}
		c.JSON(http.StatusOK, s)
	}
}

// UpdateGatewaySettingsHandler handles PUT /admin/api/gateway-settings.
func UpdateGatewaySettingsHandler(store gatewaySettingsStore, live gatewaySettingsRefresher) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in repository.GatewaySettings
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if msg := validateGatewaySettings(in); msg != "" {
			writeBadRequest(c, msg)
			return
		}
		if err := store.Update(c.Request.Context(), in); err != nil {
			slog.Error("admin: update gateway settings failed", "err", err.Error())
			writeInternal(c, "could not save gateway settings")
			return
		}
		if live != nil {
			if err := live.Refresh(c.Request.Context()); err != nil {
				slog.Warn("admin: gateway settings saved but refresh failed", "err", err.Error())
			}
		}
		slog.Info("admin: gateway settings updated", "admin", adminActor(c))
		c.JSON(http.StatusOK, in)
	}
}

func validateGatewaySettings(s repository.GatewaySettings) string {
	switch {
	case s.HealthProbeIntervalMs < 10000:
		return "health_probe_interval_ms must be at least 10000"
	case s.HealthProbeConcurrency < 1 || s.HealthProbeConcurrency > 16:
		return "health_probe_concurrency must be between 1 and 16"
	case s.HealthProbeRedThreshold < 1:
		return "health_probe_red_threshold must be at least 1"
	case s.HealthProbeGreenThreshold < 1:
		return "health_probe_green_threshold must be at least 1"
	case s.HealthProbeTimeoutMs <= 0:
		return "health_probe_timeout_ms must be greater than 0"
	case s.HealthProbeSlowThresholdMs <= 0:
		return "health_probe_slow_threshold_ms must be greater than 0"
	case s.CircuitFailureThreshold < 0:
		return "circuit_failure_threshold cannot be negative"
	case s.CircuitOpenDurationMs <= 0:
		return "circuit_open_duration_ms must be greater than 0"
	case s.CircuitHalfOpenSuccess <= 0:
		return "circuit_half_open_success must be greater than 0"
	case s.FailoverMaxAttempts < 1 || s.FailoverMaxAttempts > 10:
		return "failover_max_attempts must be between 1 and 10"
	default:
		return ""
	}
}
