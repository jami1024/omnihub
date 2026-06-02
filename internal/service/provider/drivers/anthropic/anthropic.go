// Package anthropic implements the OmniHub provider.Driver contract
// against the Anthropic Messages API.
//
// The driver targets api.anthropic.com directly (not via AWS Bedrock).
// A separate "bedrock" driver handles InvokeModel / Converse routes.
//
// IR mapping is near-identity: the IR was designed as a superset of
// the Anthropic wire format, so request and response conversion is
// mostly direct JSON re-encoding with a small number of adjustments
// (header-only fields stripped from the body, IR Extensions discarded).
package anthropic

import (
	"context"

	"github.com/jami1024/omnihub/internal/service/provider"
)

const (
	// DriverName is the string used to register and look up this driver.
	DriverName = "anthropic"

	// DefaultBaseURL is the upstream host used when an account does
	// not override it.
	DefaultBaseURL = "https://api.anthropic.com"

	// DefaultAnthropicVersion is sent as the anthropic-version header.
	DefaultAnthropicVersion = "2023-06-01"

	// messagesPath is the path appended to the base URL.
	messagesPath = "/v1/messages"
)

// Driver implements provider.Driver for the Anthropic Messages API.
type Driver struct{}

// New returns a new Anthropic driver.
func New() *Driver {
	return &Driver{}
}

// Name returns the driver name used for registration.
func (d *Driver) Name() string { return DriverName }

// Capabilities advertises features supported by this driver.
func (d *Driver) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:      true,
		Streaming: true,
		Tools:     true,
		Vision:    true,
		Thinking:  true,
	}
}

// Test probes the account with a GET /v1/models (token-free), sending
// the x-api-key + anthropic-version headers so a bad key surfaces as a
// 401 rather than a generation charge.
func (d *Driver) Test(ctx context.Context, account *provider.Account) provider.TestResult {
	var key string
	if account != nil {
		key = account.Credential("api_key")
	}
	return provider.ProbeGET(ctx, provider.ModelsURL(d.baseURL(account), DefaultBaseURL),
		map[string]string{"x-api-key": key, "anthropic-version": DefaultAnthropicVersion})
}

// baseURL returns the upstream host for the given account, falling
// back to the driver default when the account does not set one.
func (d *Driver) baseURL(account *provider.Account) string {
	if account != nil && account.BaseURL != "" {
		return account.BaseURL
	}
	return DefaultBaseURL
}
