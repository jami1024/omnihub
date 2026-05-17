package forward_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
)

// upstreamServer mocks api.anthropic.com for both streaming and
// non-streaming endpoints.
func upstreamServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func anthropicAccount(baseURL string) *provider.Account {
	return &provider.Account{
		Provider:    "anthropic",
		BaseURL:     baseURL,
		Credentials: map[string]string{"api_key": "sk-ant-test"},
	}
}

func TestForwardNonStreamingHappyPath(t *testing.T) {
	srv := upstreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test" {
			t.Errorf("upstream saw x-api-key=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req_test")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"msg_1","role":"assistant","model":"x","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":5,"output_tokens":1}}`))
	})

	f := forward.New(srv.Client())
	rec := httptest.NewRecorder()

	result, err := f.Forward(
		context.Background(),
		rec,
		&ir.UnifiedRequest{Model: "claude-sonnet-4-5", MaxTokens: 100},
		anthropic.New(),
		anthropicAccount(srv.URL),
	)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", result.StatusCode)
	}
	if got := rec.Header().Get("x-request-id"); got != "req_test" {
		t.Errorf("x-request-id should pass through, got %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"msg_1"`) {
		t.Errorf("body missing upstream content: %s", rec.Body.String())
	}
}

func TestForwardStreamingFlushesEachEvent(t *testing.T) {
	sse := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	srv := upstreamServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, sse)
	})

	f := forward.New(srv.Client())
	rec := httptest.NewRecorder()

	result, err := f.Forward(
		context.Background(),
		rec,
		&ir.UnifiedRequest{Model: "claude-sonnet-4-5", MaxTokens: 100, Stream: true},
		anthropic.New(),
		anthropicAccount(srv.URL),
	)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", result.StatusCode)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	if xa := rec.Header().Get("X-Accel-Buffering"); xa != "no" {
		t.Errorf("X-Accel-Buffering: want no, got %q", xa)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"text":"Hello"`) {
		t.Errorf("body missing streamed text: %s", body)
	}
	// Verify the body preserves SSE structure (event/data/blank).
	if !strings.Contains(body, "event: content_block_delta") {
		t.Errorf("body missing event line: %s", body)
	}
}

func TestForwardErrorResponsePassthrough(t *testing.T) {
	srv := upstreamServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("retry-after", "30")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	})

	f := forward.New(srv.Client())
	rec := httptest.NewRecorder()

	result, err := f.Forward(
		context.Background(),
		rec,
		&ir.UnifiedRequest{Model: "claude-sonnet-4-5", MaxTokens: 100},
		anthropic.New(),
		anthropicAccount(srv.URL),
	)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if result.StatusCode != 429 {
		t.Errorf("status: want 429, got %d", result.StatusCode)
	}
	if got := rec.Header().Get("retry-after"); got != "30" {
		t.Errorf("retry-after should pass through, got %q", got)
	}
	if !strings.Contains(rec.Body.String(), "rate_limit_error") {
		t.Errorf("body missing upstream error: %s", rec.Body.String())
	}
}
