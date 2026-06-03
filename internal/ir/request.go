package ir

import "encoding/json"

// UnifiedRequest is the protocol-neutral form of an LLM request after
// protocol adaptation and before driver dispatch.
type UnifiedRequest struct {
	// Model is the requested model identifier. It may be a logical
	// alias the resolver maps to a provider-specific ID.
	Model string `json:"model"`

	// Messages is the conversation history. System prompts are
	// carried separately in System, not as the first message.
	Messages []Message `json:"messages"`

	// System is the system prompt(s). Anthropic accepts an array of
	// content blocks (each can carry its own CacheControl marker).
	// Empty when no system prompt was given.
	System []ContentBlock `json:"system,omitempty"`

	// Tools is the set of tools the model may call.
	Tools []Tool `json:"tools,omitempty"`

	// ToolChoice constrains tool usage. Nil means provider default.
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`

	// Stream indicates the client expects a streaming response.
	Stream bool `json:"stream,omitempty"`

	// MaxTokens caps the number of tokens generated. 0 means
	// provider default (where providers allow it).
	MaxTokens int `json:"max_tokens,omitempty"`

	// Pointers for fields where 0 is a meaningful "set to zero"
	// value and must be distinguishable from "not set".
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`

	// StopSequences is the list of strings that stop generation.
	StopSequences []string `json:"stop_sequences,omitempty"`

	// Thinking configures Anthropic extended thinking. Nil disables it.
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// AnthropicVersion identifies the Anthropic API revision.
	// Bedrock requires "bedrock-2023-05-31"; direct API uses
	// "2023-06-01". Set by the driver before dispatch.
	AnthropicVersion string `json:"anthropic_version,omitempty"`

	// AnthropicBeta lists beta feature tokens (e.g. extended thinking,
	// tool search, computer use). Carried in the request body for
	// Bedrock and in the anthropic-beta header for direct API.
	AnthropicBeta []string `json:"anthropic_beta,omitempty"`

	// Metadata holds opaque per-request hints such as user_id.
	Metadata map[string]any `json:"metadata,omitempty"`

	// Extensions carries provider-specific fields the IR does not
	// model directly. Drivers may inspect or pass them through.
	Extensions Extensions `json:"-"`

	// ClientIP is the gateway-resolved real client IP (via gin's
	// trusted-proxy logic). Header-only metadata — never serialised into
	// the upstream body. The forwarder emits it as X-Forwarded-For only
	// when the chosen account opts in (Account.ForwardClientIP).
	ClientIP string `json:"-"`

	// ClientMetadata carries opt-in HTTP headers lifted from the
	// inbound request that drivers may forward to compatible
	// upstreams — typically the Anthropic / OpenAI SDK identifier
	// set (x-stainless-*, x-app, x-claude-code-session-id,
	// x-client-request-id). Keys are lowercase; empty values are
	// not present in the map.
	//
	// Forwarding these headers improves upstream cache partitioning
	// and analytics without leaking PII (no IP, no UA fingerprinting,
	// no auth). Drivers decide which keys to actually emit based on
	// upstream compatibility.
	ClientMetadata map[string]string `json:"-"`
}

// Tool describes a function the model may call.
//
// Anthropic supports two kinds of tools on the same `tools` array:
//
//   - Custom tools: the caller provides Name + InputSchema and the
//     model returns tool_use blocks to invoke them locally. Wire
//     shape omits `type` (or sets it to "custom"); InputSchema is
//     required.
//   - Server-side tools: opaque, server-executed primitives such as
//     web_search, computer, bash, text_editor. The wire shape uses
//     a versioned discriminator like "web_search_20250305" and may
//     carry tool-specific configuration (max_uses, allowed_domains,
//     display_width_px, user_location, ...). InputSchema is absent.
//
// The struct preserves both forms losslessly. Known fields are
// typed; anything else lands in Extra and round-trips verbatim, so
// new server-side tools work without an IR schema bump.
type Tool struct {
	Type        string          `json:"type,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`

	// CacheControl marks the tool definition as an Anthropic
	// prompt-cache breakpoint.
	CacheControl *CacheControl `json:"cache_control,omitempty"`

	// Extra holds tool-specific fields not covered above. Server-side
	// tools (web_search, computer, etc.) need this to survive the
	// IR round-trip — dropping unknown keys silently was how the
	// gateway turned every server-tool request into a 400 from the
	// upstream's "custom.input_schema" validator.
	Extra map[string]json.RawMessage `json:"-"`
}

// reservedToolKeys enumerates the JSON keys Tool models explicitly;
// every other key found at unmarshal time falls into Extra.
var reservedToolKeys = map[string]struct{}{
	"type":          {},
	"name":          {},
	"description":   {},
	"input_schema":  {},
	"cache_control": {},
}

// UnmarshalJSON fills the typed fields and captures every other
// top-level key into Extra so server-side tool configuration is
// preserved across the IR round-trip.
func (t *Tool) UnmarshalJSON(data []byte) error {
	type alias Tool
	aux := (*alias)(t)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var extra map[string]json.RawMessage
	for k, v := range raw {
		if _, ok := reservedToolKeys[k]; ok {
			continue
		}
		if extra == nil {
			extra = make(map[string]json.RawMessage, len(raw))
		}
		extra[k] = v
	}
	t.Extra = extra
	return nil
}

// MarshalJSON emits the typed fields followed by every Extra entry.
// Extra keys never clobber a typed field — if a caller stuffed
// "name" into Extra it is dropped rather than producing duplicate
// JSON keys.
func (t Tool) MarshalJSON() ([]byte, error) {
	type alias Tool
	base, err := json.Marshal(alias(t))
	if err != nil {
		return nil, err
	}
	if len(t.Extra) == 0 {
		return base, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(base, &out); err != nil {
		return nil, err
	}
	for k, v := range t.Extra {
		if _, ok := reservedToolKeys[k]; ok {
			continue
		}
		if _, exists := out[k]; exists {
			continue
		}
		out[k] = v
	}
	return json.Marshal(out)
}

// ToolChoiceType enumerates the tool selection modes.
type ToolChoiceType string

const (
	ToolChoiceAuto ToolChoiceType = "auto"
	ToolChoiceAny  ToolChoiceType = "any"
	ToolChoiceTool ToolChoiceType = "tool"
	ToolChoiceNone ToolChoiceType = "none"
)

// ToolChoice constrains how the model picks a tool.
type ToolChoice struct {
	Type ToolChoiceType `json:"type"`

	// Name is required when Type == ToolChoiceTool.
	Name string `json:"name,omitempty"`
}

// ThinkingConfig enables Anthropic extended thinking.
type ThinkingConfig struct {
	Type         string `json:"type"`                    // "enabled" | "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // max thinking tokens
}
