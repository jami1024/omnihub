// Package openai implements the OmniHub provider.Driver contract against
// the OpenAI Chat Completions API (and any OpenAI-compatible upstream:
// DeepSeek, Moonshot, vLLM, …).
//
// The driver translates between IR and the OpenAI wire format via
// internal/protocol/openai. On the matched-pair pass-through path the
// gateway copies upstream bytes to the client verbatim, so ParseResponse
// and DecodeStream are exercised mainly by tests and the future
// cross-protocol path; BuildRequest is the load-bearing method.
package openai

import (
	"strings"

	"github.com/jami1024/omnihub/internal/service/provider"
)

const (
	// DriverName is the string used to register and look up this driver.
	DriverName = "openai"

	// DefaultBaseURL is the upstream host used when an account does not
	// override it.
	DefaultBaseURL = "https://api.openai.com"

	// chatCompletionsPath is the canonical Chat Completions route.
	chatCompletionsPath = "/v1/chat/completions"
)

// Driver implements provider.Driver for the OpenAI Chat Completions API.
type Driver struct{}

// New returns a new OpenAI driver.
func New() *Driver {
	return &Driver{}
}

// Name returns the driver name used for registration.
func (d *Driver) Name() string { return DriverName }

// Capabilities advertises features supported by this driver. Thinking is
// not advertised: OpenAI reasoning is not representable in the IR's
// Anthropic-shaped thinking blocks.
func (d *Driver) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:      true,
		Streaming: true,
		Tools:     true,
		Vision:    true,
	}
}

// endpointURL resolves the Chat Completions URL for an account,
// normalising common base-URL shapes so operators can point at OpenAI,
// an OpenAI-compatible vendor, or a fully-qualified custom endpoint:
//
//   - ""                              → https://api.openai.com/v1/chat/completions
//   - "https://host"                  → https://host/v1/chat/completions
//   - "https://host/v1"               → https://host/v1/chat/completions
//   - "https://host/x/chat/completions" → used verbatim (full override)
func (d *Driver) endpointURL(account *provider.Account) string {
	base := DefaultBaseURL
	if account != nil && account.BaseURL != "" {
		base = account.BaseURL
	}
	base = strings.TrimRight(base, "/")

	switch {
	case strings.HasSuffix(base, "/chat/completions"):
		return base
	case strings.HasSuffix(base, "/v1"):
		return base + "/chat/completions"
	default:
		return base + chatCompletionsPath
	}
}
