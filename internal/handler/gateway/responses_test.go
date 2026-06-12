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
	"github.com/jami1024/omnihub/internal/service/provider/drivers/codex"
)

func newResponsesHandler(t *testing.T, upstreamURL string) gin.HandlerFunc {
	t.Helper()
	res := &stubResolver{
		account: &provider.Account{
			ID:       1,
			Name:     "test-codex",
			Provider: "openai-codex",
			BaseURL:  upstreamURL + "/backend-api/codex/responses",
			AuthType: "imported_oauth",
			Credentials: map[string]string{
				"access_token": "at-test",
				"account_id":   "acct-1",
			},
		},
		driver: codex.New(),
	}
	return gateway.ResponsesHandler(
		forward.New(nil), res, nil, nil, pricing.Default(), nil, nil, nil, nil,
	)
}

func doResponsesRequest(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h(c)
	return rec
}

func TestResponsesHandler_SSEPassthrough(t *testing.T) {
	const sse = "event: response.created\n" +
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5-codex","status":"in_progress"}}` + "\n\n" +
		"event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"hello"}` + "\n\n" +
		"event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5-codex","status":"completed","usage":{"input_tokens":12,"output_tokens":3}}}` + "\n\n"

	var gotPath, gotAuth, gotAccountID, gotBeta string
	var gotBody map[string]json.RawMessage
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("chatgpt-account-id")
		gotBeta = r.Header.Get("OpenAI-Beta")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer upstream.Close()

	h := newResponsesHandler(t, upstream.URL)
	rec := doResponsesRequest(h, `{"model":"gpt-5-codex","stream":true,"store":true,"temperature":1,"prompt_cache_key":"s1","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body %s", rec.Code, rec.Body.String())
	}
	// Upstream bytes reach the client verbatim.
	if got := rec.Body.String(); got != sse {
		t.Fatalf("SSE not passed through verbatim:\n%s", got)
	}
	if gotPath != "/backend-api/codex/responses" {
		t.Fatalf("path: %s", gotPath)
	}
	if gotAuth != "Bearer at-test" || gotAccountID != "acct-1" || gotBeta != "responses=experimental" {
		t.Fatalf("headers: auth=%q account=%q beta=%q", gotAuth, gotAccountID, gotBeta)
	}
	if string(gotBody["store"]) != "false" {
		t.Fatalf("store not forced false: %s", gotBody["store"])
	}
	if _, ok := gotBody["temperature"]; ok {
		t.Fatal("temperature not stripped")
	}
	if _, ok := gotBody["input"]; !ok {
		t.Fatal("input lost in passthrough")
	}
}

func TestResponsesHandler_BadJSON(t *testing.T) {
	h := newResponsesHandler(t, "http://127.0.0.1:1")
	rec := doResponsesRequest(h, `{"stream":true}`) // missing model
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestModelsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	gateway.ModelsHandler(codex.KnownModels)(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var out struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Object != "list" || len(out.Data) != len(codex.KnownModels) {
		t.Fatalf("shape: %+v", out)
	}
}
