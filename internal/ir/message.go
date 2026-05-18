package ir

import (
	"bytes"
	"encoding/json"
)

// Message is one turn in a conversation.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// UnmarshalJSON accepts the two wire forms Anthropic's Messages API
// allows for `content`: a JSON array of content blocks, or a bare
// JSON string that is a shortcut for a single text block. The struct
// always stores the array form so downstream code only sees one shape.
func (m *Message) UnmarshalJSON(data []byte) error {
	type alias Message
	aux := &struct {
		Content json.RawMessage `json:"content"`
		*alias
	}{alias: (*alias)(m)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	m.Content = nil
	raw := bytes.TrimSpace(aux.Content)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		m.Content = []ContentBlock{TextBlock(s)}
		return nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return err
	}
	m.Content = blocks
	return nil
}

// BlockType identifies the variant of a ContentBlock.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockImage      BlockType = "image"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
	BlockThinking   BlockType = "thinking"
)

// ContentBlock is the polymorphic content unit. Exactly one set of
// variant fields is meaningful per Type.
//
// The flat-struct-with-tagged-variants shape (rather than a Go
// interface with type assertions) is deliberate: it round-trips Anthropic
// wire format with minimal transformation, marshals/unmarshals via the
// standard json package, and is trivially cloneable / comparable.
type ContentBlock struct {
	Type BlockType `json:"type"`

	// text block
	Text string `json:"text,omitempty"`

	// image block
	Source *ImageSource `json:"source,omitempty"`

	// tool_use block (assistant invoking a tool)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result block (user reporting a tool's output)
	ToolUseID     string            `json:"tool_use_id,omitempty"`
	ResultContent ToolResultContent `json:"content,omitempty"`
	IsError       bool              `json:"is_error,omitempty"`

	// thinking block (Anthropic extended thinking)
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// CacheControl flags this block as an Anthropic prompt-cache breakpoint.
	// Other providers ignore the field.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource describes an image attached to a content block.
type ImageSource struct {
	// Type is "base64" for inline data or "url" for hosted images.
	Type string `json:"type"`

	// MediaType e.g. "image/png", "image/jpeg".
	MediaType string `json:"media_type,omitempty"`

	// Data is the base64-encoded image payload when Type == "base64".
	Data string `json:"data,omitempty"`

	// URL is the image URL when Type == "url".
	URL string `json:"url,omitempty"`
}

// CacheControl marks a content block, system block, or tool definition
// as an Anthropic prompt-cache breakpoint.
type CacheControl struct {
	// Type is currently always "ephemeral".
	Type string `json:"type"`

	// TTL is "5m" or "1h" for Claude 4.5+. Empty means provider default.
	TTL string `json:"ttl,omitempty"`
}

// TextBlock is a convenience constructor for a plain text block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

// ToolResultContent is a list of ContentBlocks for a tool_result
// block that also accepts the bare-string wire form documented by
// Anthropic ("content": "some text" as a shortcut for a single text
// block). The underlying type is []ContentBlock so indexing, ranging,
// and len work as before; default JSON marshalling always emits the
// array form on the wire.
type ToolResultContent []ContentBlock

// UnmarshalJSON accepts either a JSON string or a JSON array of
// content blocks. A string becomes a single text block.
func (t *ToolResultContent) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		*t = nil
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		*t = ToolResultContent{TextBlock(s)}
		return nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return err
	}
	*t = ToolResultContent(blocks)
	return nil
}
