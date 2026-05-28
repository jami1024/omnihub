package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jami1024/omnihub/internal/ir"
)

// RequestFromOpenAI parses an inbound OpenAI Chat Completions body into
// IR. Unmodeled top-level fields are preserved under
// Extensions[ExtensionPassthroughKey] so the driver can restore them.
func RequestFromOpenAI(body []byte) (*ir.UnifiedRequest, error) {
	var cr chatRequest
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("openai: decode request: %w", err)
	}
	if strings.TrimSpace(cr.Model) == "" {
		return nil, fmt.Errorf("openai: request missing model")
	}

	req := &ir.UnifiedRequest{
		Model:         cr.Model,
		Stream:        cr.Stream,
		Temperature:   cr.Temperature,
		TopP:          cr.TopP,
		StopSequences: cr.Stop,
	}
	if cr.MaxCompletionTokens != nil {
		req.MaxTokens = *cr.MaxCompletionTokens
	} else if cr.MaxTokens != nil {
		req.MaxTokens = *cr.MaxTokens
	}

	system, messages, err := messagesFromOpenAI(cr.Messages)
	if err != nil {
		return nil, err
	}
	req.System = system
	req.Messages = messages

	if len(cr.Tools) > 0 {
		req.Tools = toolsFromOpenAI(cr.Tools)
	}
	if tc, err := toolChoiceFromOpenAI(cr.ToolChoice); err != nil {
		return nil, err
	} else if tc != nil {
		req.ToolChoice = tc
	}

	if passthrough := passthroughFields(body); len(passthrough) > 0 {
		blob, err := json.Marshal(passthrough)
		if err != nil {
			return nil, fmt.Errorf("openai: encode passthrough: %w", err)
		}
		req.Extensions = ir.Extensions{ExtensionPassthroughKey: blob}
	}

	return req, nil
}

// passthroughFields returns the request's top-level fields minus the
// ones the driver regenerates from IR.
func passthroughFields(body []byte) map[string]json.RawMessage {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	for k := range regeneratedKeys {
		delete(raw, k)
	}
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// messagesFromOpenAI splits an OpenAI messages array into the IR's
// separate system prompt and conversation turns. system/developer
// messages collapse into System; tool messages become user-role
// tool_result turns; everything else maps role-for-role.
func messagesFromOpenAI(msgs []chatMessage) ([]ir.ContentBlock, []ir.Message, error) {
	var system []ir.ContentBlock
	var out []ir.Message

	for i, m := range msgs {
		switch m.Role {
		case "system", "developer":
			text, err := plainText(m.Content)
			if err != nil {
				return nil, nil, fmt.Errorf("openai: message %d: %w", i, err)
			}
			if text != "" {
				system = append(system, ir.TextBlock(text))
			}

		case "tool":
			text, err := plainText(m.Content)
			if err != nil {
				return nil, nil, fmt.Errorf("openai: message %d: %w", i, err)
			}
			out = append(out, ir.Message{
				Role: ir.RoleUser,
				Content: []ir.ContentBlock{{
					Type:          ir.BlockToolResult,
					ToolUseID:     m.ToolCallID,
					ResultContent: ir.ToolResultContent{ir.TextBlock(text)},
				}},
			})

		case "user":
			blocks, err := userContentBlocks(m.Content)
			if err != nil {
				return nil, nil, fmt.Errorf("openai: message %d: %w", i, err)
			}
			out = append(out, ir.Message{Role: ir.RoleUser, Content: blocks})

		case "assistant":
			blocks, err := assistantContentBlocks(m)
			if err != nil {
				return nil, nil, fmt.Errorf("openai: message %d: %w", i, err)
			}
			out = append(out, ir.Message{Role: ir.RoleAssistant, Content: blocks})

		default:
			return nil, nil, fmt.Errorf("openai: message %d: unsupported role %q", i, m.Role)
		}
	}

	return system, out, nil
}

// userContentBlocks converts an OpenAI user message's content (string or
// parts array) into IR content blocks.
func userContentBlocks(content json.RawMessage) ([]ir.ContentBlock, error) {
	if isJSONString(content) {
		var s string
		if err := json.Unmarshal(content, &s); err != nil {
			return nil, err
		}
		return []ir.ContentBlock{ir.TextBlock(s)}, nil
	}
	var parts []contentPart
	if err := json.Unmarshal(content, &parts); err != nil {
		return nil, fmt.Errorf("decode content parts: %w", err)
	}
	blocks := make([]ir.ContentBlock, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text", "input_text":
			blocks = append(blocks, ir.TextBlock(p.Text))
		case "image_url":
			if p.ImageURL != nil {
				blocks = append(blocks, imageBlock(p.ImageURL.URL))
			}
		}
	}
	return blocks, nil
}

// assistantContentBlocks converts an OpenAI assistant message (text plus
// optional tool_calls) into IR content blocks.
func assistantContentBlocks(m chatMessage) ([]ir.ContentBlock, error) {
	var blocks []ir.ContentBlock
	if len(m.Content) > 0 && !isJSONNull(m.Content) {
		if isJSONString(m.Content) {
			var s string
			if err := json.Unmarshal(m.Content, &s); err != nil {
				return nil, err
			}
			if s != "" {
				blocks = append(blocks, ir.TextBlock(s))
			}
		} else {
			var parts []contentPart
			if err := json.Unmarshal(m.Content, &parts); err == nil {
				for _, p := range parts {
					if p.Type == "text" || p.Type == "output_text" {
						blocks = append(blocks, ir.TextBlock(p.Text))
					}
				}
			}
		}
	}
	for _, tc := range m.ToolCalls {
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		blocks = append(blocks, ir.ContentBlock{
			Type:  ir.BlockToolUse,
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(args),
		})
	}
	return blocks, nil
}

// imageBlock builds an IR image block from an OpenAI image_url, handling
// both data URIs (base64) and remote URLs.
func imageBlock(url string) ir.ContentBlock {
	const dataPrefix = "data:"
	if strings.HasPrefix(url, dataPrefix) {
		if media, b64, ok := parseDataURI(url); ok {
			return ir.ContentBlock{
				Type: ir.BlockImage,
				Source: &ir.ImageSource{
					Type:      "base64",
					MediaType: media,
					Data:      b64,
				},
			}
		}
	}
	return ir.ContentBlock{
		Type:   ir.BlockImage,
		Source: &ir.ImageSource{Type: "url", URL: url},
	}
}

// parseDataURI splits "data:<media>;base64,<payload>" into its media
// type and payload.
func parseDataURI(uri string) (media, payload string, ok bool) {
	rest := strings.TrimPrefix(uri, "data:")
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return "", "", false
	}
	meta, data := rest[:comma], rest[comma+1:]
	meta = strings.TrimSuffix(meta, ";base64")
	return meta, data, true
}

// toolsFromOpenAI converts OpenAI function tools into IR tools.
func toolsFromOpenAI(tools []chatTool) []ir.Tool {
	out := make([]ir.Tool, 0, len(tools))
	for _, t := range tools {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		out = append(out, ir.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return out
}

// toolChoiceFromOpenAI maps OpenAI's tool_choice (string or object) to
// the IR's ToolChoice. Returns nil for an absent choice.
func toolChoiceFromOpenAI(raw json.RawMessage) (*ir.ToolChoice, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	if isJSONString(raw) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		switch s {
		case "auto":
			return &ir.ToolChoice{Type: ir.ToolChoiceAuto}, nil
		case "none":
			return &ir.ToolChoice{Type: ir.ToolChoiceNone}, nil
		case "required":
			return &ir.ToolChoice{Type: ir.ToolChoiceAny}, nil
		default:
			return &ir.ToolChoice{Type: ir.ToolChoiceAuto}, nil
		}
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("openai: decode tool_choice: %w", err)
	}
	return &ir.ToolChoice{Type: ir.ToolChoiceTool, Name: obj.Function.Name}, nil
}
