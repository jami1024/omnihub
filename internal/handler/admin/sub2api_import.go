package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// Direct import of sub2api-family account exports (sub2api / apipool).
// Their export is a flat envelope { type, version, proxies[], accounts[] }
// where each account carries platform+type and a credentials map. We map
// those onto OmniHub's provider + auth model so an operator can paste a
// sub2api backup and have working accounts without re-running OAuth.
//
// This is pure credential transport (no cross-protocol re-rendering): the
// OAuth tokens / refresh tokens are copied into OmniHub's standard
// credential set and the TokenManager takes over refresh from there.

// errSub2APIUnsupported flags a platform/type combination OmniHub has no
// driver for (e.g. gemini, kiro) — skipped with a warning, not failed.
var errSub2APIUnsupported = errors.New("unsupported platform/type")

// sub2apiProxy mirrors a DataProxy record. The proxy is referenced by
// proxy_key from each account; we rebuild an inline proxy URL from the
// parts (OmniHub stores it on the account as proxy_url).
type sub2apiProxy struct {
	ProxyKey string `json:"proxy_key"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// sub2apiAccount mirrors a DataAccount record. Credentials is left as a
// loose map because the exact keys vary by platform/type (and across
// forks); credFirst tolerates flat keys and a nested tokens{} object.
type sub2apiAccount struct {
	Name           string         `json:"name"`
	Platform       string         `json:"platform"`
	Type           string         `json:"type"`
	Credentials    map[string]any `json:"credentials"`
	ProxyKey       string         `json:"proxy_key"`
	Concurrency    int            `json:"concurrency"`
	Priority       int            `json:"priority"`
	RateMultiplier *float64       `json:"rate_multiplier"`
	ExpiresAt      *int64         `json:"expires_at"` // unix seconds
	Status         string         `json:"status"`
	Schedulable    *bool          `json:"schedulable"`
}

// sub2apiBundle is the export envelope. It also appears nested under
// "data" in sub2api's own import request, so the handler accepts either.
type sub2apiBundle struct {
	Type     string           `json:"type"`
	Version  int              `json:"version"`
	Proxies  []sub2apiProxy   `json:"proxies"`
	Accounts []sub2apiAccount `json:"accounts"`
}

// importSub2APIRequest accepts the export file verbatim (fields at the
// top level), the sub2api import wrapper ({data:{...}}), and an optional
// conflict policy.
type importSub2APIRequest struct {
	sub2apiBundle
	Data       *sub2apiBundle `json:"data"`
	OnConflict string         `json:"on_conflict"` // "skip" (default) | "fail"
}

// ImportSub2APIHandler handles POST /admin/api/accounts/import-sub2api.
// Each account is mapped and inserted independently: a duplicate name is
// skipped (or failed, per on_conflict), an unsupported platform/type is
// skipped with a warning, and a mapping error fails just that account —
// the batch never aborts. Proxies are rebuilt as inline proxy URLs and
// attached by proxy_key.
func ImportSub2APIHandler(store accountStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in importSub2APIRequest
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		bundle := in.sub2apiBundle
		if len(bundle.Accounts) == 0 && in.Data != nil {
			bundle = *in.Data // sub2api native {data:{...}} wrapper
		}
		if len(bundle.Accounts) == 0 {
			writeBadRequest(c, "no accounts in the bundle")
			return
		}

		// proxy_key → inline proxy URL.
		proxyURL := make(map[string]string, len(bundle.Proxies))
		for _, px := range bundle.Proxies {
			if key := strings.TrimSpace(px.ProxyKey); key != "" {
				proxyURL[key] = buildProxyURL(px)
			}
		}

		failOnConflict := strings.TrimSpace(in.OnConflict) == "fail"
		res := importAccountsResult{}
		for _, sa := range bundle.Accounts {
			name := strings.TrimSpace(sa.Name)
			if name == "" {
				res.Failed++
				res.Errors = append(res.Errors, importAccountError{Kind: "account", Name: "", Message: "name is required"})
				continue
			}

			params, err := mapSub2APIAccount(sa, proxyURL[strings.TrimSpace(sa.ProxyKey)])
			if errors.Is(err, errSub2APIUnsupported) {
				res.Skipped++
				res.Errors = append(res.Errors, importAccountError{Kind: "warning", Name: name,
					Message: fmt.Sprintf("platform %q type %q has no OmniHub driver; skipped", sa.Platform, sa.Type)})
				continue
			}
			if err != nil {
				res.Failed++
				res.Errors = append(res.Errors, importAccountError{Kind: "account", Name: name, Message: err.Error()})
				continue
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

		slog.Info("admin: sub2api accounts imported",
			"created", res.Created, "skipped", res.Skipped, "failed", res.Failed, "admin", adminActor(c))
		c.JSON(200, res)
	}
}

// mapSub2APIAccount translates one sub2api account into OmniHub insert
// params. It returns errSub2APIUnsupported for platform/type combos with
// no OmniHub driver, and a descriptive error for a recognised combo whose
// credentials are incomplete.
func mapSub2APIAccount(sa sub2apiAccount, proxyURL string) (repository.InsertParams, error) {
	platform := strings.ToLower(strings.TrimSpace(sa.Platform))
	typ := strings.ToLower(strings.TrimSpace(sa.Type))

	p := repository.InsertParams{
		Name:           strings.TrimSpace(sa.Name),
		Enabled:        true,
		Weight:         1,
		Priority:       sa.Priority,
		CostMultiplier: 1,
		MaxConcurrency: sa.Concurrency,
		ProxyURL:       proxyURL,
	}
	if sa.RateMultiplier != nil && *sa.RateMultiplier >= 0 {
		p.CostMultiplier = *sa.RateMultiplier
	}
	if strings.EqualFold(sa.Status, "disabled") || (sa.Schedulable != nil && !*sa.Schedulable) {
		p.Enabled = false
	}

	c := sa.Credentials
	access := credFirst(c, "access_token")
	refresh := credFirst(c, "refresh_token")
	idToken := credFirst(c, "id_token")
	accountID := credFirst(c, "account_id", "chatgpt_account_id")
	email := credFirst(c, "email")
	plan := credFirst(c, "plan", "plan_type")
	apiKey := credFirst(c, "api_key")
	baseURL := credFirst(c, "base_url")

	switch {
	case platform == "openai" && typ == "oauth":
		if refresh == "" {
			return p, fmt.Errorf("openai oauth account has no refresh_token; OmniHub could not renew it")
		}
		p.Provider, p.AuthType, p.AuthPlugin = "openai-codex", "imported_oauth", "codex-oauth"
		p.Credentials = stdOAuthCreds(access, refresh, idToken, accountID, email, plan, sa.ExpiresAt)

	case platform == "anthropic" && typ == "oauth":
		if refresh == "" {
			return p, fmt.Errorf("anthropic oauth account has no refresh_token; OmniHub could not renew it")
		}
		p.Provider, p.AuthType, p.AuthPlugin = "claude-subscription", "imported_oauth", "claude-oauth"
		p.Credentials = stdOAuthCreds(access, refresh, idToken, accountID, email, plan, sa.ExpiresAt)

	case platform == "anthropic" && (typ == "api_key" || typ == "setup_token"):
		if apiKey == "" {
			return p, fmt.Errorf("anthropic %s account has no api_key", typ)
		}
		p.Provider, p.AuthType = "anthropic", "api_key"
		p.Credentials = map[string]string{"api_key": apiKey}

	case platform == "openai" && (typ == "api_key" || typ == "upstream"):
		if apiKey == "" {
			return p, fmt.Errorf("openai %s account has no api_key", typ)
		}
		p.Provider, p.AuthType = "openai", "api_key"
		p.Credentials = map[string]string{"api_key": apiKey}
		if baseURL != "" {
			p.BaseURL = baseURL
		}

	default:
		return p, errSub2APIUnsupported
	}
	return p, nil
}

// stdOAuthCreds assembles OmniHub's standard imported-OAuth credential
// set. Tagged source=sub2api_import so the origin is auditable. expires_at
// (sub2api unix seconds) is carried through when present so the
// TokenManager schedules a proactive refresh; absent, the 401 path
// triggers refresh on first use.
func stdOAuthCreds(access, refresh, idToken, accountID, email, plan string, expUnix *int64) map[string]string {
	creds := map[string]string{
		"access_token":          access,
		"refresh_token":         refresh,
		"source":                "sub2api_import",
		"source_schema_version": "1",
	}
	if idToken != "" {
		creds["id_token"] = idToken
	}
	if accountID != "" {
		creds["account_id"] = accountID
	}
	if email != "" {
		creds["email"] = email
	}
	if plan != "" {
		creds["plan"] = plan
	}
	if expUnix != nil && *expUnix > 0 {
		creds["expires_at"] = strconv.FormatInt(*expUnix, 10)
	}
	return creds
}

// buildProxyURL rebuilds an inline proxy URL (scheme://user:pass@host:port)
// from a sub2api proxy record. An unknown/empty protocol defaults to http.
func buildProxyURL(px sub2apiProxy) string {
	scheme := strings.ToLower(strings.TrimSpace(px.Protocol))
	if scheme == "" {
		scheme = "http"
	}
	host := strings.TrimSpace(px.Host)
	if host == "" {
		return ""
	}
	if px.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, px.Port)
	}
	u := url.URL{Scheme: scheme, Host: host}
	if px.Username != "" {
		u.User = url.UserPassword(px.Username, px.Password)
	}
	return u.String()
}

// credFirst returns the first non-empty string value among keys, checking
// the top-level credentials map and then a nested "tokens" object (the
// native ~/.codex/auth.json layout). Numeric values are stringified.
func credFirst(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		if s := credString(m, k); s != "" {
			return s
		}
	}
	if tok, ok := m["tokens"].(map[string]any); ok {
		for _, k := range keys {
			if s := credString(tok, k); s != "" {
				return s
			}
		}
	}
	return ""
}

// credString coerces one credential value to a string (string as-is,
// numbers formatted, everything else empty).
func credString(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}
