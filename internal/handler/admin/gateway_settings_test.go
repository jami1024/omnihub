package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
)

type fakeGatewaySettingsStore struct {
	settings   repository.GatewaySettings
	getErr     error
	updateErr  error
	lastUpdate repository.GatewaySettings
}

func (f *fakeGatewaySettingsStore) Get(context.Context) (repository.GatewaySettings, error) {
	return f.settings, f.getErr
}

func (f *fakeGatewaySettingsStore) Update(_ context.Context, s repository.GatewaySettings) error {
	f.lastUpdate = s
	return f.updateErr
}

type fakeGatewaySettingsRefresher struct {
	calls int
	err   error
}

func (f *fakeGatewaySettingsRefresher) Refresh(context.Context) error {
	f.calls++
	return f.err
}

func newGatewaySettingsEngine(store *fakeGatewaySettingsStore, live *fakeGatewaySettingsRefresher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/gateway-settings", handler.GetGatewaySettingsHandler(store))
	r.PUT("/admin/api/gateway-settings", handler.UpdateGatewaySettingsHandler(store, live))
	return r
}

func TestGetGatewaySettingsReturnsCurrentValues(t *testing.T) {
	store := &fakeGatewaySettingsStore{settings: repository.GatewaySettings{
		HealthProbeEnabled:         true,
		HealthProbeIntervalMs:      120000,
		HealthProbeConcurrency:     3,
		HealthProbeRedThreshold:    4,
		HealthProbeGreenThreshold:  2,
		HealthProbeTimeoutMs:       8000,
		HealthProbeSlowThresholdMs: 1500,
		CircuitFailureThreshold:    6,
		CircuitOpenDurationMs:      45000,
		CircuitHalfOpenSuccess:     2,
		FailoverMaxAttempts:        4,
	}}
	rec := do(newGatewaySettingsEngine(store, nil), http.MethodGet, "/admin/api/gateway-settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got repository.GatewaySettings
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.FailoverMaxAttempts != 4 || got.HealthProbeConcurrency != 3 || !got.HealthProbeEnabled {
		t.Fatalf("unexpected settings: %+v", got)
	}
}

func TestUpdateGatewaySettingsPersistsAndRefreshes(t *testing.T) {
	store := &fakeGatewaySettingsStore{}
	live := &fakeGatewaySettingsRefresher{}
	body := `{
		"health_probe_enabled": true,
		"health_probe_interval_ms": 60000,
		"health_probe_concurrency": 5,
		"health_probe_red_threshold": 3,
		"health_probe_green_threshold": 1,
		"health_probe_timeout_ms": 12000,
		"health_probe_slow_threshold_ms": 2500,
		"circuit_failure_threshold": 5,
		"circuit_open_duration_ms": 30000,
		"circuit_half_open_success": 1,
		"failover_max_attempts": 3
	}`
	rec := do(newGatewaySettingsEngine(store, live), http.MethodPut, "/admin/api/gateway-settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.lastUpdate.HealthProbeConcurrency != 5 || store.lastUpdate.FailoverMaxAttempts != 3 {
		t.Fatalf("settings were not persisted as submitted: %+v", store.lastUpdate)
	}
	if live.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", live.calls)
	}
}

func TestUpdateGatewaySettingsRejectsUnsafeProbeInterval(t *testing.T) {
	body := `{
		"health_probe_enabled": true,
		"health_probe_interval_ms": 9999,
		"health_probe_concurrency": 4,
		"health_probe_red_threshold": 3,
		"health_probe_green_threshold": 1,
		"health_probe_timeout_ms": 12000,
		"health_probe_slow_threshold_ms": 2500,
		"circuit_failure_threshold": 5,
		"circuit_open_duration_ms": 30000,
		"circuit_half_open_success": 1,
		"failover_max_attempts": 3
	}`
	rec := do(newGatewaySettingsEngine(&fakeGatewaySettingsStore{}, nil), http.MethodPut, "/admin/api/gateway-settings", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateGatewaySettingsRejectsTooManyFailoverAttempts(t *testing.T) {
	body := `{
		"health_probe_enabled": false,
		"health_probe_interval_ms": 60000,
		"health_probe_concurrency": 4,
		"health_probe_red_threshold": 3,
		"health_probe_green_threshold": 1,
		"health_probe_timeout_ms": 12000,
		"health_probe_slow_threshold_ms": 2500,
		"circuit_failure_threshold": 5,
		"circuit_open_duration_ms": 30000,
		"circuit_half_open_success": 1,
		"failover_max_attempts": 11
	}`
	rec := do(newGatewaySettingsEngine(&fakeGatewaySettingsStore{}, nil), http.MethodPut, "/admin/api/gateway-settings", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
