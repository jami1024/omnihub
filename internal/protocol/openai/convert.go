package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jami1024/omnihub/internal/ir"
)

// RequestToOpenAI rebuilds an OpenAI Chat Completions body from IR. Only
// the fields the IR transforms are regenerated (model, messages, tools,
// tool_choice, stream); every other field is restored verbatim from the
// passthrough blob captured by RequestFromOpenAI. When streaming, it
// injects stream_options.include_usage so the final chunk carries token
// counts (OpenAI omits usage from streams otherwise).
func RequestToOpenAI(req *ir.UnifiedRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("openai: nil request")
	}
	out := map[string]json.RawMessage{}

	model, err := json.Marshal(req.Model)
	if err != nil {
		return nil, err
	}
	out["model"] = model

	msgs, err := json.Marshal(messagesToOpenAI(req))
	if err != nil {
		return nil, fmt.Errorf("openai: encode messages: %w", err)
	}
	out["messages"] = msgs

	if req.Stream {
		out["stream"] = json.RawMessage("true")
		so, err := json.Marshal(streamOptions{IncludeUsage: true})
		if err != nil {
			return nil, err
		}
		out["stream_options"] = so
	}

	if tools := toolsToOpenAI(req.Tools); len(tools) > 0 {
		tb, err := json.Marshal(tools)
		if err != nil {
			return nil, fmt.Errorf("openai: encode tools: %w", err)
		}
		out["tools"] = tb
	}

	if req.ToolChoice != nil {
		tc, err := toolChoiceToOpenAI(req.ToolChoice)
		if err != nil {
			return nil, err
		}
		out["tool_choice"] = tc
	}

	if blob, ok := req.Extensions[ExtensionPassthroughKey]; ok {
		var pt map[string]json.RawMessage
		if err := json.Unmarshal(blob, &pt); err == nil {
			for k, v := range pt {
				if _, exists := out[k]; !exists {
					out[k] = v
				}
			}
		}
	}

	return json.Marshal(out)
}

// messagesToOpenAI flattens the IR's separate system prompt and turns
// back into a single OpenAI messages array.
func messagesToOpenAI(req *ir.UnifiedRequest) []chatMessage {
	var out []chatMessage
	if len(req.System) > 0 {
		out = append(out, chatMessage{
			Role:    "system",
			Content: jsonString(joinTextBlocks(req.System)),
		})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case ir.RoleAssistant:
			out = append(out, assistantToOpenAI(m))
		default: // user (and any tool_result-bearing turn)
			toolMsgs, userMsg, hasUser := userToOpenAI(m)
			out = append(out, toolMsgs...)
			if hasUser {
				out = append(out, userMsg)
			}
		}
	}
	return out
}

// assistantToOpenAI converts an IR assistant turn (text + tool_use,
// thinking dropped) into one OpenAI assistant message.
func assistantToOpenAI(m ir.Message) chatMessage {
	cm := chatMessage{Role: "assistant"}
	var text strings.Builder
	for _, b := range m.Content {
		switch b.Type {
		case ir.BlockText:
			text.WriteString(b.Text)
		case ir.BlockToolUse:
			args := string(b.Input)
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			cm.ToolCalls = append(cm.ToolCalls, toolCall{
				ID:       b.ID,
				Type:     "function",
				Function: functionCall{Name: b.Name, Arguments: args},
			})
		}
	}
	if text.Len() > 0 {
		cm.Content = jsonString(text.String())
	}
	return cm
}

// userToOpenAI converts an IR user turn into zero or more OpenAI tool
// messages (one per tool_result block) plus an optional user message for
// any remaining text/image content.
func userToOpenAI(m ir.Message) (toolMsgs []chatMessage, userMsg chatMessage, hasUser bool) {
	var nonTool []ir.ContentBlock
	for _, b := range m.Content {
		if b.Type == ir.BlockToolResult {
			toolMsgs = append(toolMsgs, chatMessage{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    jsonString(joinTextBlocks(b.ResultContent)),
			})
			continue
		}
		nonTool = append(nonTool, b)
	}
	if len(nonTool) == 0 {
		return toolMsgs, chatMessage{}, false
	}
	return toolMsgs, chatMessage{Role: "user", Content: userContentToOpenAI(nonTool)}, true
}

// userContentToOpenAI renders user content as an OpenAI string when it
// is a single text block, or a parts array when it mixes text/images.
func userContentToOpenAI(blocks []ir.ContentBlock) json.RawMessage {
	if len(blocks) == 1 && blocks[0].Type == ir.BlockText {
		return jsonString(blocks[0].Text)
	}
	parts := make([]contentPart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case ir.BlockText:
			parts = append(parts, contentPart{Type: "text", Text: b.Text})
		case ir.BlockImage:
			if b.Source != nil {
				parts = append(parts, contentPart{
					Type:     "image_url",
					ImageURL: &imageURL{URL: imageSourceURL(b.Source)},
				})
			}
		}
	}
	raw, _ := json.Marshal(parts)
	return raw
}

// imageSourceURL renders an IR image source back to an OpenAI image URL,
// reconstructing a data URI for base64 sources.
func imageSourceURL(src *ir.ImageSource) string {
	if src.Type == "base64" {
		return "data:" + src.MediaType + ";base64," + src.Data
	}
	return src.URL
}

// toolsToOpenAI converts IR custom tools into OpenAI function tools.
// Server-side (Anthropic-only) tools are skipped.
func toolsToOpenAI(tools []ir.Tool) []chatTool {
	out := make([]chatTool, 0, len(tools))
	for _, t := range tools {
		if t.Type != "" && t.Type != "custom" {
			continue
		}
		out = append(out, chatTool{
			Type: "function",
			Function: functionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

// toolChoiceToOpenAI maps the IR ToolChoice back to OpenAI's wire form.
func toolChoiceToOpenAI(tc *ir.ToolChoice) (json.RawMessage, error) {
	switch tc.Type {
	case ir.ToolChoiceAuto:
		return jsonString("auto"), nil
	case ir.ToolChoiceNone:
		return jsonString("none"), nil
	case ir.ToolChoiceAny:
		return jsonString("required"), nil
	case ir.ToolChoiceTool:
		obj := map[string]any{
			"type":     "function",
			"function": map[string]string{"name": tc.Name},
		}
		return json.Marshal(obj)
	default:
		return jsonString("auto"), nil
	}
}

// joinTextBlocks concatenates the text of every text block, newline-
// separated. A single block returns its text verbatim (the common case,
// so system prompts round-trip exactly).
func joinTextBlocks(blocks []ir.ContentBlock) string {
	texts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == ir.BlockText {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// plainText extracts the text from an OpenAI message content that is a
// string, a parts array, or null.
func plainText(content json.RawMessage) (string, error) {
	if isJSONNull(content) {
		return "", nil
	}
	if isJSONString(content) {
		var s string
		if err := json.Unmarshal(content, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	var parts []contentPart
	if err := json.Unmarshal(content, &parts); err != nil {
		return "", fmt.Errorf("decode content: %w", err)
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String(), nil
}

func jsonString(s string) json.RawMessage {
	raw, _ := json.Marshal(s)
	return raw
}

func isJSONString(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '"'
}

func isJSONNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}
