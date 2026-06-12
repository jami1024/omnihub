package upstreamauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClaudeImportNativeCredentials(t *testing.T) {
	expMs := time.Now().Add(30*time.Minute).UnixMilli() / 1000 * 1000 // whole seconds
	payload := fmt.Sprintf(`{
		"claudeAiOauth": {
			"accessToken": "at-1",
			"refreshToken": "rt-1",
			"expiresAt": %d,
			"scopes": ["user:profile", "user:inference"],
			"subscriptionType": "max"
		}
	}`, expMs)

	bundle, err := NewClaudeOAuth().ImportCredentials(context.Background(), &ImportCredentialsRequest{Payload: []byte(payload)})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	c := bundle.Credentials
	if c[credAccessToken] != "at-1" || c[credRefreshToken] != "rt-1" {
		t.Fatalf("tokens: %+v", c)
	}
	if c[credPlan] != "max" || c["scopes"] != "user:profile user:inference" {
		t.Fatalf("plan/scopes: %+v", c)
	}
	if bundle.ExpiresAt == nil || bundle.ExpiresAt.UnixMilli() != expMs {
		t.Fatalf("expiresAt must convert ms→time, got %v want %d", bundle.ExpiresAt, expMs)
	}

	// Missing refresh token is rejected.
	if _, err := NewClaudeOAuth().ImportCredentials(context.Background(), &ImportCredentialsRequest{
		Payload: []byte(`{"claudeAiOauth":{"accessToken":"at"}}`),
	}); err == nil {
		t.Fatal("import without refreshToken should fail")
	}
}

func TestClaudeRefreshSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content type: %s", ct)
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["grant_type"] != "refresh_token" || in["refresh_token"] != "rt-old" || in["client_id"] != claudeClientID {
			t.Fatalf("body: %v", in)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-new",
			"refresh_token": "rt-new",
			"expires_in":    28800,
			"account_type":  "claude_max",
		})
	}))
	defer srv.Close()

	p := &ClaudeOAuth{TokenURL: srv.URL}
	bundle, err := p.Refresh(context.Background(), &RefreshRequest{
		Credentials: map[string]string{
			credRefreshToken: "rt-old",
			credEmail:        "kept@example.com",
			credAccountID:    "uuid-1",
		},
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	c := bundle.Credentials
	if c[credAccessToken] != "at-new" || c[credRefreshToken] != "rt-new" {
		t.Fatalf("tokens: %+v", c)
	}
	if c[credPlan] != "claude_max" || c[credEmail] != "kept@example.com" || c[credAccountID] != "uuid-1" {
		t.Fatalf("identity: %+v", c)
	}
	if bundle.ExpiresAt == nil || time.Until(*bundle.ExpiresAt) < 7*time.Hour {
		t.Fatalf("expiry should be ~8h out, got %v", bundle.ExpiresAt)
	}
}

func TestClaudeRefreshInvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "expired"})
	}))
	defer srv.Close()

	p := &ClaudeOAuth{TokenURL: srv.URL}
	_, err := p.Refresh(context.Background(), &RefreshRequest{
		Credentials: map[string]string{credRefreshToken: "rt-dead"},
	})
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("invalid_grant should map to ErrLoginRequired, got %v", err)
	}
}

func TestClaudeValidateProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-1" {
			t.Fatalf("auth: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("anthropic-beta") != claudeOAuthBeta {
			t.Fatalf("beta: %s", r.Header.Get("anthropic-beta"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account": map[string]any{
				"uuid":           "uuid-9",
				"email":          "max@example.com",
				"has_claude_max": true,
			},
		})
	}))
	defer srv.Close()

	p := &ClaudeOAuth{ProfileURL: srv.URL}
	profile, err := p.Validate(context.Background(), &ValidateRequest{
		Credentials: map[string]string{credAccessToken: "at-1"},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if profile.Subject != "uuid-9" || profile.Email != "max@example.com" || profile.Plan != "claude_max" {
		t.Fatalf("profile: %+v", profile)
	}
}
