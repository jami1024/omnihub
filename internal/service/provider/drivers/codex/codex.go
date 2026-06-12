// Package codex implements the OmniHub provider.Driver contract against
// the ChatGPT Codex backend (chatgpt.com/backend-api/codex) used by
// Codex / GPT Pro subscription accounts.
//
// EXPERIMENTAL. This driver is a matched-pair pass-through for the
// OpenAI Responses API: the client speaks Responses on /v1/responses,
// the upstream speaks Responses, and the body is replayed verbatim
// apart from the adjustments the codex backend requires (store=false,
// instructions always present, generation knobs the backend rejects
// stripped). There is no IR re-rendering and no cross-protocol
// conversion. Accounts use auth_type oauth/imported_oauth with the
// codex-oauth plugin; the TokenManager keeps access_token fresh before
// the request reaches BuildRequest.
package codex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// errNotImplemented marks the IR-decode methods this pass-through
// driver deliberately does not provide (no cross-protocol rendering).
var errNotImplemented = errors.New("codex: matched-pair pass-through only; IR decoding not implemented")

const (
	// DriverName is the string used to register and look up this driver.
	DriverName = "openai-codex"

	// DefaultBaseURL is the upstream host used when an account does not
	// override it.
	DefaultBaseURL = "https://chatgpt.com"

	// responsesPath is the codex Responses route on the default host.
	responsesPath = "/backend-api/codex/responses"

	// usageProbePath is a cheap authenticated GET used by the admin
	// connectivity test: it validates the access token and account id
	// without spending tokens.
	usageProbePath = "/backend-api/wham/usage"

	// originatorValue identifies the calling client family to the codex
	// backend. The native Codex CLI sends codex_cli_rs.
	originatorValue = "codex_cli_rs"
)

// KnownModels is the static model list served by /v1/models for this
// driver. The codex backend has no public model-list endpoint; this
// mirrors what current Codex subscriptions accept. Model redirects can
// map anything else onto these.
var KnownModels = []string{
	"gpt-5",
	"gpt-5-codex",
	"gpt-5.1",
	"gpt-5.1-codex",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex-mini",
	"codex-mini-latest",
}

// Driver implements provider.Driver for the ChatGPT Codex backend.
type Driver struct{}

// New returns a new codex driver.
func New() *Driver {
	return &Driver{}
}

// Name returns the driver name used for registration.
func (d *Driver) Name() string { return DriverName }

// Capabilities advertises features supported by this driver. Thinking
// maps to Responses reasoning output (encrypted content passthrough).
func (d *Driver) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:      true,
		Streaming: true,
		Tools:     true,
		Vision:    true,
		Thinking:  true,
	}
}

// Test probes the account with GET /backend-api/wham/usage — it
// exercises the OAuth access token and the chatgpt-account-id pair
// without consuming model quota.
func (d *Driver) Test(ctx context.Context, account *provider.Account) provider.TestResult {
	base := DefaultBaseURL
	var token, accountID string
	if account != nil {
		if account.BaseURL != "" {
			base = account.BaseURL
		}
		token = account.Credential("access_token")
		accountID = account.Credential("account_id")
	}
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"Accept":        "application/json",
		"originator":    originatorValue,
	}
	if accountID != "" {
		headers["chatgpt-account-id"] = accountID
	}
	return provider.ProbeGET(ctx, strings.TrimRight(base, "/")+usageProbePath, headers)
}

// ParseResponse is unused on the matched-pair pass-through path (the
// Forwarder copies upstream bytes verbatim and the usage parser sniffs
// token counts). It exists to satisfy provider.Driver.
func (d *Driver) ParseResponse(*http.Response) (*ir.UnifiedResponse, error) {
	return nil, errNotImplemented
}

// DecodeStream is unused on the matched-pair pass-through path; see
// ParseResponse.
func (d *Driver) DecodeStream(body io.ReadCloser) provider.StreamIter {
	return &unsupportedIter{body: body}
}

type unsupportedIter struct{ body io.ReadCloser }

func (it *unsupportedIter) Next() (*ir.UnifiedChunk, error) {
	return nil, errNotImplemented
}

func (it *unsupportedIter) Close() error {
	if it.body != nil {
		err := it.body.Close()
		it.body = nil
		return err
	}
	return nil
}

// endpointURL resolves the Responses URL for an account:
//
//   - ""                          → https://chatgpt.com/backend-api/codex/responses
//   - "https://host"              → https://host/backend-api/codex/responses
//   - "https://host/.../responses" → used verbatim (full override)
func (d *Driver) endpointURL(account *provider.Account) string {
	base := DefaultBaseURL
	if account != nil && account.BaseURL != "" {
		base = account.BaseURL
	}
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/responses") {
		return base
	}
	return base + responsesPath
}
