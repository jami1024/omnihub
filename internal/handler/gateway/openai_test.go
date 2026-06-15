package gateway_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/handler/gateway"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/openai"
	"github.com/jami1024/omnihub/internal/service/resolver"
)

// stubResolver always returns the configured account+driver, or an error.
type stubResolver struct {
	account *provider.Account
	driver  provider.Driver
	err     error
}

func (s *stubResolver) ResolveForProviders(string, []string, []int64) (*provider.Account, provider.Driver, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.account, s.driver, nil
}

func newOpenAIHandler(t *testing.T, upstreamURL string, resErr error) gin.HandlerFunc {
	t.Helper()
	res := &stubResolver{
		account: &provider.Account{
			ID:          1,
			Name:        "test-openai",
			Provider:    "openai",
			BaseURL:     upstreamURL,
			Credentials: map[string]string{"api_key": "sk-test"},
		},
		driver: openai.New(),
		err:    resErr,
	}
	return gateway.OpenAIChatCompletionsHandler(
		forward.New(nil), res, nil, nil, pricing.Default(), nil, nil, nil, nil, nil, nil,
	)
}

func doRequest(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h(c)
	return rec
}

func TestOpenAIHandler_NonStreamPassthrough(t *testing.T) {
	const upstreamBody = `{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer upstream.Close()

	h := newOpenAIHandler(t, upstream.URL, nil)
	rec := doRequest(h, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("upstream path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("upstream auth = %q", gotAuth)
	}
	// Response is passed through verbatim.
	if rec.Body.String() != upstreamBody {
		t.Errorf("body not passed through verbatim:\n got %s\nwant %s", rec.Body.String(), upstreamBody)
	}
}

func TestOpenAIHandler_StreamPassthroughInjectsIncludeUsage(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		"",
		`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		"",
		`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	var upstreamReqBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamReqBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer upstream.Close()

	h := newOpenAIHandler(t, upstream.URL, nil)
	rec := doRequest(h, `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// The driver must have injected stream_options.include_usage upstream.
	var sent map[string]any
	if err := json.Unmarshal(upstreamReqBody, &sent); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	so, ok := sent["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Errorf("upstream missing stream_options.include_usage: %s", upstreamReqBody)
	}
	// SSE is forwarded verbatim, including the [DONE] sentinel.
	if !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Errorf("client did not receive [DONE]: %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}
}

func TestOpenAIHandler_NoUpstreamErrorEnvelope(t *testing.T) {
	h := newOpenAIHandler(t, "http://unused", resolver.ErrNoUpstream)
	rec := doRequest(h, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("error body not JSON: %v (%s)", err, rec.Body.String())
	}
	if env.Error.Type != "no_upstream_available" || env.Error.Message == "" {
		t.Errorf("openai error envelope = %+v", env.Error)
	}
}

func TestOpenAIHandler_BadJSON(t *testing.T) {
	h := newOpenAIHandler(t, "http://unused", nil)
	rec := doRequest(h, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("error body not JSON: %v", err)
	}
	if env.Error.Type != "invalid_request_error" {
		t.Errorf("error type = %q, want invalid_request_error", env.Error.Type)
	}
}
