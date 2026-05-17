// Package usage extracts token-usage information and upstream
// identifiers from Anthropic-shaped responses, in both non-streaming
// (single JSON body) and streaming (SSE) forms.
//
// The shape of the data is identical between direct Anthropic and the
// Claude Platform on AWS endpoint (both speak the Messages API), so
// one parser covers both drivers.
package usage

import (
	"bytes"
	"encoding/json"
)

// Usage is the union of every interesting field the gateway might
// learn from an Anthropic response.
type Usage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64

	// UpstreamRequestID is Anthropic's `id` field on the message.
	// Useful for cross-system tracing against Anthropic's own logs.
	UpstreamRequestID string

	// ActualModel is the model name the upstream actually used,
	// which may differ from the client's requested alias
	// (e.g. "claude-haiku-4-5" → "claude-haiku-4-5-20251001").
	ActualModel string

	// StopReason captures the high-level reason generation ended.
	StopReason string
}

// FromAnthropicJSON parses a non-streaming Anthropic response body.
// Bodies that are not Anthropic-shaped JSON return a zero Usage.
func FromAnthropicJSON(body []byte) Usage {
	var resp struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Usage{}
	}
	return Usage{
		InputTokens:              resp.Usage.InputTokens,
		OutputTokens:             resp.Usage.OutputTokens,
		CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
		UpstreamRequestID:        resp.ID,
		ActualModel:              resp.Model,
		StopReason:               resp.StopReason,
	}
}

// SSESniffer extracts Usage from Anthropic SSE chunks as they fly past
// on the wire. Use one sniffer per request: Feed every line, then
// call Result at the end.
//
// Anthropic streams emit two events that carry usage information:
//
//   - message_start  carries the upstream id/model and the input-side
//     token count plus cache breakdowns. The output_tokens value is
//     a placeholder (typically 1–4).
//   - message_delta  carries the final output_tokens once generation
//     has completed.
//
// The sniffer reads both: message_start populates IDs and the
// input/cache counts; message_delta then overwrites output_tokens
// with the authoritative value.
type SSESniffer struct {
	u Usage
}

// NewSSESniffer returns a fresh sniffer.
func NewSSESniffer() *SSESniffer { return &SSESniffer{} }

// Feed consumes one line of SSE wire bytes. Lines that are not
// "data: { ... }" payloads are ignored. Feed is cheap on non-matching
// lines (a single prefix comparison) so the caller can pass every
// line read from the upstream.
func (s *SSESniffer) Feed(line []byte) {
	// Trim trailing CR/LF so the prefix check matches regardless of
	// the upstream's line ending style.
	line = bytes.TrimRight(line, "\r\n")
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 || payload[0] != '{' {
		return
	}

	var ev struct {
		Type    string `json:"type"`
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage *struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}

	switch ev.Type {
	case "message_start":
		if ev.Message.ID != "" {
			s.u.UpstreamRequestID = ev.Message.ID
		}
		if ev.Message.Model != "" {
			s.u.ActualModel = ev.Message.Model
		}
		if ev.Message.Usage != nil {
			s.u.InputTokens = ev.Message.Usage.InputTokens
			s.u.OutputTokens = ev.Message.Usage.OutputTokens
			s.u.CacheCreationInputTokens = ev.Message.Usage.CacheCreationInputTokens
			s.u.CacheReadInputTokens = ev.Message.Usage.CacheReadInputTokens
		}

	case "message_delta":
		if ev.Delta.StopReason != "" {
			s.u.StopReason = ev.Delta.StopReason
		}
		if ev.Usage != nil {
			// message_delta carries the authoritative final counts.
			if ev.Usage.OutputTokens > 0 {
				s.u.OutputTokens = ev.Usage.OutputTokens
			}
			if ev.Usage.InputTokens > 0 {
				s.u.InputTokens = ev.Usage.InputTokens
			}
			if ev.Usage.CacheCreationInputTokens > 0 {
				s.u.CacheCreationInputTokens = ev.Usage.CacheCreationInputTokens
			}
			if ev.Usage.CacheReadInputTokens > 0 {
				s.u.CacheReadInputTokens = ev.Usage.CacheReadInputTokens
			}
		}
	}
}

// Result returns the usage collected so far.
func (s *SSESniffer) Result() Usage { return s.u }
