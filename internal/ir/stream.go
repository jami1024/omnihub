package ir

// ChunkType enumerates the kinds of events emitted during a streaming
// response.
type ChunkType string

const (
	// ChunkMessageStart marks the beginning of a streamed message.
	ChunkMessageStart ChunkType = "message_start"

	// ChunkContentBlockStart marks the start of a new content block
	// (e.g. a text block or a tool_use block).
	ChunkContentBlockStart ChunkType = "content_block_start"

	// ChunkContentBlockDelta is an incremental update to the current
	// content block (a text fragment, a tool-input JSON fragment, etc.).
	ChunkContentBlockDelta ChunkType = "content_block_delta"

	// ChunkContentBlockStop marks the end of the current content block.
	ChunkContentBlockStop ChunkType = "content_block_stop"

	// ChunkMessageDelta carries top-level message updates (stop reason,
	// incremental usage).
	ChunkMessageDelta ChunkType = "message_delta"

	// ChunkMessageStop marks the end of a streamed message.
	ChunkMessageStop ChunkType = "message_stop"

	// ChunkPing is an upstream-injected keepalive that the gateway
	// forwards transparently.
	ChunkPing ChunkType = "ping"

	// ChunkError indicates the upstream surfaced an error mid-stream.
	ChunkError ChunkType = "error"
)

// UnifiedChunk is one event in a streaming response, normalised across
// providers but lossless: the original wire bytes can be reconstructed
// from these fields.
type UnifiedChunk struct {
	// Type is the event kind.
	Type ChunkType `json:"type"`

	// Index is the position of the current content block when
	// applicable (content_block_start, _delta, _stop).
	Index int `json:"index,omitempty"`

	// Message is populated for ChunkMessageStart and carries the
	// initial assistant message envelope (id, model, role, usage).
	Message *UnifiedResponse `json:"message,omitempty"`

	// ContentBlock is populated for ChunkContentBlockStart; it is the
	// initial state of a new content block.
	ContentBlock *ContentBlock `json:"content_block,omitempty"`

	// Delta is populated for ChunkContentBlockDelta and
	// ChunkMessageDelta. Fields are sparse: only the fields that
	// changed are filled in.
	Delta *Delta `json:"delta,omitempty"`

	// Usage is populated when the upstream emits incremental token
	// counts (typically on the final message_delta event).
	Usage *Usage `json:"usage,omitempty"`
}

// Delta carries incremental updates within a stream chunk.
type Delta struct {
	// Type identifies the delta variant: "text_delta",
	// "input_json_delta", "thinking_delta", "signature_delta".
	Type string `json:"type,omitempty"`

	// Text is populated for text_delta and thinking_delta.
	Text string `json:"text,omitempty"`

	// PartialJSON is populated for input_json_delta and carries an
	// incremental fragment of a tool_use block's "input" JSON.
	PartialJSON string `json:"partial_json,omitempty"`

	// Thinking is populated for thinking_delta.
	Thinking string `json:"thinking,omitempty"`

	// Signature is populated for signature_delta.
	Signature string `json:"signature,omitempty"`

	// StopReason is populated on the final message_delta event.
	StopReason StopReason `json:"stop_reason,omitempty"`

	// StopSequence is populated when StopReason ==
	// StopReasonStopSequence.
	StopSequence string `json:"stop_sequence,omitempty"`
}
