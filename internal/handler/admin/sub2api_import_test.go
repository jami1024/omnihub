package admin

import (
	"errors"
	"testing"
)

func TestMapSub2APIAccount_OpenAIOAuth(t *testing.T) {
	exp := int64(1745587200)
	sa := sub2apiAccount{
		Name:        "chatgpt-plus",
		Platform:    "openai",
		Type:        "oauth",
		Concurrency: 5,
		Priority:    10,
		ExpiresAt:   &exp,
		Credentials: map[string]any{
			"access_token":       "at-123",
			"refresh_token":      "rt-456",
			"id_token":           "id-789",
			"chatgpt_account_id": "acc-uuid",
			"plan_type":          "plus",
			"email":              "u@example.com",
		},
	}
	p, err := mapSub2APIAccount(sa, "http://user:pass@proxy:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Provider != "openai-codex" || p.AuthType != "imported_oauth" || p.AuthPlugin != "codex-oauth" {
		t.Fatalf("wrong provider mapping: %+v", p)
	}
	if p.Credentials["account_id"] != "acc-uuid" {
		t.Errorf("chatgpt_account_id should map to account_id, got %q", p.Credentials["account_id"])
	}
	if p.Credentials["plan"] != "plus" {
		t.Errorf("plan_type should map to plan, got %q", p.Credentials["plan"])
	}
	if p.Credentials["refresh_token"] != "rt-456" {
		t.Errorf("refresh_token missing: %q", p.Credentials["refresh_token"])
	}
	if p.Credentials["expires_at"] != "1745587200" {
		t.Errorf("expires_at not carried: %q", p.Credentials["expires_at"])
	}
	if p.Credentials["source"] != "sub2api_import" {
		t.Errorf("source tag missing: %q", p.Credentials["source"])
	}
	if p.ProxyURL != "http://user:pass@proxy:8080" {
		t.Errorf("proxy url not attached: %q", p.ProxyURL)
	}
	if p.MaxConcurrency != 5 || p.Priority != 10 {
		t.Errorf("concurrency/priority not mapped: %+v", p)
	}
}

func TestMapSub2APIAccount_NestedTokens(t *testing.T) {
	// Native ~/.codex/auth.json layout: tokens nested under "tokens".
	sa := sub2apiAccount{
		Name:     "codex-nested",
		Platform: "openai",
		Type:     "oauth",
		Credentials: map[string]any{
			"tokens": map[string]any{
				"access_token":  "at-n",
				"refresh_token": "rt-n",
				"account_id":    "acc-n",
			},
		},
	}
	p, err := mapSub2APIAccount(sa, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Credentials["refresh_token"] != "rt-n" || p.Credentials["account_id"] != "acc-n" {
		t.Errorf("nested tokens not read: %+v", p.Credentials)
	}
}

func TestMapSub2APIAccount_AnthropicOAuth(t *testing.T) {
	sa := sub2apiAccount{
		Name:     "claude-pro",
		Platform: "anthropic",
		Type:     "oauth",
		Credentials: map[string]any{
			"access_token":  "cat-1",
			"refresh_token": "crt-1",
		},
	}
	p, err := mapSub2APIAccount(sa, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Provider != "claude-subscription" || p.AuthPlugin != "claude-oauth" {
		t.Fatalf("wrong claude mapping: %+v", p)
	}
}

func TestMapSub2APIAccount_APIKey(t *testing.T) {
	sa := sub2apiAccount{
		Name:        "claude-key",
		Platform:    "anthropic",
		Type:        "api_key",
		Credentials: map[string]any{"api_key": "sk-ant-xyz"},
	}
	p, err := mapSub2APIAccount(sa, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Provider != "anthropic" || p.AuthType != "api_key" || p.Credentials["api_key"] != "sk-ant-xyz" {
		t.Fatalf("wrong api_key mapping: %+v", p)
	}
}

func TestMapSub2APIAccount_MissingRefreshToken(t *testing.T) {
	sa := sub2apiAccount{
		Name:        "no-rt",
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]any{"access_token": "at-only"},
	}
	if _, err := mapSub2APIAccount(sa, ""); err == nil {
		t.Fatal("expected error for oauth account without refresh_token")
	}
}

func TestMapSub2APIAccount_UnsupportedPlatform(t *testing.T) {
	sa := sub2apiAccount{
		Name:        "gemini-acc",
		Platform:    "gemini",
		Type:        "api_key",
		Credentials: map[string]any{"api_key": "AIza..."},
	}
	if _, err := mapSub2APIAccount(sa, ""); !errors.Is(err, errSub2APIUnsupported) {
		t.Fatalf("expected errSub2APIUnsupported, got %v", err)
	}
}

func TestMapSub2APIAccount_DisabledStatus(t *testing.T) {
	no := false
	sa := sub2apiAccount{
		Name:        "paused",
		Platform:    "anthropic",
		Type:        "api_key",
		Status:      "disabled",
		Schedulable: &no,
		Credentials: map[string]any{"api_key": "sk"},
	}
	p, err := mapSub2APIAccount(sa, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Enabled {
		t.Error("disabled/non-schedulable account should import disabled")
	}
}

func TestBuildProxyURL(t *testing.T) {
	cases := []struct {
		px   sub2apiProxy
		want string
	}{
		{sub2apiProxy{Protocol: "http", Host: "h", Port: 8080, Username: "u", Password: "p"}, "http://u:p@h:8080"},
		{sub2apiProxy{Protocol: "socks5", Host: "h", Port: 1080}, "socks5://h:1080"},
		{sub2apiProxy{Host: "h", Port: 3128}, "http://h:3128"},
		{sub2apiProxy{Protocol: "http"}, ""},
	}
	for _, tc := range cases {
		if got := buildProxyURL(tc.px); got != tc.want {
			t.Errorf("buildProxyURL(%+v) = %q, want %q", tc.px, got, tc.want)
		}
	}
}
