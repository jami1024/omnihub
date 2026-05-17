package resolver_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/resolver"
	"github.com/jami1024/omnihub/internal/service/session"
)

type stubSource struct {
	accounts []*provider.Account
}

func (s *stubSource) ListEnabled(_ context.Context) ([]*provider.Account, error) {
	return s.accounts, nil
}

type stubDriver struct{ name string }

func (s *stubDriver) Name() string                       { return s.name }
func (s *stubDriver) Capabilities() provider.Capabilities { return provider.Capabilities{Chat: true} }
func (s *stubDriver) BuildRequest(context.Context, *ir.UnifiedRequest, *provider.Account) (*http.Request, error) {
	return nil, nil
}
func (s *stubDriver) ParseResponse(*http.Response) (*ir.UnifiedResponse, error) { return nil, nil }
func (s *stubDriver) DecodeStream(io.ReadCloser) provider.StreamIter            { return nil }

func newTestPool(t *testing.T, accounts ...*provider.Account) *account.Pool {
	t.Helper()
	p := account.NewPool(&stubSource{accounts: accounts})
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return p
}

func newTestRegistry(t *testing.T, names ...string) *provider.Registry {
	t.Helper()
	r := provider.NewRegistry()
	for _, n := range names {
		r.MustRegister(&stubDriver{name: n})
	}
	return r
}

func newRes(
	t *testing.T,
	accounts []*provider.Account,
	drivers []string,
	tracker *health.Tracker,
	sessions *session.Store,
) *resolver.WeightedResolver {
	t.Helper()
	return resolver.New(
		newTestPool(t, accounts...),
		newTestRegistry(t, drivers...),
		tracker,
		sessions,
	)
}

func TestResolveReturnsErrWhenPoolEmpty(t *testing.T) {
	res := newRes(t, nil, nil, nil, nil)
	_, _, err := res.ResolveForProviders("", nil, nil)
	if !errors.Is(err, resolver.ErrNoUpstream) {
		t.Errorf("want ErrNoUpstream, got %v", err)
	}
}

func TestResolvePicksTopPriorityBucket(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Name: "pri0-a", Provider: "anthropic", Priority: 0, Weight: 100},
		{ID: 2, Name: "pri0-b", Provider: "anthropic", Priority: 0, Weight: 100},
		{ID: 3, Name: "pri1-a", Provider: "anthropic", Priority: 1, Weight: 100},
	}
	res := newRes(t, accounts, []string{"anthropic"}, nil, nil)

	seen := map[int64]int{}
	for i := 0; i < 200; i++ {
		acc, _, err := res.ResolveForProviders("", []string{"anthropic"}, nil)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		seen[acc.ID]++
	}
	if seen[3] != 0 {
		t.Errorf("priority=1 should never be picked when priority=0 exists, got %d hits", seen[3])
	}
	if seen[1] == 0 || seen[2] == 0 {
		t.Errorf("both priority=0 accounts should be picked, got %v", seen)
	}
}

func TestResolveWeightedDistribution(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Name: "A", Provider: "anthropic", Priority: 0, Weight: 90},
		{ID: 2, Name: "B", Provider: "anthropic", Priority: 0, Weight: 10},
	}
	res := newRes(t, accounts, []string{"anthropic"}, nil, nil)

	hits := map[int64]int{}
	for i := 0; i < 1000; i++ {
		acc, _, _ := res.ResolveForProviders("", []string{"anthropic"}, nil)
		hits[acc.ID]++
	}
	if hits[1] < hits[2]*6 {
		t.Errorf("90/10 weighting should heavily favour A: got A=%d, B=%d", hits[1], hits[2])
	}
}

func TestResolveFiltersToAllowedProviders(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Name: "ant", Provider: "anthropic", Weight: 100},
		{ID: 2, Name: "cp", Provider: "claude-platform", Weight: 100},
		{ID: 3, Name: "oai", Provider: "openai", Weight: 100},
	}
	res := newRes(t, accounts, []string{"anthropic", "claude-platform", "openai"}, nil, nil)

	allowed := []string{"anthropic", "claude-platform"}
	seen := map[string]int{}
	for i := 0; i < 100; i++ {
		acc, _, err := res.ResolveForProviders("", allowed, nil)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		seen[acc.Provider]++
	}
	if seen["openai"] != 0 {
		t.Errorf("filter should exclude openai, got %d hits", seen["openai"])
	}
}

func TestResolveExcludesAlreadyTriedIDs(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Provider: "anthropic", Weight: 100},
		{ID: 2, Provider: "anthropic", Weight: 100},
		{ID: 3, Provider: "anthropic", Weight: 100},
	}
	res := newRes(t, accounts, []string{"anthropic"}, nil, nil)

	for i := 0; i < 50; i++ {
		acc, _, err := res.ResolveForProviders("", nil, []int64{1, 2})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if acc.ID != 3 {
			t.Errorf("expected account 3, got %d", acc.ID)
		}
	}
}

func TestResolveSkipsUnhealthyAccounts(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Provider: "anthropic", Weight: 100},
		{ID: 2, Provider: "anthropic", Weight: 100},
	}
	tracker := health.New(health.Config{
		FailureThreshold: 1,
		OpenDuration:     time.Hour,
	})
	res := newRes(t, accounts, []string{"anthropic"}, tracker, nil)

	tracker.RecordFailure(1, errors.New("boom"))

	for i := 0; i < 30; i++ {
		acc, _, err := res.ResolveForProviders("", nil, nil)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if acc.ID == 1 {
			t.Errorf("account 1 should be filtered out, got it on iteration %d", i)
			break
		}
	}
}

func TestResolveReturnsDriver(t *testing.T) {
	res := newRes(t, []*provider.Account{{ID: 1, Provider: "anthropic", Weight: 1}},
		[]string{"anthropic"}, nil, nil)
	_, driver, err := res.ResolveForProviders("", nil, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if driver == nil || driver.Name() != "anthropic" {
		t.Errorf("driver: want anthropic, got %v", driver)
	}
}

func TestResolveFailsOnUnregisteredDriver(t *testing.T) {
	res := newRes(t, []*provider.Account{{ID: 1, Provider: "rogue", Weight: 1}},
		nil, nil, nil)
	_, _, err := res.ResolveForProviders("", nil, nil)
	if err == nil {
		t.Errorf("expected error when account references unknown driver")
	}
}

func TestStickySendsSameAccountForSameSession(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Name: "A", Provider: "anthropic", Weight: 1},
		{ID: 2, Name: "B", Provider: "anthropic", Weight: 1},
		{ID: 3, Name: "C", Provider: "anthropic", Weight: 1},
	}
	store := session.New(time.Minute)
	res := newRes(t, accounts, []string{"anthropic"}, nil, store)

	// First call binds the session to whatever account gets picked.
	first, _, err := res.ResolveForProviders("sess-1", nil, nil)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// 100 follow-up calls should always come back to the same account.
	for i := 0; i < 100; i++ {
		acc, _, err := res.ResolveForProviders("sess-1", nil, nil)
		if err != nil {
			t.Fatalf("follow-up %d: %v", i, err)
		}
		if acc.ID != first.ID {
			t.Fatalf("sticky binding broken: first=%d follow-up[%d]=%d",
				first.ID, i, acc.ID)
		}
	}
}

func TestStickyDistinctSessionsCanLandOnDifferentAccounts(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Name: "A", Provider: "anthropic", Weight: 1},
		{ID: 2, Name: "B", Provider: "anthropic", Weight: 1},
		{ID: 3, Name: "C", Provider: "anthropic", Weight: 1},
	}
	store := session.New(time.Minute)
	res := newRes(t, accounts, []string{"anthropic"}, nil, store)

	seen := map[int64]struct{}{}
	for i := 0; i < 30; i++ {
		acc, _, err := res.ResolveForProviders(
			"sess-"+string(rune('a'+i%26)),
			nil, nil,
		)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		seen[acc.ID] = struct{}{}
	}
	if len(seen) < 2 {
		t.Errorf("distinct sessions should hit at least 2 accounts, only saw %v", seen)
	}
}

func TestStickyFallsBackWhenBoundAccountUnhealthy(t *testing.T) {
	accounts := []*provider.Account{
		{ID: 1, Name: "A", Provider: "anthropic", Weight: 1},
		{ID: 2, Name: "B", Provider: "anthropic", Weight: 1},
	}
	tracker := health.New(health.Config{FailureThreshold: 1, OpenDuration: time.Hour})
	store := session.New(time.Minute)
	res := newRes(t, accounts, []string{"anthropic"}, tracker, store)

	// Force-bind sess-1 → account 1, then poison account 1.
	store.Bind("sess-1", 1)
	tracker.RecordFailure(1, errors.New("boom"))

	for i := 0; i < 20; i++ {
		acc, _, err := res.ResolveForProviders("sess-1", nil, nil)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if acc.ID == 1 {
			t.Fatalf("sticky should not return an unhealthy account")
		}
	}
}

func TestStickyNotBoundWhenExcludedIsNonEmpty(t *testing.T) {
	// During a retry loop excludedAccountIDs is non-empty; the resolver
	// must NOT bind the session to the fallback so future requests
	// re-evaluate from scratch.
	accounts := []*provider.Account{
		{ID: 1, Provider: "anthropic", Weight: 1},
		{ID: 2, Provider: "anthropic", Weight: 1},
	}
	store := session.New(time.Minute)
	res := newRes(t, accounts, []string{"anthropic"}, nil, store)

	_, _, err := res.ResolveForProviders("sess-1", nil, []int64{1})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := store.Get("sess-1"); ok {
		t.Error("retry-fallback path should not establish a sticky binding")
	}
}
