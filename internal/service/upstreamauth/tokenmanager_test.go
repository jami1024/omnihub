package upstreamauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// fakeStore is an in-memory AccountStore.
type fakeStore struct {
	mu       sync.Mutex
	account  *provider.Account
	updates  []repository.AuthRuntimeUpdate
	getCalls int
}

func (s *fakeStore) GetByID(_ context.Context, id int64) (*provider.Account, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if s.account == nil || s.account.ID != id {
		return nil, false, repository.ErrAccountNotFound
	}
	cp := *s.account
	return &cp, true, nil
}

func (s *fakeStore) UpdateAuthRuntime(_ context.Context, id int64, u repository.AuthRuntimeUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, u)
	a := s.account
	if u.Credentials != nil {
		a.Credentials = u.Credentials
	}
	a.AuthStatus = u.Status
	a.RefreshError = u.RefreshError
	if u.ExpiresAt != nil {
		a.AuthExpiresAt = u.ExpiresAt
	}
	if u.LastRefreshAt != nil {
		a.LastRefreshAt = u.LastRefreshAt
	}
	return nil
}

// fakePlugin counts refreshes and returns a canned bundle or error.
type fakePlugin struct {
	Unimplemented
	mu       sync.Mutex
	calls    int
	err      error
	expireIn time.Duration
}

func (p *fakePlugin) Metadata(context.Context) (*Metadata, error) {
	return &Metadata{Name: "fake"}, nil
}

func (p *fakePlugin) Refresh(context.Context, *RefreshRequest) (*TokenBundle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	exp := time.Now().Add(p.expireIn).UTC()
	return &TokenBundle{
		Credentials: map[string]string{credAccessToken: fmt.Sprintf("at-%d", p.calls), credRefreshToken: "rt"},
		ExpiresAt:   &exp,
		Profile:     &AccountProfile{Subject: "sub", Email: "e@x", Plan: "pro"},
	}, nil
}

func (p *fakePlugin) refreshCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func oauthAccount(expiresIn time.Duration) *provider.Account {
	exp := time.Now().Add(expiresIn).UTC()
	return &provider.Account{
		ID:            1,
		Name:          "codex-1",
		Provider:      "openai-codex",
		AuthType:      "imported_oauth",
		AuthPlugin:    "fake",
		AuthStatus:    StatusOK,
		AuthExpiresAt: &exp,
		Credentials:   map[string]string{credAccessToken: "at-old", credRefreshToken: "rt"},
	}
}

func newTestManager(a *provider.Account, p Provider) (*TokenManager, *fakeStore) {
	store := &fakeStore{account: a}
	reg := NewRegistry()
	reg.Register("fake", p)
	return NewTokenManager(store, reg, 5*time.Minute), store
}

func TestEnsureFreshPassThrough(t *testing.T) {
	plugin := &fakePlugin{expireIn: time.Hour}

	// api_key accounts are never touched.
	apiKey := &provider.Account{ID: 2, AuthType: "api_key"}
	tm, _ := newTestManager(apiKey, plugin)
	got, err := tm.EnsureFresh(context.Background(), apiKey)
	if err != nil || got != apiKey {
		t.Fatalf("api_key account must pass through, got %v err %v", got, err)
	}

	// OAuth account with plenty of life left: no refresh, no DB read.
	a := oauthAccount(time.Hour)
	tm, store := newTestManager(a, plugin)
	if _, err := tm.EnsureFresh(context.Background(), a); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if plugin.refreshCalls() != 0 || store.getCalls != 0 {
		t.Fatalf("fresh token should be untouched: refreshes=%d reads=%d", plugin.refreshCalls(), store.getCalls)
	}
}

func TestEnsureFreshRefreshesInsideWindow(t *testing.T) {
	plugin := &fakePlugin{expireIn: time.Hour}
	a := oauthAccount(time.Minute) // inside 5m window
	tm, store := newTestManager(a, plugin)

	got, err := tm.EnsureFresh(context.Background(), a)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if plugin.refreshCalls() != 1 {
		t.Fatalf("want exactly one refresh, got %d", plugin.refreshCalls())
	}
	if got.Credentials[credAccessToken] != "at-1" || got.AuthStatus != StatusOK {
		t.Fatalf("returned account not updated: %+v", got)
	}
	if len(store.updates) != 1 || store.updates[0].Credentials == nil || store.updates[0].Status != StatusOK {
		t.Fatalf("persisted update wrong: %+v", store.updates)
	}
	if store.updates[0].Subject != "sub" || store.updates[0].Plan != "pro" {
		t.Fatalf("identity not persisted: %+v", store.updates[0])
	}
}

func TestEnsureFreshSingleFlight(t *testing.T) {
	plugin := &fakePlugin{expireIn: time.Hour}
	a := oauthAccount(time.Minute)
	tm, _ := newTestManager(a, plugin)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cp := *a
			if _, err := tm.EnsureFresh(context.Background(), &cp); err != nil {
				t.Errorf("ensure: %v", err)
			}
		}()
	}
	wg.Wait()
	// The lock + reload double-check means only the first goroutine
	// refreshes; the rest see the already-renewed row.
	if plugin.refreshCalls() != 1 {
		t.Fatalf("refresh stampede: %d calls", plugin.refreshCalls())
	}
}

func TestEnsureFreshFailureParksAccount(t *testing.T) {
	plugin := &fakePlugin{err: errors.New("upstream down")}
	a := oauthAccount(time.Minute)
	tm, store := newTestManager(a, plugin)

	if _, err := tm.EnsureFresh(context.Background(), a); err == nil {
		t.Fatal("refresh failure must surface an error")
	}
	last := store.updates[len(store.updates)-1]
	if last.Status != StatusRefreshFailed || last.RefreshError == "" {
		t.Fatalf("failure not recorded: %+v", last)
	}
	if store.account.AuthRoutable() {
		t.Fatal("refresh_failed account must not be routable")
	}
}

func TestEnsureFreshLoginRequired(t *testing.T) {
	plugin := &fakePlugin{err: fmt.Errorf("dead token: %w", ErrLoginRequired)}
	a := oauthAccount(time.Minute)
	tm, store := newTestManager(a, plugin)

	if _, err := tm.EnsureFresh(context.Background(), a); err == nil {
		t.Fatal("want error")
	}
	if store.account.AuthStatus != StatusLoginRequired {
		t.Fatalf("invalid grant should park as login_required, got %q", store.account.AuthStatus)
	}
}

func TestForceRefreshDebounce(t *testing.T) {
	plugin := &fakePlugin{expireIn: time.Hour}
	a := oauthAccount(time.Hour)
	now := time.Now().UTC()
	a.LastRefreshAt = &now // just refreshed by another request
	tm, _ := newTestManager(a, plugin)

	got, err := tm.ForceRefresh(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("force: %v", err)
	}
	if plugin.refreshCalls() != 0 {
		t.Fatalf("debounce should skip the refresh, got %d calls", plugin.refreshCalls())
	}
	if got == nil || got.ID != a.ID {
		t.Fatalf("should return reloaded account, got %+v", got)
	}

	// Stale last refresh → force goes through even with a live token.
	old := time.Now().Add(-time.Hour).UTC()
	a.LastRefreshAt = &old
	if _, err := tm.ForceRefresh(context.Background(), a.ID); err != nil {
		t.Fatalf("force: %v", err)
	}
	if plugin.refreshCalls() != 1 {
		t.Fatalf("stale force refresh should run, got %d calls", plugin.refreshCalls())
	}
}

func TestSweepSkipsParkedAccounts(t *testing.T) {
	plugin := &fakePlugin{expireIn: time.Hour}
	a := oauthAccount(time.Minute)
	a.AuthStatus = StatusLoginRequired
	tm, _ := newTestManager(a, plugin)

	tm.sweep(context.Background(), []*provider.Account{a})
	if plugin.refreshCalls() != 0 {
		t.Fatalf("login_required accounts must not be retried, got %d", plugin.refreshCalls())
	}

	// refresh_failed with a recent attempt: backed off.
	a.AuthStatus = StatusRefreshFailed
	recent := time.Now().UTC()
	a.LastRefreshAt = &recent
	tm.sweep(context.Background(), []*provider.Account{a})
	if plugin.refreshCalls() != 0 {
		t.Fatalf("recent refresh_failed should back off, got %d", plugin.refreshCalls())
	}

	// refresh_failed with an old attempt: retried.
	stale := time.Now().Add(-time.Hour).UTC()
	a.LastRefreshAt = &stale
	tm.sweep(context.Background(), []*provider.Account{a})
	if plugin.refreshCalls() != 1 {
		t.Fatalf("stale refresh_failed should retry, got %d", plugin.refreshCalls())
	}
}
