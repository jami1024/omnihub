package openai

import (
	"encoding/json"
	"fmt"

	"github.com/jami1024/omnihub/internal/ir"
)

// ResponseToIR converts a non-streaming OpenAI Chat Completions response
// body into IR. Used by the openai driver's ParseResponse; not on the
// matched-pair pass-through hot path.
func ResponseToIR(body []byte) (*ir.UnifiedResponse, error) {
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}
	out := &ir.UnifiedResponse{
		ID:    cr.ID,
		Type:  "message",
		Role:  ir.RoleAssistant,
		Model: cr.Model,
	}
	if len(cr.Choices) > 0 {
		ch := cr.Choices[0]
		if ch.Message != nil {
			blocks, err := assistantContentBlocks(*ch.Message)
			if err != nil {
				return nil, err
			}
			out.Content = blocks
		}
		out.StopReason = stopReasonFromFinish(ch.FinishReason)
	}
	if cr.Usage != nil {
		out.Usage = usageToIR(cr.Usage)
	}
	return out, nil
}

// usageToIR maps OpenAI token accounting onto the IR's Anthropic-shaped
// usage. OpenAI's prompt_tokens includes cached tokens, so the cached
// portion is subtracted out of InputTokens to match Anthropic semantics
// (InputTokens = fresh, CacheReadInputTokens = reused).
func usageToIR(u *chatUsage) ir.Usage {
	out := ir.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		out.CacheReadInputTokens = u.PromptTokensDetails.CachedTokens
		out.InputTokens = u.PromptTokens - u.PromptTokensDetails.CachedTokens
	}
	return out
}

// ChunkToIR converts one OpenAI streaming chunk into an IR chunk. The
// mapping is best-effort: OpenAI's flat delta stream does not carry the
// content_block_start/stop framing the Anthropic-shaped IR expects, so
// this collapses each OpenAI event to the closest single IR chunk. It is
// used only by the openai driver's DecodeStream (off the pass-through
// hot path); a faithful re-render is deferred to the cross-protocol
// milestone.
func ChunkToIR(chunk []byte) (*ir.UnifiedChunk, error) {
	var sc streamChunk
	if err := json.Unmarshal(chunk, &sc); err != nil {
		return nil, fmt.Errorf("openai: decode chunk: %w", err)
	}

	if len(sc.Choices) == 0 {
		if sc.Usage != nil {
			u := usageToIR(sc.Usage)
			return &ir.UnifiedChunk{Type: ir.ChunkMessageDelta, Usage: &u}, nil
		}
		return &ir.UnifiedChunk{Type: ir.ChunkPing}, nil
	}

	ch := sc.Choices[0]
	if ch.FinishReason != "" {
		out := &ir.UnifiedChunk{
			Type:  ir.ChunkMessageDelta,
			Delta: &ir.Delta{StopReason: stopReasonFromFinish(ch.FinishReason)},
		}
		if sc.Usage != nil {
			u := usageToIR(sc.Usage)
			out.Usage = &u
		}
		return out, nil
	}

	if ch.Delta != nil {
		if len(ch.Delta.ToolCalls) > 0 {
			return &ir.UnifiedChunk{
				Type:  ir.ChunkContentBlockDelta,
				Index: ch.Index,
				Delta: &ir.Delta{Type: "input_json_delta", PartialJSON: ch.Delta.ToolCalls[0].Function.Arguments},
			}, nil
		}
		if len(ch.Delta.Content) > 0 && !isJSONNull(ch.Delta.Content) {
			text, _ := plainText(ch.Delta.Content)
			if text != "" {
				return &ir.UnifiedChunk{
					Type:  ir.ChunkContentBlockDelta,
					Index: ch.Index,
					Delta: &ir.Delta{Type: "text_delta", Text: text},
				}, nil
			}
		}
		if ch.Delta.Role != "" {
			return &ir.UnifiedChunk{
				Type:    ir.ChunkMessageStart,
				Message: &ir.UnifiedResponse{ID: sc.ID, Model: sc.Model, Role: ir.RoleAssistant},
			}, nil
		}
	}

	return &ir.UnifiedChunk{Type: ir.ChunkPing}, nil
}

// stopReasonFromFinish maps an OpenAI finish_reason to the IR StopReason.
func stopReasonFromFinish(finish string) ir.StopReason {
	switch finish {
	case "stop":
		return ir.StopReasonEndTurn
	case "length":
		return ir.StopReasonMaxTokens
	case "tool_calls", "function_call":
		return ir.StopReasonToolUse
	case "content_filter":
		return ir.StopReasonEndTurn
	default:
		return ""
	}
}
