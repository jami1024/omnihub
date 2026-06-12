// Package claudesub implements the OmniHub provider.Driver contract for
// Claude Pro/Max subscription accounts (OAuth Bearer auth against the
// same Anthropic Messages API the anthropic driver speaks).
//
// EXPERIMENTAL. The wire format is identical to the anthropic driver —
// response parsing and stream decoding are inherited from it — only the
// request differs:
//
//   - Authorization: Bearer <access_token> instead of x-api-key;
//   - anthropic-beta always includes oauth-2025-04-20;
//   - the system prompt must open with the Claude Code identity
//     sentence (the upstream rejects OAuth calls that do not look like
//     Claude Code);
//   - a claude-cli User-Agent, since the gateway's own UA would be
//     rejected for OAuth traffic.
//
// Accounts use auth_type oauth/imported_oauth with the claude-oauth
// plugin; the TokenManager keeps access_token fresh before requests
// reach BuildRequest.
package claudesub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
)

const (
	// DriverName is the string used to register and look up this driver.
	DriverName = "claude-subscription"

	// oauthBeta is the anthropic-beta token OAuth-authenticated calls
	// must carry (merged with whatever betas the client requested).
	oauthBeta = "oauth-2025-04-20"

	// claudeCodeSystemPrompt is the identity sentence the upstream
	// expects as the FIRST system block on OAuth traffic. OmniHub's
	// downstream clients are Claude Code, so it is normally already
	// there; when it is not, it is prepended (never replacing the
	// client's own system content).
	claudeCodeSystemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."

	// userAgent mimics the Claude Code CLI. OAuth traffic with a
	// non-CLI UA is rejected by the upstream.
	userAgent = "claude-cli/1.0.56 (external, cli)"

	// usageProbePath is a cheap authenticated GET used by the admin
	// connectivity test; it validates the OAuth token without spending
	// tokens.
	usageProbePath = "/api/oauth/usage"
)

// Driver implements provider.Driver for Claude subscription accounts.
// ParseResponse / DecodeStream / Capabilities are inherited from the
// anthropic driver (identical wire format).
type Driver struct {
	*anthropic.Driver
}

// New returns a new claude-subscription driver.
func New() *Driver {
	return &Driver{Driver: anthropic.New()}
}

// Name returns the driver name used for registration.
func (d *Driver) Name() string { return DriverName }

// Test probes the account with GET /api/oauth/usage — it exercises the
// OAuth access token without consuming model quota.
func (d *Driver) Test(ctx context.Context, account *provider.Account) provider.TestResult {
	base := anthropic.DefaultBaseURL
	var token string
	if account != nil {
		if account.BaseURL != "" {
			base = account.BaseURL
		}
		token = account.Credential("access_token")
	}
	return provider.ProbeGET(ctx, strings.TrimRight(base, "/")+usageProbePath, map[string]string{
		"Authorization":  "Bearer " + token,
		"anthropic-beta": oauthBeta,
		"Accept":         "application/json",
		"User-Agent":     userAgent,
	})
}

// BuildRequest mirrors the anthropic driver's request construction but
// signs with the OAuth Bearer token and asserts the Claude Code
// request shape the subscription backend requires.
func (d *Driver) BuildRequest(
	ctx context.Context,
	req *ir.UnifiedRequest,
	account *provider.Account,
) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("claudesub: nil request")
	}
	if account == nil {
		return nil, errors.New("claudesub: nil account")
	}
	token := account.Credential("access_token")
	if token == "" {
		return nil, errors.New("claudesub: account has no access_token; import claude credentials first")
	}

	wire := anthropic.ToWireBody(req)
	ensureClaudeCodeSystem(wire)

	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("claudesub: marshal body: %w", err)
	}

	base := anthropic.DefaultBaseURL
	if account.BaseURL != "" {
		base = account.BaseURL
	}
	url := strings.TrimRight(base, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("claudesub: build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("anthropic-version", anthropic.DefaultAnthropicVersion)
	httpReq.Header.Set("anthropic-beta", mergeBetas(req.AnthropicBeta))
	httpReq.Header.Set("User-Agent", userAgent)

	// Forward SDK identifier headers (cache partitioning); these are
	// genuine Claude Code headers on this surface.
	for k, v := range req.ClientMetadata {
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}

// mergeBetas prepends the mandatory OAuth beta to the client's own
// beta list, de-duplicating.
func mergeBetas(requested []string) string {
	out := []string{oauthBeta}
	for _, b := range requested {
		if b != "" && b != oauthBeta {
			out = append(out, b)
		}
	}
	return strings.Join(out, ",")
}

// ensureClaudeCodeSystem guarantees the first system block opens with
// the Claude Code identity sentence. Claude Code clients already send
// it; for anything else it is prepended so the client's own system
// content is preserved (shifted, not replaced).
func ensureClaudeCodeSystem(wire *anthropic.MessagesBody) {
	if len(wire.System) > 0 {
		first := wire.System[0]
		if first.Type == ir.BlockText && strings.HasPrefix(first.Text, claudeCodeSystemPrompt) {
			return
		}
	}
	system := make([]ir.ContentBlock, 0, len(wire.System)+1)
	system = append(system, ir.ContentBlock{Type: ir.BlockText, Text: claudeCodeSystemPrompt})
	system = append(system, wire.System...)
	wire.System = system
}
