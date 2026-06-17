package codex

import (
	"context"
	"encoding/json"
	"io"
	"strings"
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
	if h := httpReq.Header.Get("User-Agent"); !strings.HasPrefix(h, "codex_cli_rs/") {
		t.Fatalf("user-agent must mimic codex CLI, got %q", h)
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
	// instructions: client sent none → the official Codex base prompt is
	// injected (non-empty, opens with the Codex identity line).
	var ins string
	if err := json.Unmarshal(body["instructions"], &ins); err != nil {
		t.Fatalf("instructions: %v", err)
	}
	if !strings.HasPrefix(ins, "You are Codex") {
		t.Fatalf("instructions must default to the codex base prompt, got %q", ins[:min(40, len(ins))])
	}
	for _, k := range []string{"temperature", "top_p", "max_output_tokens"} {
		if _, ok := body[k]; ok {
			t.Fatalf("%s must be stripped", k)
		}
	}
	// gpt-5-codex is normalised to a backend-accepted slug.
	if string(body["model"]) != `"gpt-5.3-codex"` {
		t.Fatalf("model should normalise gpt-5-codex → gpt-5.3-codex, got %s", body["model"])
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

// TestNonStreamRequest pins the codex backend's hard requirement that
// every run streams: even a stream:false client request is dispatched
// with stream:true and an SSE Accept header (the handler de-streams the
// response). See WriteResponsesAggregated.
func TestNonStreamRequest(t *testing.T) {
	req, _, err := protoopenai.RequestFromResponses([]byte(`{"model":"gpt-5","stream":false}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	httpReq, err := New().BuildRequest(context.Background(), req, codexAccount())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if h := httpReq.Header.Get("Accept"); h != "text/event-stream" {
		t.Fatalf("accept must force SSE even for non-stream client: %q", h)
	}
	raw, _ := io.ReadAll(httpReq.Body)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(raw, &body)
	if string(body["stream"]) != "true" {
		t.Fatalf("stream must be forced true, got %s", body["stream"])
	}
	// gpt-5 normalises to gpt-5.4.
	if string(body["model"]) != `"gpt-5.4"` {
		t.Fatalf("model should normalise gpt-5 → gpt-5.4, got %s", body["model"])
	}
}

func TestNormalizeModel(t *testing.T) {
	cases := map[string]string{
		"gpt-5":             "gpt-5.4",
		"gpt-5-codex":       "gpt-5.3-codex",
		"gpt-5.1-codex":     "gpt-5.3-codex",
		"gpt-5.3-codex":     "gpt-5.3-codex",
		"gpt-5.4":           "gpt-5.4",
		"codex-mini-latest": "gpt-5.3-codex",
		"some-future-model": "some-future-model", // unknown passes through
	}
	for in, want := range cases {
		if got := normalizeModel(in); got != want {
			t.Errorf("normalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientInstructionsPreserved(t *testing.T) {
	// A client that sends its own non-empty instructions keeps them.
	req, _, err := protoopenai.RequestFromResponses([]byte(
		`{"model":"gpt-5","instructions":"You are Codex, custom build.","input":[]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	httpReq, err := New().BuildRequest(context.Background(), req, codexAccount())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := io.ReadAll(httpReq.Body)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(raw, &body)
	var ins string
	_ = json.Unmarshal(body["instructions"], &ins)
	if ins != "You are Codex, custom build." {
		t.Fatalf("client instructions must be preserved, got %q", ins)
	}
}

func TestStripsAllUnsupportedFields(t *testing.T) {
	req, _, err := protoopenai.RequestFromResponses([]byte(`{
		"model":"gpt-5","temperature":1,"top_p":0.5,"max_output_tokens":100,
		"max_completion_tokens":50,"frequency_penalty":0.1,"presence_penalty":0.2,
		"user":"u","metadata":{"a":1},"prompt_cache_retention":"24h",
		"safety_identifier":"s","stream_options":{"x":1},"input":[]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	httpReq, err := New().BuildRequest(context.Background(), req, codexAccount())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := io.ReadAll(httpReq.Body)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(raw, &body)
	for _, f := range unsupportedFields {
		if _, ok := body[f]; ok {
			t.Errorf("field %q must be stripped", f)
		}
	}
}
