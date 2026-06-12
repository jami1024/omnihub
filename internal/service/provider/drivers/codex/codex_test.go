package codex

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	protoopenai "github.com/jami1024/omnihub/internal/protocol/openai"
	"github.com/jami1024/omnihub/internal/service/provider"
)

func codexAccount() *provider.Account {
	return &provider.Account{
		ID:       1,
		Name:     "codex-1",
		Provider: DriverName,
		AuthType: "imported_oauth",
		Credentials: map[string]string{
			"access_token": "at-1",
			"account_id":   "acct-uuid",
		},
	}
}

const sampleResponses = `{
	"model": "gpt-5-codex",
	"stream": true,
	"store": true,
	"temperature": 0.7,
	"top_p": 0.9,
	"max_output_tokens": 4096,
	"prompt_cache_key": "sess-1",
	"input": [{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],
	"tools": [{"type":"function","name":"shell"}],
	"reasoning": {"effort":"medium","summary":"auto"}
}`

func buildSample(t *testing.T) (map[string]json.RawMessage, *provider.Account) {
	t.Helper()
	req, affinity, err := protoopenai.RequestFromResponses([]byte(sampleResponses))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if affinity != "sess-1" {
		t.Fatalf("affinity: %q", affinity)
	}
	account := codexAccount()
	httpReq, err := New().BuildRequest(context.Background(), req, account)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if got := httpReq.URL.String(); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("url: %s", got)
	}
	if h := httpReq.Header.Get("Authorization"); h != "Bearer at-1" {
		t.Fatalf("auth header: %q", h)
	}
	if h := httpReq.Header.Get("chatgpt-account-id"); h != "acct-uuid" {
		t.Fatalf("account header: %q", h)
	}
	if h := httpReq.Header.Get("OpenAI-Beta"); h != "responses=experimental" {
		t.Fatalf("beta header: %q", h)
	}
	if h := httpReq.Header.Get("originator"); h != originatorValue {
		t.Fatalf("originator header: %q", h)
	}
	if h := httpReq.Header.Get("Accept"); h != "text/event-stream" {
		t.Fatalf("accept header for stream: %q", h)
	}

	raw, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	return body, account
}

func TestBuildRequestAdjustsPayload(t *testing.T) {
	body, _ := buildSample(t)

	if string(body["store"]) != "false" {
		t.Fatalf("store must be forced false, got %s", body["store"])
	}
	if string(body["instructions"]) != `""` {
		t.Fatalf("instructions must default to empty string, got %s", body["instructions"])
	}
	for _, k := range []string{"temperature", "top_p", "max_output_tokens"} {
		if _, ok := body[k]; ok {
			t.Fatalf("%s must be stripped", k)
		}
	}
	if string(body["model"]) != `"gpt-5-codex"` {
		t.Fatalf("model: %s", body["model"])
	}
	if string(body["stream"]) != "true" {
		t.Fatalf("stream: %s", body["stream"])
	}
	// Pass-through fields stay byte-identical.
	for _, k := range []string{"input", "tools", "reasoning", "prompt_cache_key"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("passthrough field %s lost", k)
		}
	}
}

func TestBuildRequestDoesNotMutateSharedPayload(t *testing.T) {
	req, _, err := protoopenai.RequestFromResponses([]byte(sampleResponses))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := New().BuildRequest(context.Background(), req, codexAccount()); err != nil {
		t.Fatalf("build: %v", err)
	}
	// A failover retry rebuilds from the same Extensions blob: the
	// original must still carry the client's store/temperature values.
	var original map[string]json.RawMessage
	if err := json.Unmarshal(req.Extensions[protoopenai.ExtensionResponsesKey], &original); err != nil {
		t.Fatalf("reparse original: %v", err)
	}
	if string(original["store"]) != "true" || string(original["temperature"]) != "0.7" {
		t.Fatal("BuildRequest mutated the shared passthrough payload")
	}
}

func TestBuildRequestRequiresToken(t *testing.T) {
	req, _, err := protoopenai.RequestFromResponses([]byte(`{"model":"gpt-5"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	account := codexAccount()
	account.Credentials = map[string]string{}
	if _, err := New().BuildRequest(context.Background(), req, account); err == nil {
		t.Fatal("missing access_token must fail")
	}
}

func TestEndpointURLOverride(t *testing.T) {
	d := New()
	a := codexAccount()
	a.BaseURL = "https://mirror.example"
	if got := d.endpointURL(a); got != "https://mirror.example/backend-api/codex/responses" {
		t.Fatalf("base override: %s", got)
	}
	a.BaseURL = "https://mirror.example/custom/responses"
	if got := d.endpointURL(a); got != "https://mirror.example/custom/responses" {
		t.Fatalf("verbatim override: %s", got)
	}
}

func TestNonStreamRequest(t *testing.T) {
	req, _, err := protoopenai.RequestFromResponses([]byte(`{"model":"gpt-5","stream":false}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	httpReq, err := New().BuildRequest(context.Background(), req, codexAccount())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if h := httpReq.Header.Get("Accept"); h != "application/json" {
		t.Fatalf("accept for non-stream: %q", h)
	}
	raw, _ := io.ReadAll(httpReq.Body)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(raw, &body)
	if _, ok := body["stream"]; ok {
		t.Fatal("stream:false must be omitted, not sent")
	}
}
