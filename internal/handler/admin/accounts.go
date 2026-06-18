package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// accountStore is the slice of repository.AccountRepo the account
// handlers depend on. Narrowing it to an interface lets the unit tests
// stand in a fake without a live Postgres connection.
type accountStore interface {
	ListAll(ctx context.Context) ([]*provider.Account, []bool, error)
	GetByID(ctx context.Context, id int64) (*provider.Account, bool, error)
	Insert(ctx context.Context, p repository.InsertParams) (int64, error)
	Update(ctx context.Context, id int64, p repository.UpdateParams) error
	DeleteByID(ctx context.Context, id int64) error
}

// accountDTO is the JSON shape returned to the SPA. It deliberately
// omits credential VALUES — only the key names ride along
// (credential_keys), so the browser learns which secrets are
// configured ("api_key", "aws_region") without ever receiving them.
// Credentials are write-only across the whole admin API.
type accountDTO struct {
	ID                      int64    `json:"id"`
	Name                    string   `json:"name"`
	Provider                string   `json:"provider"`
	Enabled                 bool     `json:"enabled"`
	Weight                  int      `json:"weight"`
	Priority                int      `json:"priority"`
	CostMultiplier          float64  `json:"cost_multiplier"`
	BaseURL                 string   `json:"base_url"`
	CredentialKeys          []string `json:"credential_keys"`
	CircuitFailureThreshold *int     `json:"circuit_failure_threshold"`
	CircuitOpenDurationMs   *int64   `json:"circuit_open_duration_ms"`
	CircuitHalfOpenSuccess  *int     `json:"circuit_half_open_success"`

	ModelRedirects     []provider.ModelRedirect `json:"model_redirects"`
	DailyUSDLimit      *float64                 `json:"daily_usd_limit"`
	TotalUSDLimit      *float64                 `json:"total_usd_limit"`
	GroupID            *int64                   `json:"group_id"`
	GroupName          string                   `json:"group_name"`
	CustomHeaders      map[string]string        `json:"custom_headers"`
	Endpoints          []string                 `json:"endpoints"`
	HealthProbeEnabled *bool                    `json:"health_probe_enabled"`
	ProxyURL           string                   `json:"proxy_url"`
	ParamOverrides     provider.ParamOverrides  `json:"param_overrides"`
	ActiveWindows      []provider.ActiveWindow  `json:"active_windows"`
	ActiveTimezone     string                   `json:"active_timezone"`
	ForwardClientIP    bool                     `json:"forward_client_ip"`

	// AllowedModels is the per-account model allow-list ([] = serve any
	// model). When non-empty, the resolver skips this account for models
	// not in the list.
	AllowedModels []string `json:"allowed_models"`

	// Upstream auth model. auth_type / auth_plugin / client_profile /
	// client_profile_config are admin-configurable; the rest are
	// read-only runtime state maintained by the TokenManager.
	AuthType            string         `json:"auth_type"`
	AuthPlugin          string         `json:"auth_plugin"`
	AuthStatus          string         `json:"auth_status"`
	AuthSubject         string         `json:"auth_subject"`
	AuthEmail           string         `json:"auth_email"`
	AuthPlan            string         `json:"auth_plan"`
	AuthExpiresAt       *time.Time     `json:"auth_expires_at"`
	LastRefreshAt       *time.Time     `json:"last_refresh_at"`
	RefreshError        string         `json:"refresh_error"`
	ClientProfile       string         `json:"client_profile"`
	ClientProfileConfig map[string]any `json:"client_profile_config"`

	// MaxConcurrency caps in-flight requests through the account
	// (0 = unlimited; enforced in-process).
	MaxConcurrency int `json:"max_concurrency"`

	// ProxyID binds the account to a proxies row (migration 0038);
	// null = inline proxy_url / direct.
	ProxyID *int64 `json:"proxy_id"`

	// QuotaWindows is the last-known subscription usage (5h / 7d rolling
	// windows) captured passively from upstream traffic. Populated only
	// in the list response for OAuth accounts; empty otherwise.
	QuotaWindows []provider.QuotaWindow `json:"quota_windows,omitempty"`
}

// quotaSource supplies the latest passively-captured usage windows for an
// account (the accountquota store). nil-safe at the call site.
type quotaSource interface {
	Get(accountID int64) []provider.QuotaWindow
}

// toDTO projects a provider.Account (+ its enabled flag) onto the
// redacted wire shape.
func toDTO(a *provider.Account, enabled bool) accountDTO {
	keys := make([]string, 0, len(a.Credentials))
	for k := range a.Credentials {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable order so the UI doesn't reshuffle per request

	var openMs *int64
	if a.CircuitOpenDuration != nil {
		ms := a.CircuitOpenDuration.Milliseconds()
		openMs = &ms
	}
	redirects := a.ModelRedirects
	if redirects == nil {
		redirects = []provider.ModelRedirect{} // serialise as [] not null
	}
	headers := a.CustomHeaders
	if headers == nil {
		headers = map[string]string{} // serialise as {} not null
	}
	endpoints := a.Endpoints
	if endpoints == nil {
		endpoints = []string{} // serialise as [] not null
	}
	allowedModels := a.AllowedModels
	if allowedModels == nil {
		allowedModels = []string{} // serialise as [] not null
	}
	windows := a.ActiveWindows
	if windows == nil {
		windows = []provider.ActiveWindow{} // serialise as [] not null
	}
	profileConfig := a.ClientProfileConfig
	if profileConfig == nil {
		profileConfig = map[string]any{} // serialise as {} not null
	}
	return accountDTO{
		ID:                      a.ID,
		Name:                    a.Name,
		Provider:                a.Provider,
		Enabled:                 enabled,
		Weight:                  a.Weight,
		Priority:                a.Priority,
		CostMultiplier:          a.CostMultiplier,
		BaseURL:                 a.BaseURL,
		CredentialKeys:          keys,
		CircuitFailureThreshold: a.CircuitFailureThreshold,
		CircuitOpenDurationMs:   openMs,
		CircuitHalfOpenSuccess:  a.CircuitHalfOpenSuccess,
		ModelRedirects:          redirects,
		DailyUSDLimit:           a.DailyUSDLimit,
		TotalUSDLimit:           a.TotalUSDLimit,
		GroupID:                 a.GroupID,
		GroupName:               a.GroupName,
		CustomHeaders:           headers,
		Endpoints:               endpoints,
		HealthProbeEnabled:      a.HealthProbeEnabled,
		ProxyURL:                a.ProxyURL,
		ParamOverrides:          a.ParamOverrides,
		ActiveWindows:           windows,
		ActiveTimezone:          a.ActiveTimezone,
		ForwardClientIP:         a.ForwardClientIP,
		AllowedModels:           allowedModels,
		AuthType:                a.AuthType,
		AuthPlugin:              a.AuthPlugin,
		AuthStatus:              a.AuthStatus,
		AuthSubject:             a.AuthSubject,
		AuthEmail:               a.AuthEmail,
		AuthPlan:                a.AuthPlan,
		AuthExpiresAt:           a.AuthExpiresAt,
		LastRefreshAt:           a.LastRefreshAt,
		RefreshError:            a.RefreshError,
		ClientProfile:           a.ClientProfile,
		ClientProfileConfig:     profileConfig,
		MaxConcurrency:          a.MaxConcurrency,
		ProxyID:                 a.ProxyID,
	}
}

// sanitizeRedirects trims and validates each redirect rule, returning
// the cleaned set or an error message identifying the first bad rule.
func sanitizeRedirects(rules []provider.ModelRedirect) ([]provider.ModelRedirect, string) {
	if len(rules) == 0 {
		return nil, ""
	}
	out := make([]provider.ModelRedirect, 0, len(rules))
	for i, r := range rules {
		r.Source = strings.TrimSpace(r.Source)
		r.Target = strings.TrimSpace(r.Target)
		if !r.Valid() {
			return nil, "model redirect rule " + strconv.Itoa(i+1) +
				" is invalid (check match type, source, target, and regex syntax)"
		}
		out = append(out, r)
	}
	return out, ""
}

// sanitizeAllowedModels trims each model name and drops blanks,
// returning nil when nothing remains (so the column stores "[]" / "no
// restriction"). Duplicates are collapsed to keep the list tidy.
func sanitizeAllowedModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sanitizeHeaders trims header names, drops entries with a blank name,
// and rejects a name containing characters illegal in an HTTP field
// name. Returns the cleaned map (nil when empty) or an error message.
func sanitizeHeaders(h map[string]string) (map[string]string, string) {
	if len(h) == 0 {
		return nil, ""
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		name := strings.TrimSpace(k)
		if name == "" {
			continue
		}
		if strings.ContainsAny(name, " \t\r\n:") {
			return nil, "custom header name " + strconv.Quote(name) + " contains illegal characters"
		}
		out[name] = v
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, ""
}

// sanitizeProxyURL trims the proxy URL and validates its scheme. Empty
// is allowed (direct connection). Unlike base_url it is NOT SSRF-guarded
// against private hosts — a local/internal proxy (e.g. socks5://
// 127.0.0.1:1080) is a legitimate egress setup the operator chooses.
func sanitizeProxyURL(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "proxy URL is not a valid URL"
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", "proxy URL scheme must be http, https, socks5, or socks5h"
	}
	if u.Host == "" {
		return "", "proxy URL has no host"
	}
	return raw, ""
}

// validAuthTypes is the set accepted by the auth_type column (mirrors
// the CHECK constraint in migration 0036).
var validAuthTypes = map[string]bool{
	"api_key":         true,
	"oauth":           true,
	"imported_oauth":  true,
	"service_account": true,
	"adc":             true,
	"worker":          true,
}

// normalizeAuthType trims and validates the requested auth type,
// defaulting an empty value to "api_key" (the historical behaviour).
// Returns the cleaned value or an error message.
func normalizeAuthType(raw string) (string, string) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "api_key", ""
	}
	if !validAuthTypes[t] {
		return "", "auth_type must be one of api_key, oauth, imported_oauth, service_account, adc, worker"
	}
	return t, ""
}

// validateParamOverrides bounds-checks the per-account generation
// overrides. Empty (all-nil) is valid. Returns an error message on a
// nonsensical value.
func validateParamOverrides(p provider.ParamOverrides) string {
	if p.MaxTokens != nil && *p.MaxTokens <= 0 {
		return "param override max_tokens must be greater than 0"
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return "param override temperature must be between 0 and 2"
	}
	if p.TopP != nil && (*p.TopP < 0 || *p.TopP > 1) {
		return "param override top_p must be between 0 and 1"
	}
	if p.ThinkingBudget != nil && *p.ThinkingBudget <= 0 {
		return "param override thinking_budget_tokens must be greater than 0"
	}
	return ""
}

// validateActiveWindows checks each window parses and that the timezone
// (if any) is loadable. Empty windows are valid.
func validateActiveWindows(windows []provider.ActiveWindow, tz string) string {
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return "active_timezone " + strconv.Quote(tz) + " is not a valid IANA timezone"
		}
	}
	for i, w := range windows {
		if !w.Valid() {
			return "active window " + strconv.Itoa(i+1) +
				" is invalid (start/end must be HH:MM, days 0-6)"
		}
	}
	return ""
}

// sanitizeEndpoints trims each additional endpoint URL, drops blanks,
// and validates the rest with the same SSRF guard used for base_url.
// Returns the cleaned list (nil when empty) or an error message.
func sanitizeEndpoints(eps []string) ([]string, string) {
	if len(eps) == 0 {
		return nil, ""
	}
	out := make([]string, 0, len(eps))
	for i, e := range eps {
		u := strings.TrimSpace(e)
		if u == "" {
			continue
		}
		if err := provider.ValidateUpstreamURL(u); err != nil {
			return nil, "endpoint " + strconv.Itoa(i+1) + " rejected: " + err.Error()
		}
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, ""
}

// accountInput is the create/update request body. Numeric defaults are
// applied in the create path only; update is a full replace and the SPA
// always re-submits every field. Credentials are optional on update
// (omit to keep the stored secret) and required on create.
type accountInput struct {
	Name           string            `json:"name"`
	Provider       string            `json:"provider"`
	Enabled        *bool             `json:"enabled"`
	Weight         *int              `json:"weight"`
	Priority       *int              `json:"priority"`
	CostMultiplier *float64          `json:"cost_multiplier"`
	BaseURL        string            `json:"base_url"`
	Credentials    map[string]string `json:"credentials"`

	CircuitFailureThreshold *int   `json:"circuit_failure_threshold"`
	CircuitOpenDurationMs   *int64 `json:"circuit_open_duration_ms"`
	CircuitHalfOpenSuccess  *int   `json:"circuit_half_open_success"`

	ModelRedirects     []provider.ModelRedirect `json:"model_redirects"`
	DailyUSDLimit      *float64                 `json:"daily_usd_limit"`
	TotalUSDLimit      *float64                 `json:"total_usd_limit"`
	GroupID            *int64                   `json:"group_id"`
	CustomHeaders      map[string]string        `json:"custom_headers"`
	Endpoints          []string                 `json:"endpoints"`
	HealthProbeEnabled *bool                    `json:"health_probe_enabled"`
	ProxyURL           string                   `json:"proxy_url"`
	ParamOverrides     provider.ParamOverrides  `json:"param_overrides"`
	ActiveWindows      []provider.ActiveWindow  `json:"active_windows"`
	ActiveTimezone     string                   `json:"active_timezone"`
	ForwardClientIP    bool                     `json:"forward_client_ip"`

	AllowedModels []string `json:"allowed_models"`

	// Upstream auth model (admin-configurable subset only). The runtime
	// columns are never accepted from the client.
	AuthType            string         `json:"auth_type"`
	AuthPlugin          string         `json:"auth_plugin"`
	ClientProfile       string         `json:"client_profile"`
	ClientProfileConfig map[string]any `json:"client_profile_config"`

	MaxConcurrency int    `json:"max_concurrency"`
	ProxyID        *int64 `json:"proxy_id"`
}

// circuitDuration converts the millisecond wire value into the
// *time.Duration the repository expects.
func (in *accountInput) circuitDuration() *time.Duration {
	if in.CircuitOpenDurationMs == nil {
		return nil
	}
	d := time.Duration(*in.CircuitOpenDurationMs) * time.Millisecond
	return &d
}

// ListAccountsHandler returns GET /admin/api/accounts → {"accounts":[…]}.
// quota may be nil; when set, each OAuth account's DTO carries its
// last-known usage windows.
func ListAccountsHandler(store accountStore, quota quotaSource) gin.HandlerFunc {
	return func(c *gin.Context) {
		accounts, enabled, err := store.ListAll(c.Request.Context())
		if err != nil {
			slog.Error("admin: list accounts failed", "err", err.Error())
			writeInternal(c, "could not list accounts")
			return
		}
		out := make([]accountDTO, len(accounts))
		for i, a := range accounts {
			out[i] = toDTO(a, enabled[i])
			if quota != nil && a.AuthType != "api_key" {
				out[i].QuotaWindows = quota.Get(a.ID)
			}
		}
		c.JSON(http.StatusOK, gin.H{"accounts": out})
	}
}

// CreateAccountHandler handles POST /admin/api/accounts. Returns 201
// with the created account (credentials redacted).
func CreateAccountHandler(store accountStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in accountInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Provider = strings.TrimSpace(in.Provider)
		if in.Name == "" || in.Provider == "" {
			writeBadRequest(c, "name and provider are required")
			return
		}
		authType, aerr := normalizeAuthType(in.AuthType)
		if aerr != "" {
			writeBadRequest(c, aerr)
			return
		}
		if in.MaxConcurrency < 0 {
			writeBadRequest(c, "max_concurrency cannot be negative (0 = unlimited)")
			return
		}
		// api_key accounts must ship a secret at create time. oauth /
		// imported_oauth / worker accounts receive their credentials
		// later via the auth plugin (import / login), so an empty set is
		// allowed for them.
		if authType == "api_key" && len(in.Credentials) == 0 {
			writeBadRequest(c, "credentials are required (at least api_key)")
			return
		}
		redirects, rerr := sanitizeRedirects(in.ModelRedirects)
		if rerr != "" {
			writeBadRequest(c, rerr)
			return
		}
		headers, herr := sanitizeHeaders(in.CustomHeaders)
		if herr != "" {
			writeBadRequest(c, herr)
			return
		}
		endpoints, eerr := sanitizeEndpoints(in.Endpoints)
		if eerr != "" {
			writeBadRequest(c, eerr)
			return
		}
		proxyURL, perr := sanitizeProxyURL(in.ProxyURL)
		if perr != "" {
			writeBadRequest(c, perr)
			return
		}
		if verr := validateParamOverrides(in.ParamOverrides); verr != "" {
			writeBadRequest(c, verr)
			return
		}
		if werr := validateActiveWindows(in.ActiveWindows, in.ActiveTimezone); werr != "" {
			writeBadRequest(c, werr)
			return
		}

		params := repository.InsertParams{
			Name:                    in.Name,
			Provider:                in.Provider,
			Enabled:                 valueOr(in.Enabled, true),
			Weight:                  valueOr(in.Weight, 100),
			Priority:                valueOr(in.Priority, 0),
			CostMultiplier:          valueOr(in.CostMultiplier, 1.0),
			BaseURL:                 strings.TrimSpace(in.BaseURL),
			Credentials:             in.Credentials,
			CircuitFailureThreshold: in.CircuitFailureThreshold,
			CircuitOpenDuration:     in.circuitDuration(),
			CircuitHalfOpenSuccess:  in.CircuitHalfOpenSuccess,
			ModelRedirects:          redirects,
			DailyUSDLimit:           in.DailyUSDLimit,
			TotalUSDLimit:           in.TotalUSDLimit,
			GroupID:                 in.GroupID,
			CustomHeaders:           headers,
			Endpoints:               endpoints,
			HealthProbeEnabled:      in.HealthProbeEnabled,
			ProxyURL:                proxyURL,
			ParamOverrides:          in.ParamOverrides,
			ActiveWindows:           in.ActiveWindows,
			ActiveTimezone:          strings.TrimSpace(in.ActiveTimezone),
			ForwardClientIP:         in.ForwardClientIP,
			AllowedModels:           sanitizeAllowedModels(in.AllowedModels),
			AuthType:                authType,
			AuthPlugin:              strings.TrimSpace(in.AuthPlugin),
			ClientProfile:           strings.TrimSpace(in.ClientProfile),
			ClientProfileConfig:     in.ClientProfileConfig,
			MaxConcurrency:          in.MaxConcurrency,
			ProxyID:                 in.ProxyID,
		}

		id, err := store.Insert(c.Request.Context(), params)
		if err != nil {
			if errors.Is(err, repository.ErrAccountNameTaken) {
				writeError(c, http.StatusConflict, "name_taken",
					"an account named "+in.Name+" already exists")
				return
			}
			slog.Error("admin: create account failed", "err", err.Error())
			writeInternal(c, "could not create account")
			return
		}

		acct, enabled, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			// The row exists; a read-back failure is purely cosmetic.
			slog.Error("admin: read-back after create failed", "id", id, "err", err.Error())
			c.JSON(http.StatusCreated, gin.H{"id": id})
			return
		}
		slog.Info("admin: account created", "id", id, "name", in.Name, "admin", adminActor(c))
		c.JSON(http.StatusCreated, toDTO(acct, enabled))
	}
}

// UpdateAccountHandler handles PATCH /admin/api/accounts/:id. The body
// is a full metadata replace; omitting credentials keeps the stored
// secret untouched.
func UpdateAccountHandler(store accountStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in accountInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Provider = strings.TrimSpace(in.Provider)
		if in.Name == "" || in.Provider == "" {
			writeBadRequest(c, "name and provider are required")
			return
		}
		// An empty credentials object would wipe the secret; treat the
		// "didn't touch it" case (nil) as keep, but reject an explicit
		// empty map so the operator can't blank credentials by accident.
		if in.Credentials != nil && len(in.Credentials) == 0 {
			writeBadRequest(c, "credentials cannot be empty; omit the field to keep the existing secret")
			return
		}
		authType, aerr := normalizeAuthType(in.AuthType)
		if aerr != "" {
			writeBadRequest(c, aerr)
			return
		}
		if in.MaxConcurrency < 0 {
			writeBadRequest(c, "max_concurrency cannot be negative (0 = unlimited)")
			return
		}
		redirects, rerr := sanitizeRedirects(in.ModelRedirects)
		if rerr != "" {
			writeBadRequest(c, rerr)
			return
		}
		headers, herr := sanitizeHeaders(in.CustomHeaders)
		if herr != "" {
			writeBadRequest(c, herr)
			return
		}
		endpoints, eerr := sanitizeEndpoints(in.Endpoints)
		if eerr != "" {
			writeBadRequest(c, eerr)
			return
		}
		proxyURL, perr := sanitizeProxyURL(in.ProxyURL)
		if perr != "" {
			writeBadRequest(c, perr)
			return
		}
		if verr := validateParamOverrides(in.ParamOverrides); verr != "" {
			writeBadRequest(c, verr)
			return
		}
		if werr := validateActiveWindows(in.ActiveWindows, in.ActiveTimezone); werr != "" {
			writeBadRequest(c, werr)
			return
		}

		params := repository.UpdateParams{
			Name:                    in.Name,
			Provider:                in.Provider,
			Enabled:                 valueOr(in.Enabled, true),
			Weight:                  valueOr(in.Weight, 100),
			Priority:                valueOr(in.Priority, 0),
			CostMultiplier:          valueOr(in.CostMultiplier, 1.0),
			BaseURL:                 strings.TrimSpace(in.BaseURL),
			Credentials:             in.Credentials,
			CircuitFailureThreshold: in.CircuitFailureThreshold,
			CircuitOpenDuration:     in.circuitDuration(),
			CircuitHalfOpenSuccess:  in.CircuitHalfOpenSuccess,
			ModelRedirects:          redirects,
			DailyUSDLimit:           in.DailyUSDLimit,
			TotalUSDLimit:           in.TotalUSDLimit,
			GroupID:                 in.GroupID,
			CustomHeaders:           headers,
			Endpoints:               endpoints,
			HealthProbeEnabled:      in.HealthProbeEnabled,
			ProxyURL:                proxyURL,
			ParamOverrides:          in.ParamOverrides,
			ActiveWindows:           in.ActiveWindows,
			ActiveTimezone:          strings.TrimSpace(in.ActiveTimezone),
			ForwardClientIP:         in.ForwardClientIP,
			AllowedModels:           sanitizeAllowedModels(in.AllowedModels),
			AuthType:                authType,
			AuthPlugin:              strings.TrimSpace(in.AuthPlugin),
			ClientProfile:           strings.TrimSpace(in.ClientProfile),
			ClientProfileConfig:     in.ClientProfileConfig,
			MaxConcurrency:          in.MaxConcurrency,
			ProxyID:                 in.ProxyID,
		}

		if err := store.Update(c.Request.Context(), id, params); err != nil {
			switch {
			case errors.Is(err, repository.ErrAccountNotFound):
				writeError(c, http.StatusNotFound, "not_found", "account not found")
			case errors.Is(err, repository.ErrAccountNameTaken):
				writeError(c, http.StatusConflict, "name_taken",
					"an account named "+in.Name+" already exists")
			default:
				slog.Error("admin: update account failed", "id", id, "err", err.Error())
				writeInternal(c, "could not update account")
			}
			return
		}

		acct, enabled, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			slog.Error("admin: read-back after update failed", "id", id, "err", err.Error())
			writeInternal(c, "account updated but could not be re-read")
			return
		}
		slog.Info("admin: account updated", "id", id, "name", in.Name, "admin", adminActor(c))
		c.JSON(http.StatusOK, toDTO(acct, enabled))
	}
}

// DeleteAccountHandler handles DELETE /admin/api/accounts/:id → 204.
func DeleteAccountHandler(store accountStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := store.DeleteByID(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrAccountNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "account not found")
				return
			}
			slog.Error("admin: delete account failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete account")
			return
		}
		slog.Info("admin: account deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// parseIDParam reads the :id path segment as an int64, writing a 400 and
// returning ok=false on a malformed value.
func parseIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeBadRequest(c, "invalid account id")
		return 0, false
	}
	return id, true
}

// valueOr dereferences p, falling back to def when p is nil.
func valueOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
