package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
)

func TestDriverNameAndCapabilities(t *testing.T) {
	d := anthropic.New()
	if d.Name() != "anthropic" {
		t.Errorf("Name: want anthropic, got %q", d.Name())
	}
	caps := d.Capabilities()
	if !caps.Chat || !caps.Streaming || !caps.Tools || !caps.Vision || !caps.Thinking {
		t.Errorf("Capabilities should advertise chat/streaming/tools/vision/thinking, got %+v", caps)
	}
}

func TestBuildRequestShape(t *testing.T) {
	d := anthropic.New()
	temp := 0.5
	req := &ir.UnifiedRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1024,
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.TextBlock("hello")}},
		},
		Temperature:   &temp,
		AnthropicBeta: []string{"computer-use-2025-11-24", "tool-search-tool-2025-10-19"},
	}
	account := &provider.Account{
		Provider:    "anthropic",
		Credentials: map[string]string{"api_key": "sk-ant-test"},
	}

	httpReq, err := d.BuildRequest(context.Background(), req, account)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	if httpReq.Method != http.MethodPost {
		t.Errorf("method: want POST, got %s", httpReq.Method)
	}
	if got := httpReq.URL.String(); got != "https://api.anthropic.com/v1/messages" {
		t.Errorf("URL: want default endpoint, got %s", got)
	}
	if got := httpReq.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Errorf("x-api-key: want sk-ant-test, got %q", got)
	}
	if got := httpReq.Header.Get("anthropic-version"); got != anthropic.DefaultAnthropicVersion {
		t.Errorf("anthropic-version: want %q, got %q", anthropic.DefaultAnthropicVersion, got)
	}
	if got := httpReq.Header.Get("anthropic-beta"); got != "computer-use-2025-11-24,tool-search-tool-2025-10-19" {
		t.Errorf("anthropic-beta: unexpected value %q", got)
	}

	bodyBytes, _ := io.ReadAll(httpReq.Body)
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	// Header-only fields must not appear in the body.
	for _, key := range []string{"anthropic_version", "anthropic_beta"} {
		if _, ok := body[key]; ok {
			t.Errorf("body should not contain %q, got: %s", key, bodyBytes)
		}
	}
	if body["model"] != "claude-sonnet-4-5" {
		t.Errorf("body.model: want claude-sonnet-4-5, got %v", body["model"])
	}
	if body["max_tokens"].(float64) != 1024 {
		t.Errorf("body.max_tokens: want 1024, got %v", body["max_tokens"])
	}
}

func TestBuildRequestUsesAccountBaseURL(t *testing.T) {
	d := anthropic.New()
	account := &provider.Account{
		BaseURL:     "https://my-proxy.example.com",
		Credentials: map[string]string{"api_key": "sk-ant-test"},
	}
	httpReq, err := d.BuildRequest(context.Background(), &ir.UnifiedRequest{Model: "x", MaxTokens: 1}, account)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	want := "https://my-proxy.example.com/v1/messages"
	if got := httpReq.URL.String(); got != want {
		t.Errorf("URL: want %s, got %s", want, got)
	}
}

func TestBuildRequestRejectsMissingCredentials(t *testing.T) {
	d := anthropic.New()
	_, err := d.BuildRequest(context.Background(), &ir.UnifiedRequest{Model: "x"}, &provider.Account{})
	if err == nil {
		t.Errorf("expected error when api_key is missing")
	}
}

func TestParseResponse(t *testing.T) {
	body := `{
		"id": "msg_01ABC",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-5-20250929",
		"content": [{"type": "text", "text": "Hi!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 12, "output_tokens": 4}
	}`
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	out, err := anthropic.New().ParseResponse(resp)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if out.ID != "msg_01ABC" {
		t.Errorf("ID: want msg_01ABC, got %s", out.ID)
	}
	if out.Role != ir.RoleAssistant {
		t.Errorf("Role: want assistant, got %s", out.Role)
	}
	if len(out.Content) != 1 || out.Content[0].Text != "Hi!" {
		t.Errorf("Content: unexpected %+v", out.Content)
	}
	if out.StopReason != ir.StopReasonEndTurn {
		t.Errorf("StopReason: want end_turn, got %s", out.StopReason)
	}
	if out.Usage.InputTokens != 12 || out.Usage.OutputTokens != 4 {
		t.Errorf("Usage: want (12,4), got (%d,%d)", out.Usage.InputTokens, out.Usage.OutputTokens)
	}
}

// fakeBody implements io.ReadCloser over a string, recording closes.
type fakeBody struct {
	*strings.Reader
	closed bool
}

func (f *fakeBody) Close() error {
	f.closed = true
	return nil
}

func TestDecodeStream(t *testing.T) {
	sse := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
	body := &fakeBody{Reader: strings.NewReader(sse)}

	it := anthropic.New().DecodeStream(body)
	defer it.Close()

	var chunks []*ir.UnifiedChunk
	for {
		c, err := it.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		chunks = append(chunks, c)
	}

	if got, want := len(chunks), 7; got != want {
		t.Fatalf("chunk count: want %d, got %d", want, got)
	}
	if chunks[0].Type != ir.ChunkMessageStart || chunks[0].Message == nil {
		t.Errorf("chunk[0]: want message_start with Message, got %+v", chunks[0])
	}
	if chunks[2].Type != ir.ChunkContentBlockDelta || chunks[2].Delta == nil || chunks[2].Delta.Text != "Hello" {
		t.Errorf("chunk[2]: want content_block_delta text=Hello, got %+v", chunks[2])
	}
	if chunks[5].Type != ir.ChunkMessageDelta || chunks[5].Delta == nil || chunks[5].Delta.StopReason != ir.StopReasonEndTurn {
		t.Errorf("chunk[5]: want message_delta stop_reason=end_turn, got %+v", chunks[5])
	}

	if err := it.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if !body.closed {
		t.Errorf("Close should close underlying body")
	}
}
