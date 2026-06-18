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
	"github.com/jami1024/omnihub/internal/service/provider"
)

// Account backup format. Export produces this shape; import consumes
// it (extra fields like exported_at are ignored on import).
const (
	accountsExportType    = "omnihub-accounts"
	accountsExportVersion = 1
)

// exportedAccount is the round-trippable projection of an account used
// for admin backup/restore. Unlike accountDTO it carries the DECRYPTED
// credentials in cleartext — this is the whole point of an explicit
// operator backup. The group is referenced by NAME (not id) so a bundle
// restores onto another instance whose group ids differ. Runtime auth
// state (auth_status / *_at / refresh_error) is intentionally omitted:
// the TokenManager re-establishes it from the imported tokens.
type exportedAccount struct {
	Name                    string                   `json:"name"`
	Provider                string                   `json:"provider"`
	Enabled                 bool                     `json:"enabled"`
	Weight                  int                      `json:"weight"`
	Priority                int                      `json:"priority"`
	CostMultiplier          float64                  `json:"cost_multiplier"`
	BaseURL                 string                   `json:"base_url"`
	Credentials             map[string]string        `json:"credentials"`
	CircuitFailureThreshold *int                     `json:"circuit_failure_threshold"`
	CircuitOpenDurationMs   *int64                   `json:"circuit_open_duration_ms"`
	CircuitHalfOpenSuccess  *int                     `json:"circuit_half_open_success"`
	ModelRedirects          []provider.ModelRedirect `json:"model_redirects"`
	DailyUSDLimit           *float64                 `json:"daily_usd_limit"`
	TotalUSDLimit           *float64                 `json:"total_usd_limit"`
	GroupName               string                   `json:"group_name"`
	CustomHeaders           map[string]string        `json:"custom_headers"`
	Endpoints               []string                 `json:"endpoints"`
	HealthProbeEnabled      *bool                    `json:"health_probe_enabled"`
	ProxyURL                string                   `json:"proxy_url"`
	ParamOverrides          provider.ParamOverrides  `json:"param_overrides"`
	ActiveWindows           []provider.ActiveWindow  `json:"active_windows"`
	ActiveTimezone          string                   `json:"active_timezone"`
	ForwardClientIP         bool                     `json:"forward_client_ip"`
	AllowedModels           []string                 `json:"allowed_models"`
	AuthType                string                   `json:"auth_type"`
	AuthPlugin              string                   `json:"auth_plugin"`
	ClientProfile           string                   `json:"client_profile"`
	ClientProfileConfig     map[string]any           `json:"client_profile_config"`
	MaxConcurrency          int                      `json:"max_concurrency"`
}

// accountsBundle is the export envelope.
type accountsBundle struct {
	Type       string            `json:"type"`
	Version    int               `json:"version"`
	ExportedAt string            `json:"exported_at"`
	Accounts   []exportedAccount `json:"accounts"`
}

func toExported(a *provider.Account) exportedAccount {
	var openMs *int64
	if a.CircuitOpenDuration != nil {
		ms := a.CircuitOpenDuration.Milliseconds()
		openMs = &ms
	}
	return exportedAccount{
		Name:                    a.Name,
		Provider:                a.Provider,
		Enabled:                 true, // overwritten by caller from the enabled slice
		Weight:                  a.Weight,
		Priority:                a.Priority,
		CostMultiplier:          a.CostMultiplier,
		BaseURL:                 a.BaseURL,
		Credentials:             a.Credentials,
		CircuitFailureThreshold: a.CircuitFailureThreshold,
		CircuitOpenDurationMs:   openMs,
		CircuitHalfOpenSuccess:  a.CircuitHalfOpenSuccess,
		ModelRedirects:          a.ModelRedirects,
		DailyUSDLimit:           a.DailyUSDLimit,
		TotalUSDLimit:           a.TotalUSDLimit,
		GroupName:               a.GroupName,
		CustomHeaders:           a.CustomHeaders,
		Endpoints:               a.Endpoints,
		HealthProbeEnabled:      a.HealthProbeEnabled,
		ProxyURL:                a.ProxyURL,
		ParamOverrides:          a.ParamOverrides,
		ActiveWindows:           a.ActiveWindows,
		ActiveTimezone:          a.ActiveTimezone,
		ForwardClientIP:         a.ForwardClientIP,
		AllowedModels:           a.AllowedModels,
		AuthType:                a.AuthType,
		AuthPlugin:              a.AuthPlugin,
		ClientProfile:           a.ClientProfile,
		ClientProfileConfig:     a.ClientProfileConfig,
		MaxConcurrency:          a.MaxConcurrency,
	}
}

// ExportAccountsHandler handles GET /admin/api/accounts/export — an
// operator backup of every account (or a subset via ?ids=1,2,3),
// credentials in cleartext. The response is an attachment so a browser
// downloads it as a file.
func ExportAccountsHandler(store accountStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		accounts, enabled, err := store.ListAll(c.Request.Context())
		if err != nil {
			slog.Error("admin: export accounts failed", "err", err.Error())
			writeInternal(c, "could not list accounts")
			return
		}
		idFilter := parseIDFilter(c.Query("ids"))

		out := make([]exportedAccount, 0, len(accounts))
		for i, a := range accounts {
			if idFilter != nil && !idFilter[a.ID] {
				continue
			}
			ea := toExported(a)
			ea.Enabled = enabled[i]
			out = append(out, ea)
		}

		slog.Info("admin: accounts exported", "count", len(out), "admin", adminActor(c))
		c.Header("Content-Disposition", `attachment; filename="omnihub-accounts.json"`)
		c.JSON(http.StatusOK, accountsBundle{
			Type:       accountsExportType,
			Version:    accountsExportVersion,
			ExportedAt: time.Now().UTC().Format(time.RFC3339),
			Accounts:   out,
		})
	}
}

// groupLister is the slice of the provider-group repository the import
// handler needs to resolve group names to ids.
type groupLister interface {
	List(ctx context.Context) ([]repository.ProviderGroup, error)
}

// importAccountsRequest is the import body: the bundle's accounts plus a
// conflict policy. The bundle's type/version/exported_at fields are
// accepted and ignored (the client can post the export file verbatim
// with on_conflict added).
type importAccountsRequest struct {
	Accounts   []exportedAccount `json:"accounts"`
	OnConflict string            `json:"on_conflict"` // "skip" (default) | "fail"
}

type importAccountError struct {
	Kind    string `json:"kind"` // "account" | "warning"
	Name    string `json:"name"`
	Message string `json:"message"`
}

type importAccountsResult struct {
	Created int                  `json:"created"`
	Skipped int                  `json:"skipped"`
	Failed  int                  `json:"failed"`
	Errors  []importAccountError `json:"errors,omitempty"`
}

// ImportAccountsHandler handles POST /admin/api/accounts/import. Each
// account is inserted independently; a duplicate name is skipped (or
// failed, per on_conflict) without aborting the batch, and a missing
// group name downgrades the account to ungrouped with a warning rather
// than failing it. The result reports per-account outcomes.
func ImportAccountsHandler(store accountStore, groups groupLister) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in importAccountsRequest
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if len(in.Accounts) == 0 {
			writeBadRequest(c, "no accounts in the bundle")
			return
		}
		failOnConflict := strings.TrimSpace(in.OnConflict) == "fail"

		// Resolve group names → ids once.
		groupID := map[string]int64{}
		if grps, err := groups.List(c.Request.Context()); err == nil {
			for _, g := range grps {
				groupID[g.Name] = g.ID
			}
		} else {
			slog.Warn("admin: import could not list groups; all accounts will be ungrouped", "err", err.Error())
		}

		res := importAccountsResult{}
		for _, ea := range in.Accounts {
			name := strings.TrimSpace(ea.Name)
			if name == "" || strings.TrimSpace(ea.Provider) == "" {
				res.Failed++
				res.Errors = append(res.Errors, importAccountError{Kind: "account", Name: name, Message: "name and provider are required"})
				continue
			}
			authType, aerr := normalizeAuthType(ea.AuthType)
			if aerr != "" {
				res.Failed++
				res.Errors = append(res.Errors, importAccountError{Kind: "account", Name: name, Message: aerr})
				continue
			}

			params := repository.InsertParams{
				Name:                    name,
				Provider:                strings.TrimSpace(ea.Provider),
				Enabled:                 ea.Enabled,
				Weight:                  ea.Weight,
				Priority:                ea.Priority,
				CostMultiplier:          ea.CostMultiplier,
				BaseURL:                 strings.TrimSpace(ea.BaseURL),
				Credentials:             ea.Credentials,
				CircuitFailureThreshold: ea.CircuitFailureThreshold,
				CircuitHalfOpenSuccess:  ea.CircuitHalfOpenSuccess,
				ModelRedirects:          ea.ModelRedirects,
				DailyUSDLimit:           ea.DailyUSDLimit,
				TotalUSDLimit:           ea.TotalUSDLimit,
				CustomHeaders:           ea.CustomHeaders,
				Endpoints:               ea.Endpoints,
				HealthProbeEnabled:      ea.HealthProbeEnabled,
				ProxyURL:                strings.TrimSpace(ea.ProxyURL),
				ParamOverrides:          ea.ParamOverrides,
				ActiveWindows:           ea.ActiveWindows,
				ActiveTimezone:          strings.TrimSpace(ea.ActiveTimezone),
				ForwardClientIP:         ea.ForwardClientIP,
				AllowedModels:           sanitizeAllowedModels(ea.AllowedModels),
				AuthType:                authType,
				AuthPlugin:              strings.TrimSpace(ea.AuthPlugin),
				ClientProfile:           strings.TrimSpace(ea.ClientProfile),
				ClientProfileConfig:     ea.ClientProfileConfig,
				MaxConcurrency:          ea.MaxConcurrency,
			}
			if ea.CircuitOpenDurationMs != nil {
				d := time.Duration(*ea.CircuitOpenDurationMs) * time.Millisecond
				params.CircuitOpenDuration = &d
			}
			if gn := strings.TrimSpace(ea.GroupName); gn != "" {
				if id, ok := groupID[gn]; ok {
					params.GroupID = &id
				} else {
					res.Errors = append(res.Errors, importAccountError{Kind: "warning", Name: name,
						Message: "group " + gn + " not found; imported as ungrouped"})
				}
			}

			if _, err := store.Insert(c.Request.Context(), params); err != nil {
				if errors.Is(err, repository.ErrAccountNameTaken) {
					if failOnConflict {
						res.Failed++
						res.Errors = append(res.Errors, importAccountError{Kind: "account", Name: name, Message: "name already exists"})
					} else {
						res.Skipped++
					}
					continue
				}
				res.Failed++
				res.Errors = append(res.Errors, importAccountError{Kind: "account", Name: name, Message: err.Error()})
				continue
			}
			res.Created++
		}

		slog.Info("admin: accounts imported",
			"created", res.Created, "skipped", res.Skipped, "failed", res.Failed, "admin", adminActor(c))
		c.JSON(http.StatusOK, res)
	}
}

// parseIDFilter parses a "1,2,3" query value into a set. Returns nil
// (no filter — export everything) for an empty value; blank/invalid
// entries are skipped.
func parseIDFilter(raw string) map[int64]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[int64]bool{}
	for _, part := range strings.Split(raw, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil {
			out[id] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
