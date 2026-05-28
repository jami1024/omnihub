package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
)

func TestRequestFromOpenAI_Basic(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "be brief"},
			{"role": "user", "content": "hello"}
		],
		"temperature": 0.5,
		"max_tokens": 256,
		"stop": "STOP",
		"stream": true
	}`)

	req, err := RequestFromOpenAI(body)
	if err != nil {
		t.Fatalf("RequestFromOpenAI: %v", err)
	}
	if req.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", req.Model)
	}
	if !req.Stream {
		t.Errorf("stream = false, want true")
	}
	if req.MaxTokens != 256 {
		t.Errorf("max_tokens = %d, want 256", req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != 0.5 {
		t.Errorf("temperature = %v, want 0.5", req.Temperature)
	}
	if len(req.StopSequences) != 1 || req.StopSequences[0] != "STOP" {
		t.Errorf("stop = %v, want [STOP]", req.StopSequences)
	}
	if len(req.System) != 1 || req.System[0].Text != "be brief" {
		t.Errorf("system = %+v, want one block 'be brief'", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != ir.RoleUser {
		t.Fatalf("messages = %+v, want one user message", req.Messages)
	}
	if got := req.Messages[0].Content[0].Text; got != "hello" {
		t.Errorf("user text = %q, want hello", got)
	}
}

func TestRequestFromOpenAI_MaxCompletionTokensWins(t *testing.T) {
	body := []byte(`{"model":"o3","messages":[{"role":"user","content":"hi"}],"max_tokens":10,"max_completion_tokens":99}`)
	req, err := RequestFromOpenAI(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 99 {
		t.Errorf("max_tokens = %d, want 99 (max_completion_tokens wins)", req.MaxTokens)
	}
}

func TestRoundTrip_PreservesUnmodeledFields(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hi"}],
		"presence_penalty": 0.2,
		"frequency_penalty": 0.1,
		"seed": 42,
		"response_format": {"type": "json_object"},
		"user": "u-123"
	}`)

	req, err := RequestFromOpenAI(body)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RequestToOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"presence_penalty", "frequency_penalty", "seed", "response_format", "user"} {
		if _, ok := got[k]; !ok {
			t.Errorf("round-trip dropped unmodeled field %q", k)
		}
	}
	if string(got["seed"]) != "42" {
		t.Errorf("seed = %s, want 42", got["seed"])
	}
}

func TestRequestToOpenAI_InjectsIncludeUsageWhenStreaming(t *testing.T) {
	req := &ir.UnifiedRequest{
		Model:    "gpt-4o",
		Stream:   true,
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.TextBlock("hi")}}},
	}
	out, err := RequestToOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stream        bool `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Stream || !got.StreamOptions.IncludeUsage {
		t.Errorf("expected stream + stream_options.include_usage, got %+v", got)
	}
}

func TestRequestToOpenAI_NoStreamOptionsWhenNotStreaming(t *testing.T) {
	req := &ir.UnifiedRequest{
		Model:    "gpt-4o",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.TextBlock("hi")}}},
	}
	out, err := RequestToOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(out, &got)
	if _, ok := got["stream_options"]; ok {
		t.Errorf("stream_options should be absent for non-streaming request")
	}
	if _, ok := got["stream"]; ok {
		t.Errorf("stream should be absent for non-streaming request")
	}
}

func TestRoundTrip_Tools(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "weather?"}],
		"tools": [{
			"type": "function",
			"function": {
				"name": "get_weather",
				"description": "look up weather",
				"parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
			}
		}],
		"tool_choice": "required"
	}`)

	req, err := RequestFromOpenAI(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "get_weather" {
		t.Fatalf("tools = %+v", req.Tools)
	}
	if req.ToolChoice == nil || req.ToolChoice.Type != ir.ToolChoiceAny {
		t.Fatalf("tool_choice = %+v, want type=any (required)", req.ToolChoice)
	}

	out, err := RequestToOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		ToolChoice string `json:"tool_choice"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Function.Name != "get_weather" || got.Tools[0].Type != "function" {
		t.Errorf("tools round-trip = %+v", got.Tools)
	}
	if got.ToolChoice != "required" {
		t.Errorf("tool_choice = %q, want required", got.ToolChoice)
	}
}

func TestRoundTrip_ToolCallAndResult(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "weather in NYC?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"NYC\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "sunny"}
		]
	}`)

	req, err := RequestFromOpenAI(body)
	if err != nil {
		t.Fatal(err)
	}
	// assistant turn -> tool_use block
	asst := req.Messages[1]
	if asst.Role != ir.RoleAssistant || asst.Content[0].Type != ir.BlockToolUse {
		t.Fatalf("assistant turn = %+v", asst)
	}
	if asst.Content[0].ID != "call_1" || asst.Content[0].Name != "get_weather" {
		t.Errorf("tool_use = %+v", asst.Content[0])
	}
	// tool turn -> user message with tool_result block
	toolTurn := req.Messages[2]
	if toolTurn.Role != ir.RoleUser || toolTurn.Content[0].Type != ir.BlockToolResult {
		t.Fatalf("tool turn = %+v", toolTurn)
	}
	if toolTurn.Content[0].ToolUseID != "call_1" {
		t.Errorf("tool_result tool_use_id = %q, want call_1", toolTurn.Content[0].ToolUseID)
	}

	// round-trip back to OpenAI
	out, err := RequestToOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("got %d messages, want 3: %s", len(got.Messages), out)
	}
	if got.Messages[1].Role != "assistant" || len(got.Messages[1].ToolCalls) != 1 {
		t.Errorf("assistant round-trip = %+v", got.Messages[1])
	}
	if got.Messages[1].ToolCalls[0].Function.Arguments != `{"city":"NYC"}` {
		t.Errorf("arguments = %q", got.Messages[1].ToolCalls[0].Function.Arguments)
	}
	if got.Messages[2].Role != "tool" || got.Messages[2].ToolCallID != "call_1" {
		t.Errorf("tool round-trip = %+v", got.Messages[2])
	}
}

func TestRequestFromOpenAI_ImageDataURI(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": [
			{"type": "text", "text": "what is this"},
			{"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}
		]}]
	}`)

	req, err := RequestFromOpenAI(body)
	if err != nil {
		t.Fatal(err)
	}
	blocks := req.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	img := blocks[1]
	if img.Type != ir.BlockImage || img.Source == nil {
		t.Fatalf("image block = %+v", img)
	}
	if img.Source.Type != "base64" || img.Source.MediaType != "image/png" || img.Source.Data != "AAAA" {
		t.Errorf("image source = %+v", img.Source)
	}

	// round-trip the image back to a data URI
	out, err := RequestToOpenAI(req)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("invalid round-trip JSON")
	}
}

func TestResponseToIR(t *testing.T) {
	body := []byte(`{
		"id": "chatcmpl-1",
		"model": "gpt-4o-2024",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "hi there"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 5, "total_tokens": 17, "prompt_tokens_details": {"cached_tokens": 4}}
	}`)

	resp, err := ResponseToIR(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "chatcmpl-1" || resp.Model != "gpt-4o-2024" {
		t.Errorf("resp id/model = %q/%q", resp.ID, resp.Model)
	}
	if resp.StopReason != ir.StopReasonEndTurn {
		t.Errorf("stop_reason = %q, want end_turn", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hi there" {
		t.Errorf("content = %+v", resp.Content)
	}
	// prompt_tokens(12) includes cached(4) -> InputTokens 8, CacheRead 4
	if resp.Usage.InputTokens != 8 || resp.Usage.CacheReadInputTokens != 4 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestChunkToIR(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want ir.ChunkType
	}{
		{"text delta", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`, ir.ChunkContentBlockDelta},
		{"role start", `{"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`, ir.ChunkMessageStart},
		{"finish", `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, ir.ChunkMessageDelta},
		{"usage only", `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`, ir.ChunkMessageDelta},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunk, err := ChunkToIR([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if chunk.Type != tc.want {
				t.Errorf("type = %q, want %q", chunk.Type, tc.want)
			}
		})
	}
}

func TestStopField_StringAndArray(t *testing.T) {
	var single stopValue
	if err := json.Unmarshal([]byte(`"x"`), &single); err != nil || !reflect.DeepEqual([]string(single), []string{"x"}) {
		t.Errorf("single stop = %v err=%v", single, err)
	}
	var many stopValue
	if err := json.Unmarshal([]byte(`["a","b"]`), &many); err != nil || !reflect.DeepEqual([]string(many), []string{"a", "b"}) {
		t.Errorf("many stop = %v err=%v", many, err)
	}
}
