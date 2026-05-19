package ir_test

import (
	"reflect"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
)

func TestRectifyThinkingBlocksConvertsToText(t *testing.T) {
	req := &ir.UnifiedRequest{
		Model:    "claude-opus-4-7",
		Thinking: &ir.ThinkingConfig{Type: "enabled", BudgetTokens: 2000},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.TextBlock("hi")}},
			{Role: ir.RoleAssistant, Content: []ir.ContentBlock{
				{Type: ir.BlockThinking, Thinking: "let me think", Signature: "sig"},
				ir.TextBlock("hello"),
			}},
		},
	}

	got := ir.RectifyThinkingBlocks(req)

	if got == req {
		t.Fatal("rectifier must return a new request, not the input pointer")
	}
	if got.Thinking != nil {
		t.Errorf("Thinking should be cleared, got %+v", got.Thinking)
	}
	if req.Thinking == nil {
		t.Error("original request must not be mutated")
	}
	if len(got.Messages) != 2 {
		t.Fatalf("Messages: want 2, got %d", len(got.Messages))
	}

	wantAssistant := []ir.ContentBlock{
		ir.TextBlock("let me think"),
		ir.TextBlock("hello"),
	}
	if !reflect.DeepEqual(got.Messages[1].Content, wantAssistant) {
		t.Errorf("assistant content not rectified\n  want %+v\n  got  %+v",
			wantAssistant, got.Messages[1].Content)
	}
}

func TestRectifyThinkingBlocksDropsRedacted(t *testing.T) {
	req := &ir.UnifiedRequest{
		Messages: []ir.Message{
			{Role: ir.RoleAssistant, Content: []ir.ContentBlock{
				{Type: ir.BlockRedactedThinking, Data: "opaque"},
				ir.TextBlock("answer"),
			}},
		},
	}

	got := ir.RectifyThinkingBlocks(req)

	want := []ir.ContentBlock{ir.TextBlock("answer")}
	if !reflect.DeepEqual(got.Messages[0].Content, want) {
		t.Errorf("redacted_thinking should be dropped\n  want %+v\n  got  %+v",
			want, got.Messages[0].Content)
	}
}

func TestRectifyThinkingBlocksDropsEmptyThinking(t *testing.T) {
	// An empty thinking block carries no recoverable content and
	// should disappear cleanly rather than becoming a 0-length text
	// block (which Anthropic rejects).
	req := &ir.UnifiedRequest{
		Messages: []ir.Message{
			{Role: ir.RoleAssistant, Content: []ir.ContentBlock{
				{Type: ir.BlockThinking, Thinking: "", Signature: "sig"},
				ir.TextBlock("done"),
			}},
		},
	}

	got := ir.RectifyThinkingBlocks(req)

	want := []ir.ContentBlock{ir.TextBlock("done")}
	if !reflect.DeepEqual(got.Messages[0].Content, want) {
		t.Errorf("empty thinking block should be dropped\n  want %+v\n  got  %+v",
			want, got.Messages[0].Content)
	}
}

func TestRectifyThinkingBlocksPreservesOtherTypes(t *testing.T) {
	// Tool blocks, images, text — none of these are touched by the
	// rectifier. Only thinking variants get rewritten.
	req := &ir.UnifiedRequest{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{
				{Type: ir.BlockText, Text: "look at this image"},
				{Type: ir.BlockImage, Source: &ir.ImageSource{Type: "base64", MediaType: "image/png", Data: "x"}},
			}},
			{Role: ir.RoleAssistant, Content: []ir.ContentBlock{
				{Type: ir.BlockToolUse, ID: "toolu_01", Name: "get_weather"},
			}},
		},
	}

	got := ir.RectifyThinkingBlocks(req)

	if !reflect.DeepEqual(got.Messages, req.Messages) {
		t.Errorf("non-thinking content must pass through unchanged")
	}
}

func TestRectifyThinkingBlocksNilInput(t *testing.T) {
	if got := ir.RectifyThinkingBlocks(nil); got != nil {
		t.Errorf("nil in → nil out, got %+v", got)
	}
}
