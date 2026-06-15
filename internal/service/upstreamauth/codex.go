package upstreamauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Codex OAuth constants. These mirror the official Codex CLI client:
// the gateway refreshes tokens exactly the way the CLI itself would,
// so imported ~/.codex/auth.json credentials keep working.
const (
	codexPluginName   = "codex-oauth"
	codexTokenURL     = "https://auth.openai.com/oauth/token"
	codexAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	codexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexRefreshScope = "openid profile email"
	// codexLoginScope is the broader scope requested at browser login
	// (offline_access yields a refresh_token).
	codexLoginScope = "openid profile email offline_access"
	// codexRedirectURI is the native Codex CLI's loopback callback. The
	// browser lands there after login; with no local listener the page
	// just fails to load, but the address bar holds ?code=&state= which
	// the admin pastes back. An admin can override it.
	codexRedirectURI = "http://localhost:1455/auth/callback"
	// codexOriginator identifies the client family in the authorize URL.
	codexOriginator = "codex_cli_rs"
	// codexAuthClaim is the namespaced JWT claim OpenAI puts account
	// metadata under (chatgpt_account_id, chatgpt_plan_type, ...).
	codexAuthClaim = "https://api.openai.com/auth"
	// codexDefaultTokenTTL approximates the access-token lifetime when
	// neither the JWT exp claim nor expires_in is available.
	codexDefaultTokenTTL = time.Hour
)

// Standard credential keys persisted for imported OAuth accounts
// (design doc §6 导入 CLI 凭证).
const (
	credAccessToken  = "access_token"
	credRefreshToken = "refresh_token"
	credIDToken      = "id_token"
	credExpiresAt    = "expires_at" // unix seconds, decimal string
	credAccountID    = "account_id"
	credEmail        = "email"
	credPlan         = "plan"
	credSource       = "source"
	credSourceSchema = "source_schema_version"
)

// CodexOAuth implements the import + refresh + validate subset of the
// SPI for ChatGPT/Codex subscription accounts. Browser OAuth (PKCE)
// and device-code login are a later milestone.
type CodexOAuth struct {
	Unimplemented
	// HTTPClient overrides the default refresh client (tests). A
	// per-call client is still built when the account has a proxy.
	HTTPClient *http.Client
	// TokenURL overrides codexTokenURL (tests).
	TokenURL string
}

// NewCodexOAuth returns the codex-oauth plugin with default transport.
func NewCodexOAuth() *CodexOAuth {
	return &CodexOAuth{}
}

func (p *CodexOAuth) Metadata(context.Context) (*Metadata, error) {
	return &Metadata{
		Name:               codexPluginName,
		DisplayName:        "OpenAI Codex OAuth",
		SupportedProviders: []string{"openai-codex"},
		AuthMethods:        []string{"import_auth_json", "browser_oauth"},
		Experimental:       true,
	}, nil
}

// codexAuthJSON is the on-disk shape of ~/.codex/auth.json as written
// by the Codex CLI. A flat already-standard credential object is also
// accepted (fields promoted to the top level).
type codexAuthJSON struct {
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	Tokens       struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`

	// Flat / pre-normalised variants.
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
	ExpiresAt    string `json:"expires_at"`
}

// ImportCredentials parses a pasted auth.json (native Codex CLI layout
// or the flat normalised layout) into the standard credential set.
func (p *CodexOAuth) ImportCredentials(_ context.Context, req *ImportCredentialsRequest) (*TokenBundle, error) {
	if req == nil || len(strings.TrimSpace(string(req.Payload))) == 0 {
		return nil, fmt.Errorf("codex-oauth: empty auth.json payload")
	}
	var in codexAuthJSON
	if err := json.Unmarshal(req.Payload, &in); err != nil {
		return nil, fmt.Errorf("codex-oauth: parse auth.json: %w", err)
	}

	access := firstNonEmpty(in.Tokens.AccessToken, in.AccessToken)
	refresh := firstNonEmpty(in.Tokens.RefreshToken, in.RefreshToken)
	idToken := firstNonEmpty(in.Tokens.IDToken, in.IDToken)
	accountID := firstNonEmpty(in.Tokens.AccountID, in.AccountID)
	if access == "" && refresh == "" {
		return nil, fmt.Errorf("codex-oauth: auth.json carries neither access_token nor refresh_token (is this the right file?)")
	}
	if refresh == "" {
		return nil, fmt.Errorf("codex-oauth: auth.json has no refresh_token; the gateway could not renew the session — re-login in Codex CLI and re-export")
	}

	profile := codexProfileFromJWTs(idToken, access)
	if accountID == "" {
		accountID = profile.Subject
	} else {
		profile.Subject = accountID
	}

	expiresAt := codexImportExpiry(in, access)

	creds := map[string]string{
		credAccessToken:  access,
		credRefreshToken: refresh,
		credAccountID:    accountID,
		credSource:       "codex_auth_json",
		credSourceSchema: "1",
	}
	if idToken != "" {
		creds[credIDToken] = idToken
	}
	if profile.Email != "" {
		creds[credEmail] = profile.Email
	}
	if profile.Plan != "" {
		creds[credPlan] = profile.Plan
	}
	if expiresAt != nil {
		creds[credExpiresAt] = strconv.FormatInt(expiresAt.Unix(), 10)
	}

	return &TokenBundle{Credentials: creds, ExpiresAt: expiresAt, Profile: &profile}, nil
}

// codexImportExpiry derives the access-token expiry at import time:
// explicit expires_at field, then the JWT exp claim, then
// last_refresh + default TTL. A zero-time fallback (now) forces the
// TokenManager to refresh on first use rather than trusting an
// unknown-age token.
func codexImportExpiry(in codexAuthJSON, accessToken string) *time.Time {
	if in.ExpiresAt != "" {
		if sec, err := strconv.ParseInt(strings.TrimSpace(in.ExpiresAt), 10, 64); err == nil && sec > 0 {
			t := time.Unix(sec, 0).UTC()
			return &t
		}
	}
	if claims := decodeJWTClaims(accessToken); claims != nil {
		if exp, ok := claims["exp"].(float64); ok && exp > 0 {
			t := time.Unix(int64(exp), 0).UTC()
			return &t
		}
	}
	if in.LastRefresh != "" {
		if lr, err := time.Parse(time.RFC3339, in.LastRefresh); err == nil {
			t := lr.Add(codexDefaultTokenTTL).UTC()
			return &t
		}
	}
	now := time.Now().UTC()
	return &now
}

// codexTokenResponse is the auth.openai.com/oauth/token reply.
type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Refresh exchanges the stored refresh_token for a fresh access token.
// The refresh_token may rotate: the returned bundle always carries the
// newest one (falling back to the previous token when the server omits
// it). Identity fields are re-derived when a new id_token arrives.
func (p *CodexOAuth) Refresh(ctx context.Context, req *RefreshRequest) (*TokenBundle, error) {
	if req == nil || req.Credentials[credRefreshToken] == "" {
		return nil, fmt.Errorf("codex-oauth: no refresh_token stored; re-import auth.json")
	}
	old := req.Credentials
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {codexClientID},
		"refresh_token": {old[credRefreshToken]},
		"scope":         {codexRefreshScope},
	}
	tr, err := p.postCodexToken(ctx, form, req.ProxyURL)
	if err != nil {
		return nil, err
	}
	return bundleFromCodexToken(tr, old, "codex_auth_json"), nil
}

// BeginAuth generates a PKCE browser-login authorize URL. The returned
// CodeVerifier and State are held by the admin layer until the callback.
func (p *CodexOAuth) BeginAuth(_ context.Context, req *BeginAuthRequest) (*BeginAuthResponse, error) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, err
	}
	state, err := generateState()
	if err != nil {
		return nil, err
	}
	redirect := codexRedirectURI
	if req != nil && req.RedirectURI != "" {
		redirect = req.RedirectURI
	}
	params := url.Values{
		"response_type":              {"code"},
		"client_id":                  {codexClientID},
		"redirect_uri":               {redirect},
		"scope":                      {codexLoginScope},
		"state":                      {state},
		"code_challenge":             {codeChallengeS256(verifier)},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {codexOriginator},
	}
	return &BeginAuthResponse{
		AuthorizeURL: codexAuthorizeURL + "?" + params.Encode(),
		State:        state,
		CodeVerifier: verifier,
	}, nil
}

// ExchangeCallback swaps the authorization code (+ PKCE verifier) for a
// fresh token bundle. State validation is the admin layer's job.
func (p *CodexOAuth) ExchangeCallback(ctx context.Context, req *CallbackRequest) (*TokenBundle, error) {
	if req == nil || req.Code == "" || req.CodeVerifier == "" {
		return nil, fmt.Errorf("codex-oauth: code and code_verifier are required")
	}
	redirect := codexRedirectURI
	if req.RedirectURI != "" {
		redirect = req.RedirectURI
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {codexClientID},
		"code":          {req.Code},
		"redirect_uri":  {redirect},
		"code_verifier": {req.CodeVerifier},
	}
	tr, err := p.postCodexToken(ctx, form, req.ProxyURL)
	if err != nil {
		return nil, err
	}
	return bundleFromCodexToken(tr, nil, "codex_oauth_login"), nil
}

// postCodexToken POSTs a form to the token endpoint and returns the
// parsed response. A dead grant (invalid_grant / 401 / 403) is wrapped
// with ErrLoginRequired.
func (p *CodexOAuth) postCodexToken(ctx context.Context, form url.Values, proxyURL string) (codexTokenResponse, error) {
	tokenURL := p.TokenURL
	if tokenURL == "" {
		tokenURL = codexTokenURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return codexTokenResponse{}, fmt.Errorf("codex-oauth: build token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client, err := p.refreshClient(proxyURL)
	if err != nil {
		return codexTokenResponse{}, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return codexTokenResponse{}, fmt.Errorf("codex-oauth: token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tr codexTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil && resp.StatusCode == http.StatusOK {
		return codexTokenResponse{}, fmt.Errorf("codex-oauth: decode token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		msg := strings.TrimSpace(firstNonEmpty(tr.ErrorDesc, tr.Error, truncate(string(body), 200)))
		if tr.Error == "invalid_grant" || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return tr, fmt.Errorf("codex-oauth: token rejected (HTTP %d): %s: %w", resp.StatusCode, msg, ErrLoginRequired)
		}
		return tr, fmt.Errorf("codex-oauth: token rejected (HTTP %d): %s", resp.StatusCode, msg)
	}
	return tr, nil
}

// bundleFromCodexToken builds a TokenBundle from a successful token
// response, merging with prior credentials (old may be nil for a fresh
// login). defaultSource tags where the credentials came from.
func bundleFromCodexToken(tr codexTokenResponse, old map[string]string, defaultSource string) *TokenBundle {
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = codexDefaultTokenTTL
	}
	expiresAt := time.Now().Add(ttl).UTC()

	idToken := firstNonEmpty(tr.IDToken, old[credIDToken])
	profile := codexProfileFromJWTs(idToken, tr.AccessToken)
	if profile.Subject == "" {
		profile.Subject = old[credAccountID]
	}

	creds := map[string]string{
		credAccessToken:  tr.AccessToken,
		credRefreshToken: firstNonEmpty(tr.RefreshToken, old[credRefreshToken]),
		credAccountID:    firstNonEmpty(profile.Subject, old[credAccountID]),
		credExpiresAt:    strconv.FormatInt(expiresAt.Unix(), 10),
		credSource:       firstNonEmpty(old[credSource], defaultSource),
		credSourceSchema: firstNonEmpty(old[credSourceSchema], "1"),
	}
	if idToken != "" {
		creds[credIDToken] = idToken
	}
	if v := firstNonEmpty(profile.Email, old[credEmail]); v != "" {
		creds[credEmail] = v
	}
	if v := firstNonEmpty(profile.Plan, old[credPlan]); v != "" {
		creds[credPlan] = v
	}

	return &TokenBundle{Credentials: creds, ExpiresAt: &expiresAt, Profile: &profile}
}

// Validate re-derives the profile from stored tokens. Offline: claims
// come from the JWTs; no upstream call is made.
func (p *CodexOAuth) Validate(_ context.Context, req *ValidateRequest) (*AccountProfile, error) {
	if req == nil || (req.Credentials[credIDToken] == "" && req.Credentials[credAccessToken] == "") {
		return nil, fmt.Errorf("codex-oauth: no tokens stored")
	}
	profile := codexProfileFromJWTs(req.Credentials[credIDToken], req.Credentials[credAccessToken])
	if profile.Subject == "" {
		profile.Subject = req.Credentials[credAccountID]
	}
	return &profile, nil
}

// refreshClient picks the HTTP client for a refresh call, building a
// proxied one when the account routes through a proxy.
func (p *CodexOAuth) refreshClient(proxyURL string) (*http.Client, error) {
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("codex-oauth: bad account proxy_url: %w", err)
		}
		return &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		}, nil
	}
	if p.HTTPClient != nil {
		return p.HTTPClient, nil
	}
	return &http.Client{Timeout: 30 * time.Second}, nil
}

// codexProfileFromJWTs extracts identity from the id_token (preferred)
// or access_token claims: top-level email plus chatgpt_account_id /
// chatgpt_plan_type under the namespaced auth claim.
func codexProfileFromJWTs(tokens ...string) AccountProfile {
	var out AccountProfile
	for _, tok := range tokens {
		claims := decodeJWTClaims(tok)
		if claims == nil {
			continue
		}
		if out.Email == "" {
			if v, ok := claims["email"].(string); ok {
				out.Email = v
			}
		}
		if auth, ok := claims[codexAuthClaim].(map[string]any); ok {
			if out.Subject == "" {
				if v, ok := auth["chatgpt_account_id"].(string); ok {
					out.Subject = v
				}
			}
			if out.Plan == "" {
				if v, ok := auth["chatgpt_plan_type"].(string); ok {
					out.Plan = v
				}
			}
		}
		if out.Subject != "" && out.Email != "" && out.Plan != "" {
			break
		}
	}
	return out
}

// decodeJWTClaims base64url-decodes a JWT payload into a claim map.
// The signature is intentionally NOT verified: the token was received
// over TLS from the issuer (or pasted by the admin) and is only used
// for display metadata, never to grant access on our side.
func decodeJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
