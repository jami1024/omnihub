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
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`

	// CacheControl marks the tool definition as an Anthropic
	// prompt-cache breakpoint.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
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
