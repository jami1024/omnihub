package ir_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
)

func TestUnifiedRequestRoundTrip(t *testing.T) {
	temp := 0.7
	req := ir.UnifiedRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1024,
		Stream:    true,
		System: []ir.ContentBlock{
			{Type: ir.BlockText, Text: "You are a helpful assistant.",
				CacheControl: &ir.CacheControl{Type: "ephemeral"}},
		},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{
				ir.TextBlock("hello"),
			}},
		},
		Temperature: &temp,
		Thinking: &ir.ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 2000,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ir.UnifiedRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(req, got) {
		t.Fatalf("round-trip mismatch\nwant: %+v\n got: %+v", req, got)
	}
}

func TestContentBlockToolUseShape(t *testing.T) {
	block := ir.ContentBlock{
		Type:  ir.BlockToolUse,
		ID:    "toolu_01ABC",
		Name:  "get_weather",
		Input: json.RawMessage(`{"city":"Tokyo"}`),
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Anthropic wire shape is flat: id/name/input at the top of the block,
	// not nested under a "tool_use" key.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, key := range []string{"id", "name", "input"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected top-level key %q in marshalled tool_use block, got: %s", key, data)
		}
	}
}

func TestMessageContentAcceptsString(t *testing.T) {
	const wire = `{"role":"user","content":"hello"}`

	var msg ir.Message
	if err := json.Unmarshal([]byte(wire), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Role != ir.RoleUser {
		t.Errorf("Role: want %q, got %q", ir.RoleUser, msg.Role)
	}
	want := []ir.ContentBlock{ir.TextBlock("hello")}
	if !reflect.DeepEqual(msg.Content, want) {
		t.Errorf("Content: want %+v, got %+v", want, msg.Content)
	}
}

func TestMessageContentAcceptsArray(t *testing.T) {
	const wire = `{"role":"user","content":[{"type":"text","text":"hello"}]}`

	var msg ir.Message
	if err := json.Unmarshal([]byte(wire), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []ir.ContentBlock{{Type: ir.BlockText, Text: "hello"}}
	if !reflect.DeepEqual(msg.Content, want) {
		t.Errorf("Content: want %+v, got %+v", want, msg.Content)
	}
}

func TestToolResultContentAcceptsString(t *testing.T) {
	const wire = `{"type":"tool_result","tool_use_id":"toolu_01","content":"42"}`

	var block ir.ContentBlock
	if err := json.Unmarshal([]byte(wire), &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block.Type != ir.BlockToolResult {
		t.Errorf("Type: want %q, got %q", ir.BlockToolResult, block.Type)
	}
	want := ir.ToolResultContent{ir.TextBlock("42")}
	if !reflect.DeepEqual(block.ResultContent, want) {
		t.Errorf("ResultContent: want %+v, got %+v", want, block.ResultContent)
	}
}

func TestToolResultContentAcceptsArray(t *testing.T) {
	const wire = `{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"text","text":"42"}]}`

	var block ir.ContentBlock
	if err := json.Unmarshal([]byte(wire), &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := ir.ToolResultContent{{Type: ir.BlockText, Text: "42"}}
	if !reflect.DeepEqual(block.ResultContent, want) {
		t.Errorf("ResultContent: want %+v, got %+v", want, block.ResultContent)
	}
}

func TestToolRoundTripCustom(t *testing.T) {
	// Custom tool: no `type` on the wire, input_schema is required.
	const wire = `{"name":"get_weather","description":"Look up the weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}`

	var tool ir.Tool
	if err := json.Unmarshal([]byte(wire), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool.Type != "" {
		t.Errorf("Type should stay empty for custom tools, got %q", tool.Type)
	}
	if tool.Name != "get_weather" {
		t.Errorf("Name: %q", tool.Name)
	}
	if len(tool.Extra) != 0 {
		t.Errorf("Extra should be empty for custom tools, got %v", tool.Extra)
	}
	got, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(canonicalize(t, got), canonicalize(t, []byte(wire))) {
		t.Errorf("round-trip mismatch\n  want %s\n  got  %s", wire, got)
	}
}

func TestToolRoundTripServerSideWebSearch(t *testing.T) {
	// Server-side web_search: discriminator type + max_uses +
	// allowed_domains + user_location must all survive. This is the
	// fix for "tools.0.custom.input_schema: ..." 400s.
	const wire = `{"type":"web_search_20250305","name":"web_search","max_uses":5,"allowed_domains":["example.com"],"user_location":{"type":"approximate","city":"San Francisco","country":"US"}}`

	var tool ir.Tool
	if err := json.Unmarshal([]byte(wire), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool.Type != "web_search_20250305" {
		t.Errorf("Type: want %q, got %q", "web_search_20250305", tool.Type)
	}
	if tool.Name != "web_search" {
		t.Errorf("Name: %q", tool.Name)
	}
	if len(tool.InputSchema) != 0 {
		t.Errorf("InputSchema should be absent for server-side tools, got %q", tool.InputSchema)
	}
	for _, k := range []string{"max_uses", "allowed_domains", "user_location"} {
		if _, ok := tool.Extra[k]; !ok {
			t.Errorf("Extra missing key %q (have %v)", k, tool.Extra)
		}
	}
	got, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(canonicalize(t, got), canonicalize(t, []byte(wire))) {
		t.Errorf("round-trip mismatch\n  want %s\n  got  %s", wire, got)
	}
}

func TestToolExtraDoesNotClobberTypedFields(t *testing.T) {
	// Defensive: if a caller stuffs "name" into Extra, marshalling
	// must keep the typed Name as authoritative.
	tool := ir.Tool{
		Name: "real_name",
		Extra: map[string]json.RawMessage{
			"name": json.RawMessage(`"hijacked"`),
			"max_uses": json.RawMessage(`3`),
		},
	}
	got, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundtrip map[string]json.RawMessage
	if err := json.Unmarshal(got, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(roundtrip["name"]) != `"real_name"` {
		t.Errorf("Extra must not clobber Name, got %s", roundtrip["name"])
	}
	if string(roundtrip["max_uses"]) != "3" {
		t.Errorf("Extra max_uses should pass through, got %s", roundtrip["max_uses"])
	}
}

// canonicalize re-parses and re-encodes JSON so two equivalent
// objects with different key orderings compare equal byte-wise.
func canonicalize(t *testing.T, data []byte) []byte {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("canonicalize unmarshal: %v\n  %s", err, data)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("canonicalize marshal: %v", err)
	}
	return out
}

func TestTextBlockEmitsTextEvenWhenEmpty(t *testing.T) {
	// Anthropic rejects text blocks where the "text" key is absent
	// even if the value is an empty string — this happens in practice
	// when a tool_result wraps an empty-string text response.
	cases := []struct {
		name  string
		block ir.ContentBlock
		want  string
	}{
		{
			name:  "non-empty text",
			block: ir.ContentBlock{Type: ir.BlockText, Text: "hello"},
			want:  `{"type":"text","text":"hello"}`,
		},
		{
			name:  "empty text still emits key",
			block: ir.ContentBlock{Type: ir.BlockText, Text: ""},
			want:  `{"type":"text","text":""}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.block)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("marshal:\n  want %s\n  got  %s", tc.want, got)
			}
		})
	}
}

func TestToolResultPreservesEmptyTextInnerBlock(t *testing.T) {
	// End-to-end: a tool_result containing an empty-string text block
	// must serialize with the inner text field present, otherwise
	// Anthropic returns "messages.N.content.M.tool_result.content.0
	// .text.text: Field required".
	block := ir.ContentBlock{
		Type:      ir.BlockToolResult,
		ToolUseID: "toolu_01",
		ResultContent: ir.ToolResultContent{
			{Type: ir.BlockText, Text: ""},
		},
	}
	got, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"text","text":""}]}`
	if string(got) != want {
		t.Errorf("marshal:\n  want %s\n  got  %s", want, got)
	}
}

func TestThinkingBlockEmitsRequiredFields(t *testing.T) {
	cases := []struct {
		name  string
		block ir.ContentBlock
		want  string
	}{
		{
			name:  "thinking with both fields populated",
			block: ir.ContentBlock{Type: ir.BlockThinking, Thinking: "step", Signature: "sig"},
			want:  `{"type":"thinking","thinking":"step","signature":"sig"}`,
		},
		{
			// Anthropic rejects assistant-replay thinking blocks where
			// the "thinking" key is absent — even an empty string must
			// survive marshalling.
			name:  "thinking with empty text still emits key",
			block: ir.ContentBlock{Type: ir.BlockThinking, Signature: "sig"},
			want:  `{"type":"thinking","thinking":"","signature":"sig"}`,
		},
		{
			name:  "redacted_thinking emits data even when empty",
			block: ir.ContentBlock{Type: ir.BlockRedactedThinking},
			want:  `{"type":"redacted_thinking","data":""}`,
		},
		{
			name:  "redacted_thinking with payload",
			block: ir.ContentBlock{Type: ir.BlockRedactedThinking, Data: "opaque"},
			want:  `{"type":"redacted_thinking","data":"opaque"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.block)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("marshal:\n  want %s\n  got  %s", tc.want, got)
			}
		})
	}
}

func TestUsageAdd(t *testing.T) {
	u := ir.Usage{InputTokens: 100, OutputTokens: 50}
	u.Add(ir.Usage{InputTokens: 20, CacheReadInputTokens: 5})

	if u.InputTokens != 120 {
		t.Errorf("InputTokens: want 120, got %d", u.InputTokens)
	}
	if u.OutputTokens != 50 {
		t.Errorf("OutputTokens: want 50, got %d", u.OutputTokens)
	}
	if u.CacheReadInputTokens != 5 {
		t.Errorf("CacheReadInputTokens: want 5, got %d", u.CacheReadInputTokens)
	}
	if u.TotalTokens() != 170 {
		t.Errorf("TotalTokens: want 170, got %d", u.TotalTokens())
	}
}

func TestChunkSerializesDelta(t *testing.T) {
	chunk := ir.UnifiedChunk{
		Type:  ir.ChunkContentBlockDelta,
		Index: 0,
		Delta: &ir.Delta{Type: "text_delta", Text: "hello"},
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ir.UnifiedChunk
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Type != ir.ChunkContentBlockDelta {
		t.Errorf("Type: want %q, got %q", ir.ChunkContentBlockDelta, got.Type)
	}
	if got.Delta == nil || got.Delta.Text != "hello" {
		t.Errorf("Delta.Text: want 'hello', got %+v", got.Delta)
	}
}
