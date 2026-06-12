package claudesub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
)

func subAccount() *provider.Account {
	return &provider.Account{
		ID:       1,
		Name:     "claude-max-1",
		Provider: DriverName,
		AuthType: "imported_oauth",
		Credentials: map[string]string{
			"access_token": "at-1",
		},
	}
}

func claudeCodeRequest() *ir.UnifiedRequest {
	return &ir.UnifiedRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 1024,
		System: []ir.ContentBlock{
			{Type: ir.BlockText, Text: claudeCodeSystemPrompt},
			{Type: ir.BlockText, Text: "Extra project context."},
		},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.BlockText, Text: "hi"}}},
		},
		AnthropicBeta: []string{"interleaved-thinking-2025-05-14"},
	}
}

func buildAndDecode(t *testing.T, req *ir.UnifiedRequest) (map[string]json.RawMessage, http.Header) {
	t.Helper()
	httpReq, err := New().BuildRequest(context.Background(), req, subAccount())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := io.ReadAll(httpReq.Body)
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	return body, httpReq.Header
}

func TestBuildRequestOAuthHeaders(t *testing.T) {
	req := claudeCodeRequest()
	_, headers := buildAndDecode(t, req)

	if got := headers.Get("Authorization"); got != "Bearer at-1" {
		t.Fatalf("authorization: %q", got)
	}
	if got := headers.Get("x-api-key"); got != "" {
		t.Fatalf("x-api-key must be absent on OAuth traffic, got %q", got)
	}
	if got := headers.Get("anthropic-beta"); got != "oauth-2025-04-20,interleaved-thinking-2025-05-14" {
		t.Fatalf("beta merge: %q", got)
	}
	if got := headers.Get("User-Agent"); got != userAgent {
		t.Fatalf("user-agent: %q", got)
	}
}

func TestSystemPromptPreservedWhenAlreadyClaudeCode(t *testing.T) {
	body, _ := buildAndDecode(t, claudeCodeRequest())
	var system []ir.ContentBlock
	if err := json.Unmarshal(body["system"], &system); err != nil {
		t.Fatalf("system: %v", err)
	}
	if len(system) != 2 || system[0].Text != claudeCodeSystemPrompt || system[1].Text != "Extra project context." {
		t.Fatalf("system must be untouched for Claude Code clients: %+v", system)
	}
}

func TestSystemPromptPrependedWhenMissing(t *testing.T) {
	req := claudeCodeRequest()
	req.System = []ir.ContentBlock{{Type: ir.BlockText, Text: "You are a helpful bot."}}
	body, _ := buildAndDecode(t, req)
	var system []ir.ContentBlock
	if err := json.Unmarshal(body["system"], &system); err != nil {
		t.Fatalf("system: %v", err)
	}
	if len(system) != 2 || system[0].Text != claudeCodeSystemPrompt || system[1].Text != "You are a helpful bot." {
		t.Fatalf("identity must be prepended, client content preserved: %+v", system)
	}
	// The IR itself must stay untouched (failover retries rebuild from it).
	if len(req.System) != 1 || req.System[0].Text != "You are a helpful bot." {
		t.Fatalf("BuildRequest mutated the shared IR: %+v", req.System)
	}
}

func TestBuildRequestRequiresToken(t *testing.T) {
	account := subAccount()
	account.Credentials = map[string]string{}
	if _, err := New().BuildRequest(context.Background(), claudeCodeRequest(), account); err == nil {
		t.Fatal("missing access_token must fail")
	}
}

func TestInheritedWireFormat(t *testing.T) {
	d := New()
	if !d.Capabilities().Thinking {
		t.Fatal("capabilities should be inherited from the anthropic driver")
	}
	if d.Name() != "claude-subscription" {
		t.Fatalf("name: %s", d.Name())
	}
}
