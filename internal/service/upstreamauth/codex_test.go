package upstreamauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// makeJWT builds an unsigned JWT with the given payload claims — the
// plugin never verifies signatures, so "x" works as the signature part.
func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
}

func codexClaims(exp int64) map[string]any {
	return map[string]any{
		"email": "dev@example.com",
		"exp":   exp,
		codexAuthClaim: map[string]any{
			"chatgpt_account_id": "acct-123",
			"chatgpt_plan_type":  "pro",
		},
	}
}

func TestCodexImportNativeAuthJSON(t *testing.T) {
	exp := time.Now().Add(45 * time.Minute).Unix()
	idToken := makeJWT(t, codexClaims(exp))
	access := makeJWT(t, map[string]any{"exp": exp})
	payload := fmt.Sprintf(`{
		"OPENAI_API_KEY": null,
		"tokens": {
			"id_token": %q,
			"access_token": %q,
			"refresh_token": "rt-1",
			"account_id": "acct-123"
		},
		"last_refresh": "2026-06-11T00:00:00Z"
	}`, idToken, access)

	bundle, err := NewCodexOAuth().ImportCredentials(context.Background(), &ImportCredentialsRequest{Payload: []byte(payload)})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	c := bundle.Credentials
	if c[credAccessToken] != access || c[credRefreshToken] != "rt-1" {
		t.Fatalf("tokens not carried over: %+v", c)
	}
	if c[credAccountID] != "acct-123" || c[credEmail] != "dev@example.com" || c[credPlan] != "pro" {
		t.Fatalf("identity not extracted: %+v", c)
	}
	if c[credSource] != "codex_auth_json" {
		t.Fatalf("source missing: %+v", c)
	}
	if bundle.ExpiresAt == nil || bundle.ExpiresAt.Unix() != exp {
		t.Fatalf("expiry should come from JWT exp claim, got %v want %d", bundle.ExpiresAt, exp)
	}
	if bundle.Profile == nil || bundle.Profile.Subject != "acct-123" {
		t.Fatalf("profile: %+v", bundle.Profile)
	}
}

func TestCodexImportFlatAndErrors(t *testing.T) {
	// Flat / pre-normalised layout.
	bundle, err := NewCodexOAuth().ImportCredentials(context.Background(), &ImportCredentialsRequest{
		Payload: []byte(`{"access_token":"at","refresh_token":"rt","account_id":"a1","expires_at":"1780000000"}`),
	})
	if err != nil {
		t.Fatalf("flat import: %v", err)
	}
	if bundle.Credentials[credExpiresAt] != "1780000000" {
		t.Fatalf("explicit expires_at not honoured: %+v", bundle.Credentials)
	}

	// Missing refresh token must be rejected — unrefreshable imports rot.
	if _, err := NewCodexOAuth().ImportCredentials(context.Background(), &ImportCredentialsRequest{
		Payload: []byte(`{"access_token":"at"}`),
	}); err == nil {
		t.Fatal("import without refresh_token should fail")
	}

	// Garbage payload.
	if _, err := NewCodexOAuth().ImportCredentials(context.Background(), &ImportCredentialsRequest{
		Payload: []byte(`not json`),
	}); err == nil {
		t.Fatal("non-JSON payload should fail")
	}
}

func TestCodexRefreshSuccess(t *testing.T) {
	exp := time.Now().Add(time.Hour).Unix()
	newID := makeJWT(t, codexClaims(exp))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "rt-old" {
			t.Fatalf("unexpected form: %v", r.Form)
		}
		if r.Form.Get("client_id") != codexClientID {
			t.Fatalf("client_id: %v", r.Form.Get("client_id"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-new",
			"refresh_token": "rt-new",
			"id_token":      newID,
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	p := &CodexOAuth{TokenURL: srv.URL}
	bundle, err := p.Refresh(context.Background(), &RefreshRequest{
		Credentials: map[string]string{credRefreshToken: "rt-old", credAccountID: "acct-123"},
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if bundle.Credentials[credAccessToken] != "at-new" || bundle.Credentials[credRefreshToken] != "rt-new" {
		t.Fatalf("rotated tokens not stored: %+v", bundle.Credentials)
	}
	if bundle.ExpiresAt == nil || time.Until(*bundle.ExpiresAt) < 55*time.Minute {
		t.Fatalf("expiry should be ~1h out, got %v", bundle.ExpiresAt)
	}
	if bundle.Profile.Email != "dev@example.com" || bundle.Profile.Plan != "pro" {
		t.Fatalf("profile not re-derived from new id_token: %+v", bundle.Profile)
	}
}

func TestCodexRefreshKeepsOldRefreshTokenWhenNotRotated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-new", "expires_in": 600})
	}))
	defer srv.Close()

	p := &CodexOAuth{TokenURL: srv.URL}
	bundle, err := p.Refresh(context.Background(), &RefreshRequest{
		Credentials: map[string]string{credRefreshToken: "rt-keep", credEmail: "kept@example.com"},
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if bundle.Credentials[credRefreshToken] != "rt-keep" {
		t.Fatalf("old refresh token should be kept: %+v", bundle.Credentials)
	}
	if bundle.Credentials[credEmail] != "kept@example.com" {
		t.Fatalf("identity fields should carry over: %+v", bundle.Credentials)
	}
}

func TestCodexRefreshInvalidGrantIsLoginRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "token revoked"})
	}))
	defer srv.Close()

	p := &CodexOAuth{TokenURL: srv.URL}
	_, err := p.Refresh(context.Background(), &RefreshRequest{
		Credentials: map[string]string{credRefreshToken: "rt-dead"},
	})
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("invalid_grant should map to ErrLoginRequired, got %v", err)
	}
}

func TestCodexRefreshServerErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	p := &CodexOAuth{TokenURL: srv.URL}
	_, err := p.Refresh(context.Background(), &RefreshRequest{
		Credentials: map[string]string{credRefreshToken: "rt"},
	})
	if err == nil || errors.Is(err, ErrLoginRequired) {
		t.Fatalf("5xx should be a plain (retryable) failure, got %v", err)
	}
}
