package ir

// UnifiedResponse is the protocol-neutral form of a non-streaming
// LLM response.
type UnifiedResponse struct {
	// ID is the upstream-assigned response identifier (e.g.
	// "msg_01ABC..." for Anthropic).
	ID string `json:"id"`

	// Type echoes the wire type (Anthropic returns "message").
	Type string `json:"type,omitempty"`

	// Role is always RoleAssistant for chat completions.
	Role Role `json:"role"`

	// Model is the model that actually served the request, which may
	// differ from the requested model after redirects or aliasing.
	Model string `json:"model"`

	// Content is the assistant's reply, possibly mixing text,
	// tool_use, and thinking blocks.
	Content []ContentBlock `json:"content"`

	// StopReason explains why generation stopped.
	StopReason StopReason `json:"stop_reason,omitempty"`

	// StopSequence is the matched stop sequence when StopReason is
	// StopReasonStopSequence. Empty otherwise.
	StopSequence string `json:"stop_sequence,omitempty"`

	// Usage reports token accounting.
	Usage Usage `json:"usage"`

	// Extensions carries provider-specific fields that drivers chose
	// to surface upward (e.g. amazon-bedrock-invocationMetrics).
	Extensions Extensions `json:"-"`
}

// StopReason enumerates why the assistant stopped generating.
type StopReason string

const (
	StopReasonEndTurn      StopReason = "end_turn"
	StopReasonMaxTokens    StopReason = "max_tokens"
	StopReasonStopSequence StopReason = "stop_sequence"
	StopReasonToolUse      StopReason = "tool_use"
	StopReasonError        StopReason = "error"
)
