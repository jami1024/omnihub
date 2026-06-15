package upstreamauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCodexBeginAuth(t *testing.T) {
	resp, err := NewCodexOAuth().BeginAuth(context.Background(), &BeginAuthRequest{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if resp.CodeVerifier == "" || resp.State == "" {
		t.Fatal("verifier and state must be set")
	}
	u, err := url.Parse(resp.AuthorizeURL)
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	q := u.Query()
	if u.Host != "auth.openai.com" || u.Path != "/oauth/authorize" {
		t.Fatalf("authorize endpoint: %s", u.String())
	}
	if q.Get("client_id") != codexClientID || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("params: %v", q)
	}
	if q.Get("state") != resp.State {
		t.Fatal("state must match")
	}
	if q.Get("code_challenge") != codeChallengeS256(resp.CodeVerifier) {
		t.Fatal("challenge must be S256 of the verifier")
	}
}

func TestCodexExchangeCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "the-code" ||
			r.Form.Get("code_verifier") != "the-verifier" || r.Form.Get("client_id") != codexClientID {
			t.Errorf("unexpected form: %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-new", "refresh_token": "rt-new", "expires_in": 3600,
		})
	}))
	defer srv.Close()

	p := &CodexOAuth{TokenURL: srv.URL}
	bundle, err := p.ExchangeCallback(context.Background(),
		&CallbackRequest{Code: "the-code", CodeVerifier: "the-verifier"})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if bundle.Credentials[credAccessToken] != "at-new" || bundle.Credentials[credRefreshToken] != "rt-new" {
		t.Fatalf("creds: %+v", bundle.Credentials)
	}
	if bundle.Credentials[credSource] != "codex_oauth_login" {
		t.Fatalf("source should mark a login: %s", bundle.Credentials[credSource])
	}

	// Missing code/verifier is rejected without a network call.
	if _, err := p.ExchangeCallback(context.Background(), &CallbackRequest{Code: "c"}); err == nil {
		t.Fatal("missing verifier must fail")
	}
}

func TestClaudeBeginAuth(t *testing.T) {
	resp, err := NewClaudeOAuth().BeginAuth(context.Background(), &BeginAuthRequest{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u, _ := url.Parse(resp.AuthorizeURL)
	q := u.Query()
	if u.Host != "claude.ai" || u.Path != "/oauth/authorize" {
		t.Fatalf("authorize endpoint: %s", u.String())
	}
	if q.Get("client_id") != claudeClientID || q.Get("code_challenge_method") != "S256" || q.Get("code") != "true" {
		t.Fatalf("params: %v", q)
	}
	if q.Get("code_challenge") != codeChallengeS256(resp.CodeVerifier) {
		t.Fatal("challenge must be S256 of the verifier")
	}
}

func TestClaudeExchangeCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["grant_type"] != "authorization_code" || in["code"] != "the-code" ||
			in["code_verifier"] != "the-verifier" || in["client_id"] != claudeClientID {
			t.Errorf("unexpected body: %v", in)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-c", "refresh_token": "rt-c", "expires_in": 28800, "account_type": "claude_max",
		})
	}))
	defer srv.Close()

	p := &ClaudeOAuth{TokenURL: srv.URL}
	bundle, err := p.ExchangeCallback(context.Background(),
		&CallbackRequest{Code: "the-code", CodeVerifier: "the-verifier", State: "s"})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if bundle.Credentials[credAccessToken] != "at-c" || bundle.Credentials[credPlan] != "claude_max" {
		t.Fatalf("creds: %+v", bundle.Credentials)
	}
	if bundle.Credentials[credSource] != "claude_oauth_login" {
		t.Fatalf("source: %s", bundle.Credentials[credSource])
	}
}
