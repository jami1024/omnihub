// Package openai translates between the OpenAI Chat Completions wire
// format and OmniHub's internal representation (ir).
//
// It powers two call sites:
//
//   - the /v1/chat/completions entry handler, which parses an inbound
//     OpenAI request into IR (RequestFromOpenAI) so the rest of the
//     pipeline (auth, limits, session, resolver, billing) operates on
//     the unified shape; and
//   - the openai provider driver, which rebuilds an OpenAI request from
//     IR (RequestToOpenAI) and decodes OpenAI responses back into IR
//     (ResponseToIR / ChunkToIR).
//
// The IR is shaped after the Anthropic Messages API, so conversion is a
// real transformation rather than a pass-through. To avoid dropping
// OpenAI request fields the IR does not model (sampling penalties,
// response_format, seed, …), RequestFromOpenAI stashes every unmodeled
// top-level field in Extensions[ExtensionPassthroughKey] and
// RequestToOpenAI overlays them back. Only the fields the IR genuinely
// transforms — model, messages, tools, tool_choice, stream — are
// regenerated from the IR.
package openai

import (
	"bytes"
	"encoding/json"
)

// ExtensionPassthroughKey is the Extensions map key under which
// RequestFromOpenAI stores the original request's unmodeled top-level
// fields as a JSON object, so RequestToOpenAI can restore them.
const ExtensionPassthroughKey = "openai_passthrough"

// regeneratedKeys are the top-level request fields the IR transforms and
// the driver therefore rebuilds from IR. Everything else rides through
// verbatim in the passthrough blob.
var regeneratedKeys = map[string]struct{}{
	"model":          {},
	"messages":       {},
	"stream":         {},
	"stream_options": {},
	"tools":          {},
	"tool_choice":    {},
}

// chatRequest is the subset of the OpenAI Chat Completions request the
// converter reads directly. Unmodeled fields are captured separately as
// raw JSON for passthrough. The sampling fields are read only to
// populate the IR honestly; the driver re-emits them from the
// passthrough blob, not from IR, so their wire form is preserved.
type chatRequest struct {
	Model               string          `json:"model"`
	Messages            []chatMessage   `json:"messages"`
	Stream              bool            `json:"stream"`
	Tools               []chatTool      `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	Stop                stopValue       `json:"stop"`
}

// stopValue accepts OpenAI's `stop` field in either wire form: a single
// string or an array of strings. It always stores the slice form.
type stopValue []string

func (s *stopValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*s = nil
		return nil
	}
	if trimmed[0] == '"' {
		var single string
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return err
		}
		*s = stopValue{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(trimmed, &many); err != nil {
		return err
	}
	*s = stopValue(many)
	return nil
}

// chatMessage is one entry in the OpenAI messages array.
type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []toolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// contentPart is one element of a structured (array) message content.
type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// toolCall is an assistant's request to invoke a function.
type toolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// chatTool is an OpenAI tool definition (only "function" is supported).
type chatTool struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// streamOptions mirrors OpenAI's stream_options object. The driver sets
// IncludeUsage so the final streamed chunk carries token counts.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatResponse is a non-streaming OpenAI Chat Completions response.
type chatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

type chatUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// streamChunk is one OpenAI "chat.completion.chunk" SSE event.
type streamChunk struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}
