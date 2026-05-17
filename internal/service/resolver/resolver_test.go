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

func TestResolveReturnsErrWhenPoolEmpty(t *testing.T) {
	res := resolver.New(newTestPool(t), newTestRegistry(t), nil)
	_, _, err := res.ResolveForProviders(nil, nil)
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
	res := resolver.New(newTestPool(t, accounts...), newTestRegistry(t, "anthropic"), nil)

	seen := map[int64]int{}
	for i := 0; i < 200; i++ {
		acc, _, err := res.ResolveForProviders([]string{"anthropic"}, nil)
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
	res := resolver.New(
		newTestPool(t, accounts...),
		newTestRegistry(t, "anthropic"),
		nil,
	)

	hits := map[int64]int{}
	for i := 0; i < 1000; i++ {
		acc, _, _ := res.ResolveForProviders([]string{"anthropic"}, nil)
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
	res := resolver.New(
		newTestPool(t, accounts...),
		newTestRegistry(t, "anthropic", "claude-platform", "openai"),
		nil,
	)

	allowed := []string{"anthropic", "claude-platform"}
	seen := map[string]int{}
	for i := 0; i < 100; i++ {
		acc, _, err := res.ResolveForProviders(allowed, nil)
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
	res := resolver.New(
		newTestPool(t, accounts...),
		newTestRegistry(t, "anthropic"),
		nil,
	)

	// Exclude 1 and 2: only 3 should ever be returned.
	for i := 0; i < 50; i++ {
		acc, _, err := res.ResolveForProviders(nil, []int64{1, 2})
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
		FailureThreshold: 1, // trip after a single failure
		OpenDuration:     time.Hour,
	})

	res := resolver.New(
		newTestPool(t, accounts...),
		newTestRegistry(t, "anthropic"),
		tracker,
	)

	// Knock account 1 out: tracker should now report it unavailable.
	tracker.RecordFailure(1, errors.New("boom"))

	for i := 0; i < 30; i++ {
		acc, _, err := res.ResolveForProviders(nil, nil)
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
	res := resolver.New(
		newTestPool(t, &provider.Account{ID: 1, Provider: "anthropic", Weight: 1}),
		newTestRegistry(t, "anthropic"),
		nil,
	)
	_, driver, err := res.ResolveForProviders(nil, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if driver == nil || driver.Name() != "anthropic" {
		t.Errorf("driver: want anthropic, got %v", driver)
	}
}

func TestResolveFailsOnUnregisteredDriver(t *testing.T) {
	res := resolver.New(
		newTestPool(t, &provider.Account{ID: 1, Provider: "rogue", Weight: 1}),
		newTestRegistry(t), // empty registry
		nil,
	)
	_, _, err := res.ResolveForProviders(nil, nil)
	if err == nil {
		t.Errorf("expected error when account references unknown driver")
	}
}
