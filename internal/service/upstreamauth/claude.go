package upstreamauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Claude OAuth constants. These mirror the official Claude Code CLI
// client: the gateway refreshes tokens exactly the way the CLI itself
// would, so imported ~/.claude/.credentials.json sessions keep working.
const (
	claudePluginName = "claude-oauth"
	claudeTokenURL   = "https://console.anthropic.com/v1/oauth/token"
	claudeClientID   = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	// claudeProfileURL returns the authenticated account's identity
	// (email, pro/max flags) — used by Validate to enrich the admin UI.
	claudeProfileURL = "https://api.anthropic.com/api/oauth/profile"
	// claudeOAuthBeta is the anthropic-beta value OAuth-authenticated
	// calls must carry.
	claudeOAuthBeta = "oauth-2025-04-20"

	claudeDefaultTokenTTL = time.Hour
)

// ClaudeOAuth implements the import + refresh + validate subset of the
// SPI for Claude Pro/Max subscription accounts. Browser OAuth (PKCE)
// is a later milestone.
type ClaudeOAuth struct {
	Unimplemented
	// HTTPClient overrides the default client (tests). A per-call
	// client is still built when the account has a proxy.
	HTTPClient *http.Client
	// TokenURL / ProfileURL override the defaults (tests).
	TokenURL   string
	ProfileURL string
}

// NewClaudeOAuth returns the claude-oauth plugin with default transport.
func NewClaudeOAuth() *ClaudeOAuth {
	return &ClaudeOAuth{}
}

func (p *ClaudeOAuth) Metadata(context.Context) (*Metadata, error) {
	return &Metadata{
		Name:               claudePluginName,
		DisplayName:        "Claude Pro/Max OAuth",
		SupportedProviders: []string{"claude-subscription"},
		AuthMethods:        []string{"import_credentials_json"},
		Experimental:       true,
	}, nil
}

// claudeCredentialsJSON is the on-disk shape of
// ~/.claude/.credentials.json as written by Claude Code. A flat
// already-standard credential object is also accepted.
type claudeCredentialsJSON struct {
	ClaudeAiOauth struct {
		AccessToken      string   `json:"accessToken"`
		RefreshToken     string   `json:"refreshToken"`
		ExpiresAt        int64    `json:"expiresAt"` // milliseconds since epoch
		Scopes           []string `json:"scopes"`
		SubscriptionType string   `json:"subscriptionType"`
	} `json:"claudeAiOauth"`

	// Flat / pre-normalised variants.
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"` // unix seconds, decimal string
	Plan         string `json:"plan"`
}

// ImportCredentials parses a pasted .credentials.json (native Claude
// Code layout or the flat normalised layout) into the standard
// credential set.
func (p *ClaudeOAuth) ImportCredentials(_ context.Context, req *ImportCredentialsRequest) (*TokenBundle, error) {
	if req == nil || len(strings.TrimSpace(string(req.Payload))) == 0 {
		return nil, fmt.Errorf("claude-oauth: empty credentials payload")
	}
	var in claudeCredentialsJSON
	if err := json.Unmarshal(req.Payload, &in); err != nil {
		return nil, fmt.Errorf("claude-oauth: parse credentials: %w", err)
	}

	access := firstNonEmpty(in.ClaudeAiOauth.AccessToken, in.AccessToken)
	refresh := firstNonEmpty(in.ClaudeAiOauth.RefreshToken, in.RefreshToken)
	if access == "" && refresh == "" {
		return nil, fmt.Errorf("claude-oauth: payload carries neither accessToken nor refreshToken (is this ~/.claude/.credentials.json?)")
	}
	if refresh == "" {
		return nil, fmt.Errorf("claude-oauth: no refreshToken; the gateway could not renew the session — re-login in Claude Code and re-export")
	}

	var expiresAt *time.Time
	switch {
	case in.ClaudeAiOauth.ExpiresAt > 0:
		t := time.UnixMilli(in.ClaudeAiOauth.ExpiresAt).UTC()
		expiresAt = &t
	case in.ExpiresAt != "":
		if sec, err := strconv.ParseInt(strings.TrimSpace(in.ExpiresAt), 10, 64); err == nil && sec > 0 {
			t := time.Unix(sec, 0).UTC()
			expiresAt = &t
		}
	}
	if expiresAt == nil {
		// Unknown age: force a refresh on first use instead of trusting
		// a possibly-dead token.
		now := time.Now().UTC()
		expiresAt = &now
	}

	plan := firstNonEmpty(in.ClaudeAiOauth.SubscriptionType, in.Plan)

	creds := map[string]string{
		credAccessToken:  access,
		credRefreshToken: refresh,
		credExpiresAt:    strconv.FormatInt(expiresAt.Unix(), 10),
		credSource:       "claude_credentials_json",
		credSourceSchema: "1",
	}
	if scopes := strings.Join(in.ClaudeAiOauth.Scopes, " "); scopes != "" {
		creds["scopes"] = scopes
	}
	if plan != "" {
		creds[credPlan] = plan
	}

	// The credentials file carries no email/uuid; Validate (the profile
	// endpoint) fills those in when reachable.
	return &TokenBundle{
		Credentials: creds,
		ExpiresAt:   expiresAt,
		Profile:     &AccountProfile{Plan: plan},
	}, nil
}

// claudeTokenResponse is the console.anthropic.com/v1/oauth/token reply.
type claudeTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	Scope        string `json:"scope"`
	AccountType  string `json:"account_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Refresh exchanges the stored refresh_token for a fresh access token.
// Unlike Codex, the Claude token endpoint takes a JSON body.
func (p *ClaudeOAuth) Refresh(ctx context.Context, req *RefreshRequest) (*TokenBundle, error) {
	if req == nil || req.Credentials[credRefreshToken] == "" {
		return nil, fmt.Errorf("claude-oauth: no refresh_token stored; re-import credentials")
	}
	old := req.Credentials

	payload, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": old[credRefreshToken],
		"client_id":     claudeClientID,
	})
	if err != nil {
		return nil, fmt.Errorf("claude-oauth: encode refresh request: %w", err)
	}
	tokenURL := p.TokenURL
	if tokenURL == "" {
		tokenURL = claudeTokenURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("claude-oauth: build refresh request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client, err := p.client(req.ProxyURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claude-oauth: refresh request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tr claudeTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil && resp.StatusCode == http.StatusOK {
		return nil, fmt.Errorf("claude-oauth: decode refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		msg := strings.TrimSpace(firstNonEmpty(tr.ErrorDesc, tr.Error, truncate(string(body), 200)))
		if tr.Error == "invalid_grant" || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("claude-oauth: refresh rejected (HTTP %d): %s: %w", resp.StatusCode, msg, ErrLoginRequired)
		}
		return nil, fmt.Errorf("claude-oauth: refresh rejected (HTTP %d): %s", resp.StatusCode, msg)
	}

	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = claudeDefaultTokenTTL
	}
	expiresAt := time.Now().Add(ttl).UTC()

	creds := map[string]string{
		credAccessToken:  tr.AccessToken,
		credRefreshToken: firstNonEmpty(tr.RefreshToken, old[credRefreshToken]),
		credExpiresAt:    strconv.FormatInt(expiresAt.Unix(), 10),
		credSource:       firstNonEmpty(old[credSource], "claude_credentials_json"),
		credSourceSchema: firstNonEmpty(old[credSourceSchema], "1"),
	}
	if scopes := firstNonEmpty(tr.Scope, old["scopes"]); scopes != "" {
		creds["scopes"] = scopes
	}
	plan := firstNonEmpty(tr.AccountType, old[credPlan])
	if plan != "" {
		creds[credPlan] = plan
	}
	if v := old[credAccountID]; v != "" {
		creds[credAccountID] = v
	}
	if v := old[credEmail]; v != "" {
		creds[credEmail] = v
	}

	return &TokenBundle{
		Credentials: creds,
		ExpiresAt:   &expiresAt,
		Profile:     &AccountProfile{Plan: plan, Subject: old[credAccountID], Email: old[credEmail]},
	}, nil
}

// claudeProfileResponse is the /api/oauth/profile reply subset the
// gateway cares about.
type claudeProfileResponse struct {
	Account struct {
		UUID         string `json:"uuid"`
		Email        string `json:"email"`
		HasClaudeMax bool   `json:"has_claude_max"`
		HasClaudePro bool   `json:"has_claude_pro"`
	} `json:"account"`
	Organization struct {
		OrganizationType string `json:"organization_type"`
	} `json:"organization"`
}

// Validate fetches the authenticated identity from the profile
// endpoint: email, account uuid, and the effective plan tier.
func (p *ClaudeOAuth) Validate(ctx context.Context, req *ValidateRequest) (*AccountProfile, error) {
	if req == nil || req.Credentials[credAccessToken] == "" {
		return nil, fmt.Errorf("claude-oauth: no access_token stored")
	}
	profileURL := p.ProfileURL
	if profileURL == "" {
		profileURL = claudeProfileURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("claude-oauth: build profile request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.Credentials[credAccessToken])
	httpReq.Header.Set("anthropic-beta", claudeOAuthBeta)
	httpReq.Header.Set("Accept", "application/json")

	client, err := p.client(req.ProxyURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claude-oauth: profile request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude-oauth: profile rejected (HTTP %d): %s", resp.StatusCode, truncate(string(body), 200))
	}

	var pr claudeProfileResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("claude-oauth: decode profile: %w", err)
	}
	plan := "claude_free"
	switch {
	case pr.Account.HasClaudeMax || pr.Organization.OrganizationType == "claude_enterprise":
		plan = "claude_max"
	case pr.Account.HasClaudePro:
		plan = "claude_pro"
	}
	return &AccountProfile{
		Subject: pr.Account.UUID,
		Email:   pr.Account.Email,
		Plan:    plan,
	}, nil
}

// client picks the HTTP client for a plugin call, building a proxied
// one when the account routes through a proxy.
func (p *ClaudeOAuth) client(proxyURL string) (*http.Client, error) {
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("claude-oauth: bad account proxy_url: %w", err)
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
