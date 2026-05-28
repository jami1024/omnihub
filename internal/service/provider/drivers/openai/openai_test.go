package openai_test

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
	"github.com/jami1024/omnihub/internal/service/provider/drivers/openai"
)

func TestDriverNameAndCapabilities(t *testing.T) {
	d := openai.New()
	if d.Name() != "openai" {
		t.Errorf("Name: want openai, got %q", d.Name())
	}
	caps := d.Capabilities()
	if !caps.Chat || !caps.Streaming || !caps.Tools || !caps.Vision {
		t.Errorf("Capabilities should advertise chat/streaming/tools/vision, got %+v", caps)
	}
	if caps.Thinking {
		t.Errorf("Capabilities should not advertise thinking, got %+v", caps)
	}
}

func TestBuildRequestShape(t *testing.T) {
	d := openai.New()
	req := &ir.UnifiedRequest{
		Model:    "gpt-4o",
		Stream:   true,
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.TextBlock("hello")}}},
	}
	account := &provider.Account{
		Provider:    "openai",
		Credentials: map[string]string{"api_key": "sk-test", "organization": "org-1", "project": "proj-1"},
	}

	httpReq, err := d.BuildRequest(context.Background(), req, account)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if httpReq.Method != http.MethodPost {
		t.Errorf("method: want POST, got %s", httpReq.Method)
	}
	if got := httpReq.URL.String(); got != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("URL: want default endpoint, got %s", got)
	}
	if got := httpReq.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization: want 'Bearer sk-test', got %q", got)
	}
	if got := httpReq.Header.Get("OpenAI-Organization"); got != "org-1" {
		t.Errorf("OpenAI-Organization: want org-1, got %q", got)
	}
	if got := httpReq.Header.Get("OpenAI-Project"); got != "proj-1" {
		t.Errorf("OpenAI-Project: want proj-1, got %q", got)
	}

	bodyBytes, _ := io.ReadAll(httpReq.Body)
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if body["model"] != "gpt-4o" {
		t.Errorf("body.model: want gpt-4o, got %v", body["model"])
	}
	so, ok := body["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Errorf("body.stream_options.include_usage should be true, got %v", body["stream_options"])
	}
}

func TestEndpointNormalization(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"", "https://api.openai.com/v1/chat/completions"},
		{"https://api.deepseek.com", "https://api.deepseek.com/v1/chat/completions"},
		{"https://api.deepseek.com/", "https://api.deepseek.com/v1/chat/completions"},
		{"https://host/v1", "https://host/v1/chat/completions"},
		{"https://gw.internal/proxy/chat/completions", "https://gw.internal/proxy/chat/completions"},
	}
	d := openai.New()
	for _, tc := range cases {
		account := &provider.Account{
			BaseURL:     tc.base,
			Credentials: map[string]string{"api_key": "sk-test"},
		}
		httpReq, err := d.BuildRequest(context.Background(), &ir.UnifiedRequest{Model: "x"}, account)
		if err != nil {
			t.Fatalf("BuildRequest(base=%q): %v", tc.base, err)
		}
		if got := httpReq.URL.String(); got != tc.want {
			t.Errorf("base=%q: URL = %s, want %s", tc.base, got, tc.want)
		}
	}
}

func TestBuildRequestRejectsMissingCredentials(t *testing.T) {
	_, err := openai.New().BuildRequest(context.Background(), &ir.UnifiedRequest{Model: "x"}, &provider.Account{})
	if err == nil {
		t.Errorf("expected error when api_key is missing")
	}
}

func TestParseResponse(t *testing.T) {
	body := `{
		"id": "chatcmpl-9",
		"model": "gpt-4o-2024",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hi!"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 4, "total_tokens": 16}
	}`
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
	out, err := openai.New().ParseResponse(resp)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if out.ID != "chatcmpl-9" || out.Role != ir.RoleAssistant {
		t.Errorf("id/role = %q/%s", out.ID, out.Role)
	}
	if len(out.Content) != 1 || out.Content[0].Text != "Hi!" {
		t.Errorf("content = %+v", out.Content)
	}
	if out.StopReason != ir.StopReasonEndTurn {
		t.Errorf("stop_reason = %q, want end_turn", out.StopReason)
	}
	if out.Usage.InputTokens != 12 || out.Usage.OutputTokens != 4 {
		t.Errorf("usage = %+v", out.Usage)
	}
}

type fakeBody struct {
	*strings.Reader
	closed bool
}

func (f *fakeBody) Close() error { f.closed = true; return nil }

func TestDecodeStream(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		"",
		`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		"",
		`data: {"id":"c1","choices":[{"index":0,"delta":{"content":" world"}}]}`,
		"",
		`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	body := &fakeBody{Reader: strings.NewReader(sse)}

	it := openai.New().DecodeStream(body)
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

	if got, want := len(chunks), 5; got != want {
		t.Fatalf("chunk count: want %d, got %d", want, got)
	}
	if chunks[0].Type != ir.ChunkMessageStart {
		t.Errorf("chunk[0]: want message_start, got %s", chunks[0].Type)
	}
	if chunks[1].Type != ir.ChunkContentBlockDelta || chunks[1].Delta == nil || chunks[1].Delta.Text != "Hello" {
		t.Errorf("chunk[1]: want text delta Hello, got %+v", chunks[1])
	}
	if chunks[3].Type != ir.ChunkMessageDelta || chunks[3].Delta == nil || chunks[3].Delta.StopReason != ir.StopReasonEndTurn {
		t.Errorf("chunk[3]: want message_delta stop end_turn, got %+v", chunks[3])
	}
	if chunks[4].Type != ir.ChunkMessageDelta || chunks[4].Usage == nil || chunks[4].Usage.OutputTokens != 2 {
		t.Errorf("chunk[4]: want usage chunk, got %+v", chunks[4])
	}

	if !body.closed {
		_ = it.Close()
	}
	if !body.closed {
		t.Errorf("Close should close underlying body")
	}
}
