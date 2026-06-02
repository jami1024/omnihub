package forward_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
	"github.com/jami1024/omnihub/internal/service/usage"
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
		usage.Anthropic,
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

func TestForwardCustomHeadersAppliedButCannotOverrideInvariants(t *testing.T) {
	srv := upstreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Org-Id"); got != "acme" {
			t.Errorf("custom header X-Org-Id: want acme, got %q", got)
		}
		// The custom header tried to set these; the gateway's invariants
		// must win regardless.
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding must stay identity, got %q", got)
		}
		if got := r.Header.Get("X-Forwarded-For"); got != "" {
			t.Errorf("X-Forwarded-For must be stripped, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"msg_1","role":"assistant","model":"x","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	})

	acct := anthropicAccount(srv.URL)
	acct.CustomHeaders = map[string]string{
		"X-Org-Id":        "acme",
		"Accept-Encoding": "gzip",      // must be overridden back to identity
		"X-Forwarded-For": "9.9.9.9",   // must be stripped
	}

	f := forward.New(srv.Client())
	rec := httptest.NewRecorder()
	if _, err := f.Forward(
		context.Background(), rec,
		&ir.UnifiedRequest{Model: "claude-sonnet-4-5", MaxTokens: 100},
		anthropic.New(), acct, usage.Anthropic,
	); err != nil {
		t.Fatalf("Forward: %v", err)
	}
}

func TestForwardEndpointFailover(t *testing.T) {
	// First endpoint returns 503 (retriable) → forwarder must fail over
	// to the second, whose 200 is what the client sees.
	var firstHits, secondHits int
	bad := upstreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		firstHits++
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	})
	good := upstreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		secondHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"msg_ok","role":"assistant","model":"x","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	})

	acct := anthropicAccount(bad.URL)
	acct.Endpoints = []string{good.URL} // failover target

	f := forward.New(bad.Client())
	resp, _, err := f.Dispatch(context.Background(),
		&ir.UnifiedRequest{Model: "claude-sonnet-4-5", MaxTokens: 100},
		anthropic.New(), acct)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("want 200 from failover endpoint, got %d", resp.StatusCode)
	}
	if firstHits != 1 || secondHits != 1 {
		t.Errorf("expected 1 hit each (first=%d second=%d)", firstHits, secondHits)
	}
}

func TestForwardEndpointFailoverAllRetriable(t *testing.T) {
	// Both endpoints 503 → Dispatch returns the LAST response (still
	// retriable) so the caller's inter-account loop can take over.
	srv := upstreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	})
	acct := anthropicAccount(srv.URL)
	acct.Endpoints = []string{srv.URL + "/b"} // distinct URL, same server → still 503

	f := forward.New(srv.Client())
	resp, _, err := f.Dispatch(context.Background(),
		&ir.UnifiedRequest{Model: "claude-sonnet-4-5", MaxTokens: 100},
		anthropic.New(), acct)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("want last 503 returned, got %d", resp.StatusCode)
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
		usage.Anthropic,
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

// failingResponseWriter wraps httptest.ResponseRecorder and starts
// returning an error from Write after failAfter successful calls.
// Flush is a no-op so forwardSSE's http.Flusher assertion passes.
type failingResponseWriter struct {
	*httptest.ResponseRecorder
	failAfter  int
	writeCount int
}

func (f *failingResponseWriter) Write(b []byte) (int, error) {
	f.writeCount++
	if f.writeCount > f.failAfter {
		return 0, errors.New("client gone")
	}
	return f.ResponseRecorder.Write(b)
}

func (*failingResponseWriter) Flush() {}

func TestForwardStreamingDrainsAfterClientDisconnect(t *testing.T) {
	// SSE stream where message_start carries the input/cache counts
	// and a placeholder output_tokens=1, then message_delta carries
	// the authoritative output_tokens=237. The client "disconnects"
	// on the very first write, so the drain MUST run to land 237 in
	// the result.
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_drain","model":"claude-haiku-4-5","usage":{"input_tokens":50,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":237}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	srv := upstreamServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, sse)
	})

	rec := &failingResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		failAfter:        0, // first Write fails — simulates immediate disconnect
	}

	f := forward.New(srv.Client())
	result, err := f.Forward(
		context.Background(),
		rec,
		&ir.UnifiedRequest{Model: "claude-haiku-4-5", MaxTokens: 100, Stream: true},
		anthropic.New(),
		anthropicAccount(srv.URL),
		usage.Anthropic,
	)

	// The write failure must surface so the handler can log it.
	if err == nil {
		t.Fatal("expected error from client disconnect")
	}
	if !strings.Contains(err.Error(), "write client") {
		t.Errorf("error should mention client write, got: %v", err)
	}

	// The whole point of this test: drain captured the
	// authoritative output_tokens from message_delta. Without the
	// drain we would see 1 (the message_start placeholder).
	if got := result.Usage.OutputTokens; got != 237 {
		t.Errorf("output_tokens = %d, want 237 (drain should have captured message_delta)", got)
	}
	if got := result.Usage.InputTokens; got != 50 {
		t.Errorf("input_tokens = %d, want 50", got)
	}
	if got := result.Usage.UpstreamRequestID; got != "msg_drain" {
		t.Errorf("upstream_request_id = %q, want msg_drain", got)
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
		usage.Anthropic,
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
	if !strings.Contains(string(result.ErrorBody), "rate_limit_error") {
		t.Errorf("ErrorBody not captured: %q", result.ErrorBody)
	}
}

func TestForwardErrorBodyCappedForLargeUpstream(t *testing.T) {
	// Upstream emits more bytes than the capture cap. The full body
	// must still reach the client; the capture is the first slice.
	big := strings.Repeat("x", 16<<10) // 16 KiB > 8 KiB cap
	srv := upstreamServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, big)
	})

	f := forward.New(srv.Client())
	rec := httptest.NewRecorder()

	result, err := f.Forward(
		context.Background(),
		rec,
		&ir.UnifiedRequest{Model: "claude-sonnet-4-5", MaxTokens: 100},
		anthropic.New(),
		anthropicAccount(srv.URL),
		usage.Anthropic,
	)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if rec.Body.Len() != len(big) {
		t.Errorf("client body truncated: want %d bytes, got %d", len(big), rec.Body.Len())
	}
	if len(result.ErrorBody) != 8<<10 {
		t.Errorf("ErrorBody should be capped at 8KiB, got %d", len(result.ErrorBody))
	}
}

func TestForwardErrorAugmentsToolName(t *testing.T) {
	// Upstream rejects tools[1]; the augmented body should call out
	// the tool by name so the user knows which one is broken.
	srv := upstreamServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"tools.1.custom.input_schema: Input does not match the expected shape."}}`)
	})

	req := &ir.UnifiedRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 100,
		Tools: []ir.Tool{
			{Name: "get_weather", InputSchema: []byte(`{}`)},
			{Name: "search_docs", InputSchema: []byte(`{}`)},
		},
	}

	f := forward.New(srv.Client())
	rec := httptest.NewRecorder()

	result, err := f.Forward(context.Background(), rec, req, anthropic.New(), anthropicAccount(srv.URL), usage.Anthropic)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if result.StatusCode != 400 {
		t.Errorf("status: want 400, got %d", result.StatusCode)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "[tool=search_docs]") {
		t.Errorf("client body missing tool-name prefix: %s", body)
	}
	if !strings.Contains(body, "tools.1.custom.input_schema") {
		t.Errorf("client body lost original message: %s", body)
	}
	// Augmented body is also what gets stored for diagnostics.
	if !strings.Contains(string(result.ErrorBody), "[tool=search_docs]") {
		t.Errorf("captured body not augmented: %s", result.ErrorBody)
	}
}

func TestIsThinkingSignatureError(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "anthropic signature error",
			body: `{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid ` + "`signature`" + ` in ` + "`thinking`" + ` block"}}`,
			want: true,
		},
		{
			name: "prompt too long is not signature",
			body: `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 207925 tokens > 200000 maximum"}}`,
			want: false,
		},
		{
			name: "tool schema error is not signature",
			body: `{"type":"error","error":{"type":"invalid_request_error","message":"tools.0.custom.input_schema: Input does not match the expected shape."}}`,
			want: false,
		},
		{
			name: "empty body",
			body: "",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := forward.IsThinkingSignatureError([]byte(tc.body)); got != tc.want {
				t.Errorf("want %v, got %v for body %s", tc.want, got, tc.body)
			}
		})
	}
}

func TestForwardErrorAugmentationSkippedWhenNoMatch(t *testing.T) {
	// Non-tool error message: augmentation must be a no-op so other
	// error categories aren't mangled.
	original := `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 207925 tokens > 200000 maximum"}}`
	srv := upstreamServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = io.WriteString(w, original)
	})

	req := &ir.UnifiedRequest{
		Model:     "claude-haiku-4-5",
		MaxTokens: 100,
		Tools:     []ir.Tool{{Name: "irrelevant", InputSchema: []byte(`{}`)}},
	}

	f := forward.New(srv.Client())
	rec := httptest.NewRecorder()

	if _, err := f.Forward(context.Background(), rec, req, anthropic.New(), anthropicAccount(srv.URL), usage.Anthropic); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if rec.Body.String() != original {
		t.Errorf("body should be untouched, got %s", rec.Body.String())
	}
}
