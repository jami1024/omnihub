package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/handler/gateway"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/claudesub"
)

// doMessages drives a POST /v1/messages through a gin handler (the
// handler ignores the path, but this keeps the intent clear).
func doMessages(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h(c)
	return rec
}

// claudeSubAccount is an OAuth-backed Claude subscription account whose
// upstream is the given mock server.
func claudeSubAccount(baseURL, accessToken string) *provider.Account {
	return &provider.Account{
		ID:          1,
		Name:        "claude-max-1",
		Provider:    "claude-subscription",
		BaseURL:     baseURL,
		AuthType:    "imported_oauth",
		Credentials: map[string]string{"access_token": accessToken},
	}
}

// newAnthropicHandler wires AnthropicMessagesHandler around a stub
// resolver pinned to the given account + the real claude-subscription
// driver, with the supplied tracker and token freshener (both may be
// nil). buffer/limiter/billing are nil — this exercises the driver and
// forward path, not persistence.
func newAnthropicHandler(account *provider.Account, tracker *health.Tracker, tokens gateway.TokenFreshener) gin.HandlerFunc {
	res := &stubResolver{account: account, driver: claudesub.New()}
	return gateway.AnthropicMessagesHandler(
		forward.New(nil), res, tracker, nil, pricing.Default(), nil, nil, nil, tokens, nil,
	)
}

// TestClaudeSubscriptionHandler_E2E drives a /v1/messages request
// through the claude-subscription driver to a mock upstream and asserts
// the OAuth request shape (Bearer token, oauth beta, claude-cli UA, NO
// x-api-key, Claude Code system prompt prepended) plus verbatim
// response pass-through.
func TestClaudeSubscriptionHandler_E2E(t *testing.T) {
	const upstreamBody = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`
	var gotPath, gotAuth, gotAPIKey, gotBeta, gotUA string
	var gotBody map[string]json.RawMessage
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer upstream.Close()

	h := newAnthropicHandler(claudeSubAccount(upstream.URL, "at-live"), nil, nil)
	// Client sends a non-Claude-Code system prompt; the driver must
	// prepend the identity sentence without dropping it.
	rec := doMessages(h, `{"model":"claude-sonnet-4-6","max_tokens":64,"system":[{"type":"text","text":"You are a bot."}],"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/messages" {
		t.Errorf("upstream path = %q", gotPath)
	}
	if gotAuth != "Bearer at-live" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key must be absent on OAuth traffic, got %q", gotAPIKey)
	}
	if gotBeta == "" || gotBeta[:16] != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta must lead with the oauth beta, got %q", gotBeta)
	}
	if len(gotUA) < 10 || gotUA[:10] != "claude-cli" {
		t.Errorf("user-agent should be claude-cli, got %q", gotUA)
	}
	if rec.Body.String() != upstreamBody {
		t.Errorf("response not passed through verbatim")
	}

	var system []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(gotBody["system"], &system); err != nil {
		t.Fatalf("system: %v", err)
	}
	if len(system) != 2 || system[0].Text != "You are Claude Code, Anthropic's official CLI for Claude." || system[1].Text != "You are a bot." {
		t.Fatalf("Claude Code identity must be prepended, client content kept: %+v", system)
	}
}

// fakeFreshener is a TokenFreshener stub: EnsureFresh passes through;
// ForceRefresh swaps in a pre-canned refreshed account and counts calls.
type fakeFreshener struct {
	refreshed  *provider.Account
	forceCalls int32
	ensureErr  error
}

func (f *fakeFreshener) EnsureFresh(_ context.Context, a *provider.Account) (*provider.Account, error) {
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	return a, nil
}

func (f *fakeFreshener) ForceRefresh(_ context.Context, _ int64) (*provider.Account, error) {
	atomic.AddInt32(&f.forceCalls, 1)
	return f.refreshed, nil
}

// TestAnthropicHandler_401RefreshRetry verifies the 401 recovery path:
// the upstream rejects the first call with 401, the handler force-
// refreshes the token and retries the SAME account, and the retry
// (carrying the new Bearer token) is committed to the client.
func TestAnthropicHandler_401RefreshRetry(t *testing.T) {
	const okBody = `{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`
	var calls int32
	var secondAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error"}}`)
			return
		}
		secondAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okBody)
	}))
	defer upstream.Close()

	account := claudeSubAccount(upstream.URL, "at-stale")
	refreshed := claudeSubAccount(upstream.URL, "at-fresh")
	fresh := &fakeFreshener{refreshed: refreshed}

	h := newAnthropicHandler(account, nil, fresh)
	rec := doMessages(h, `{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(&fresh.forceCalls) != 1 {
		t.Fatalf("want exactly one ForceRefresh, got %d", fresh.forceCalls)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("want one 401 + one retry, got %d upstream calls", calls)
	}
	if secondAuth != "Bearer at-fresh" {
		t.Fatalf("retry must carry the refreshed token, got %q", secondAuth)
	}
	if rec.Body.String() != okBody {
		t.Fatalf("retry response not committed verbatim")
	}
}

// TestAnthropicHandler_429SetsCooldown verifies that a 429 with a
// Retry-After parks the account on the health tracker for the upstream-
// stated window — so a later resolve would skip it.
func TestAnthropicHandler_429SetsCooldown(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error"}}`)
	}))
	defer upstream.Close()

	tracker := health.New(health.DefaultConfig())
	account := claudeSubAccount(upstream.URL, "at-live")

	h := newAnthropicHandler(account, tracker, nil)
	rec := doMessages(h, `{"model":"claude-sonnet-4-6","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)

	// Every attempt 429s and the stub resolver only knows one account,
	// so the request ends in a failover-exhausted error (not 200).
	if rec.Code == http.StatusOK {
		t.Fatalf("expected a non-200 after exhausting retries, got 200")
	}
	if until := tracker.CooldownUntil(account.ID); until.IsZero() {
		t.Fatal("429 must park the account on a cooldown")
	}
}
