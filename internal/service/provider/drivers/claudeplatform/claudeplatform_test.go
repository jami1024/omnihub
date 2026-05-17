package claudeplatform_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/claudeplatform"
)

func newAccount(creds map[string]string) *provider.Account {
	return &provider.Account{Provider: "claude-platform", Credentials: creds}
}

func TestDriverIdentity(t *testing.T) {
	d := claudeplatform.New()
	if d.Name() != "claude-platform" {
		t.Errorf("Name: want claude-platform, got %q", d.Name())
	}
	if !d.Capabilities().Streaming || !d.Capabilities().Tools {
		t.Errorf("Capabilities should inherit Anthropic flags, got %+v", d.Capabilities())
	}
}

func TestBuildRequestRegionalEndpointAndHeaders(t *testing.T) {
	d := claudeplatform.New()
	req := &ir.UnifiedRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 100,
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{ir.TextBlock("hi")}},
		},
		AnthropicBeta: []string{"computer-use-2025-11-24"},
	}
	account := newAccount(map[string]string{
		"api_key":      "sk-aws-test",
		"aws_region":   "us-east-1",
		"workspace_id": "ws_abc",
	})

	httpReq, err := d.BuildRequest(context.Background(), req, account)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	wantURL := "https://aws-external-anthropic.us-east-1.api.aws/v1/messages"
	if got := httpReq.URL.String(); got != wantURL {
		t.Errorf("URL: want %s, got %s", wantURL, got)
	}
	if got := httpReq.Header.Get("x-api-key"); got != "sk-aws-test" {
		t.Errorf("x-api-key: want sk-aws-test, got %q", got)
	}
	if got := httpReq.Header.Get("anthropic-workspace-id"); got != "ws_abc" {
		t.Errorf("anthropic-workspace-id: want ws_abc, got %q", got)
	}
	if got := httpReq.Header.Get("anthropic-version"); got == "" {
		t.Errorf("anthropic-version header should be set")
	}
	if got := httpReq.Header.Get("anthropic-beta"); got != "computer-use-2025-11-24" {
		t.Errorf("anthropic-beta: want passthrough, got %q", got)
	}

	bodyBytes, _ := io.ReadAll(httpReq.Body)
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	for _, key := range []string{"anthropic_version", "anthropic_beta", "workspace_id"} {
		if _, ok := body[key]; ok {
			t.Errorf("body should not contain header-only key %q", key)
		}
	}
	if body["model"] != "claude-sonnet-4-5" {
		t.Errorf("body.model: want claude-sonnet-4-5, got %v", body["model"])
	}
}

func TestBuildRequestBaseURLOverride(t *testing.T) {
	d := claudeplatform.New()
	account := newAccount(map[string]string{
		"api_key":      "sk-aws-test",
		"workspace_id": "ws_abc",
	})
	account.BaseURL = "https://vpce.example.aws"

	httpReq, err := d.BuildRequest(context.Background(),
		&ir.UnifiedRequest{Model: "x", MaxTokens: 1},
		account)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got, want := httpReq.URL.String(), "https://vpce.example.aws/v1/messages"; got != want {
		t.Errorf("URL: want %s, got %s", want, got)
	}
}

func TestBuildRequestAlternativeWorkspaceKey(t *testing.T) {
	d := claudeplatform.New()
	account := newAccount(map[string]string{
		"api_key":          "sk-aws-test",
		"aws_region":       "eu-west-1",
		"aws_workspace_id": "ws_xyz", // alternate key
	})
	httpReq, err := d.BuildRequest(context.Background(),
		&ir.UnifiedRequest{Model: "x", MaxTokens: 1},
		account)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got := httpReq.Header.Get("anthropic-workspace-id"); got != "ws_xyz" {
		t.Errorf("anthropic-workspace-id: want ws_xyz, got %q", got)
	}
}

func TestBuildRequestRejectsMissingFields(t *testing.T) {
	d := claudeplatform.New()
	cases := []struct {
		name  string
		creds map[string]string
	}{
		{"no api_key", map[string]string{"aws_region": "us-east-1", "workspace_id": "ws"}},
		{"no aws_region", map[string]string{"api_key": "k", "workspace_id": "ws"}},
		{"no workspace_id", map[string]string{"api_key": "k", "aws_region": "us-east-1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := d.BuildRequest(context.Background(),
				&ir.UnifiedRequest{Model: "x", MaxTokens: 1},
				newAccount(c.creds))
			if err == nil {
				t.Errorf("expected error for missing credential")
			}
		})
	}
}

// Smoke test that ParseResponse and DecodeStream are correctly promoted
// from the embedded Anthropic driver (i.e. the composition works).
func TestPromotedMethods(t *testing.T) {
	d := claudeplatform.New()

	// ParseResponse (synthesise a minimal Anthropic JSON body).
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"id":"m1","role":"assistant","model":"x","content":[],"usage":{}}`)),
	}
	out, err := d.ParseResponse(resp)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if out.ID != "m1" {
		t.Errorf("ParseResponse: want id m1, got %q", out.ID)
	}

	// DecodeStream returns a non-nil iterator we can immediately close.
	iter := d.DecodeStream(io.NopCloser(strings.NewReader("")))
	if iter == nil {
		t.Fatalf("DecodeStream: nil iterator")
	}
	_ = iter.Close()

	// Capabilities: claude-platform inherits anthropic's flags.
	if !d.Capabilities().Chat {
		t.Errorf("Capabilities.Chat should be true")
	}
}

// Make sure the driver satisfies the provider.Driver interface at
// compile time.
var _ provider.Driver = (*claudeplatform.Driver)(nil)
