package resolver_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/resolver"
)

// stubSource is a Source implementation backed by an in-memory slice.
type stubSource struct {
	accounts []*provider.Account
	err      error
}

func (s *stubSource) ListEnabled(_ context.Context) ([]*provider.Account, error) {
	if s.err != nil {
		return nil, s.err
	}
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
	res := resolver.New(newTestPool(t), newTestRegistry(t))
	_, _, err := res.ResolveForProviders(nil)
	if !errors.Is(err, resolver.ErrNoUpstream) {
		t.Errorf("want ErrNoUpstream, got %v", err)
	}
}

func TestResolvePicksTopPriorityBucket(t *testing.T) {
	// priority=0 is the top bucket; priority=1 should never be chosen.
	accounts := []*provider.Account{
		{ID: 1, Name: "pri0-a", Provider: "anthropic", Priority: 0, Weight: 100},
		{ID: 2, Name: "pri0-b", Provider: "anthropic", Priority: 0, Weight: 100},
		{ID: 3, Name: "pri1-a", Provider: "anthropic", Priority: 1, Weight: 100},
	}
	pool := newTestPool(t, accounts...)
	reg := newTestRegistry(t, "anthropic")
	res := resolver.New(pool, reg)

	seen := map[int64]int{}
	for i := 0; i < 200; i++ {
		acc, _, err := res.ResolveForProviders([]string{"anthropic"})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		seen[acc.ID]++
	}
	if seen[3] != 0 {
		t.Errorf("priority=1 account should never be picked when priority=0 exists, got %d hits", seen[3])
	}
	if seen[1] == 0 || seen[2] == 0 {
		t.Errorf("both priority=0 accounts should be picked, got %v", seen)
	}
}

func TestResolveWeightedDistribution(t *testing.T) {
	// account A has weight 90, B has weight 10. Over many trials A
	// should be picked ~9× more often than B.
	accounts := []*provider.Account{
		{ID: 1, Name: "A", Provider: "anthropic", Priority: 0, Weight: 90},
		{ID: 2, Name: "B", Provider: "anthropic", Priority: 0, Weight: 10},
	}
	res := resolver.New(
		newTestPool(t, accounts...),
		newTestRegistry(t, "anthropic"),
	)

	hits := map[int64]int{}
	for i := 0; i < 1000; i++ {
		acc, _, _ := res.ResolveForProviders([]string{"anthropic"})
		hits[acc.ID]++
	}
	// Loose bounds: A ≥ 6× B is plenty to detect a broken algorithm.
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
	)

	// Restrict to Anthropic-format providers only.
	allowedProviders := []string{"anthropic", "claude-platform"}
	seen := map[string]int{}
	for i := 0; i < 100; i++ {
		acc, _, err := res.ResolveForProviders(allowedProviders)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		seen[acc.Provider]++
	}
	if seen["openai"] != 0 {
		t.Errorf("filter should exclude openai, got %d hits", seen["openai"])
	}
	if seen["anthropic"] == 0 || seen["claude-platform"] == 0 {
		t.Errorf("both anthropic-format providers should be picked: %v", seen)
	}
}

func TestResolveReturnsDriver(t *testing.T) {
	res := resolver.New(
		newTestPool(t, &provider.Account{ID: 1, Provider: "anthropic", Weight: 1}),
		newTestRegistry(t, "anthropic"),
	)
	_, driver, err := res.ResolveForProviders(nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if driver == nil {
		t.Fatalf("driver should be non-nil")
	}
	if driver.Name() != "anthropic" {
		t.Errorf("driver name: want anthropic, got %q", driver.Name())
	}
}

func TestResolveFailsOnUnregisteredDriver(t *testing.T) {
	// Pool has an account whose provider has no registered driver.
	res := resolver.New(
		newTestPool(t, &provider.Account{ID: 1, Provider: "rogue", Weight: 1}),
		newTestRegistry(t), // empty registry
	)
	_, _, err := res.ResolveForProviders(nil)
	if err == nil {
		t.Errorf("expected error when account references unknown driver")
	}
}

func TestResolveZeroWeightDegradesToUniform(t *testing.T) {
	// All weights 0 → resolver must still pick (uniform fallback).
	accounts := []*provider.Account{
		{ID: 1, Provider: "anthropic", Weight: 0},
		{ID: 2, Provider: "anthropic", Weight: 0},
	}
	res := resolver.New(
		newTestPool(t, accounts...),
		newTestRegistry(t, "anthropic"),
	)
	for i := 0; i < 20; i++ {
		_, _, err := res.ResolveForProviders(nil)
		if err != nil {
			t.Fatalf("Resolve at iter %d: %v", i, err)
		}
	}
}
