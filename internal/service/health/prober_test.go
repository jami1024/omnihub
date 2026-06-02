package health

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// fakeDriver implements provider.Driver and (optionally) provider.Tester.
type fakeDriver struct {
	name    string
	tester  bool
	mu      sync.Mutex
	verdict provider.TestStatus
}

func (d *fakeDriver) Name() string                       { return d.name }
func (d *fakeDriver) Capabilities() provider.Capabilities { return provider.Capabilities{Chat: true} }
func (d *fakeDriver) BuildRequest(context.Context, *ir.UnifiedRequest, *provider.Account) (*http.Request, error) {
	return nil, nil
}
func (d *fakeDriver) ParseResponse(*http.Response) (*ir.UnifiedResponse, error) { return nil, nil }
func (d *fakeDriver) DecodeStream(io.ReadCloser) provider.StreamIter            { return nil }

func (d *fakeDriver) setVerdict(s provider.TestStatus) {
	d.mu.Lock()
	d.verdict = s
	d.mu.Unlock()
}
func (d *fakeDriver) Test(context.Context, *provider.Account) provider.TestResult {
	d.mu.Lock()
	v := d.verdict
	d.mu.Unlock()
	return provider.TestResult{Status: v, Message: string(v)}
}

// testerDriver is the Tester-capable variant; a Driver without Test is
// just *fakeDriver with tester=false wrapped so the type-assert fails.
type noTesterDriver struct{ name string }

func (d *noTesterDriver) Name() string                       { return d.name }
func (d *noTesterDriver) Capabilities() provider.Capabilities { return provider.Capabilities{} }
func (d *noTesterDriver) BuildRequest(context.Context, *ir.UnifiedRequest, *provider.Account) (*http.Request, error) {
	return nil, nil
}
func (d *noTesterDriver) ParseResponse(*http.Response) (*ir.UnifiedResponse, error) { return nil, nil }
func (d *noTesterDriver) DecodeStream(io.ReadCloser) provider.StreamIter            { return nil }

type fakeRegistry map[string]provider.Driver

func (r fakeRegistry) Get(name string) (provider.Driver, bool) {
	d, ok := r[name]
	return d, ok
}

func boolp(b bool) *bool { return &b }

func newTestTracker() *Tracker {
	return New(Config{FailureThreshold: 5, OpenDuration: time.Hour, HalfOpenSuccessThreshold: 1})
}

// Sustained red probes must eventually open the breaker, but only after
// the de-bounce threshold plus the breaker's own FailureThreshold.
func TestProberSustainedRedOpensBreaker(t *testing.T) {
	drv := &fakeDriver{name: "fake", verdict: provider.TestRed}
	tr := newTestTracker()
	p := NewProber(tr, fakeRegistry{"fake": drv}, true, 4)
	acct := &provider.Account{ID: 1, Name: "a", Provider: "fake"}
	snap := []*provider.Account{acct}

	// reds 1,2 → de-bounced (no RecordFailure). 3..7 → 5 RecordFailure
	// calls → opens at the 5th. So after 7 passes IsAvailable is false.
	for i := 0; i < 7; i++ {
		p.probeAll(context.Background(), snap)
	}
	if tr.IsAvailable(1) {
		t.Fatalf("expected breaker OPEN after sustained red probes")
	}
}

func TestProberFewRedsDoNotOpen(t *testing.T) {
	drv := &fakeDriver{name: "fake", verdict: provider.TestRed}
	tr := newTestTracker()
	p := NewProber(tr, fakeRegistry{"fake": drv}, true, 4)
	snap := []*provider.Account{{ID: 1, Name: "a", Provider: "fake"}}
	// 2 reds are below probeRedThreshold (3) → no tracker call at all.
	p.probeAll(context.Background(), snap)
	p.probeAll(context.Background(), snap)
	if !tr.IsAvailable(1) {
		t.Fatalf("a couple of transient reds must NOT eject the account")
	}
}

func TestProberGreenRecovers(t *testing.T) {
	drv := &fakeDriver{name: "fake", verdict: provider.TestRed}
	tr := newTestTracker()
	p := NewProber(tr, fakeRegistry{"fake": drv}, true, 4)
	snap := []*provider.Account{{ID: 1, Name: "a", Provider: "fake"}}
	for i := 0; i < 7; i++ {
		p.probeAll(context.Background(), snap)
	}
	if tr.IsAvailable(1) {
		t.Fatalf("precondition: breaker should be open")
	}
	// A green probe records success → closes the breaker.
	drv.setVerdict(provider.TestGreen)
	p.probeAll(context.Background(), snap)
	if !tr.IsAvailable(1) {
		t.Fatalf("a green probe should restore availability")
	}
}

func TestProberYellowNeverEjects(t *testing.T) {
	drv := &fakeDriver{name: "fake", verdict: provider.TestYellow}
	tr := newTestTracker()
	p := NewProber(tr, fakeRegistry{"fake": drv}, true, 4)
	snap := []*provider.Account{{ID: 1, Name: "a", Provider: "fake"}}
	for i := 0; i < 20; i++ {
		p.probeAll(context.Background(), snap)
	}
	if !tr.IsAvailable(1) {
		t.Fatalf("yellow (slow/429) must never eject a reachable account")
	}
}

func TestProberSkipsOptOutAndNoTester(t *testing.T) {
	drv := &fakeDriver{name: "fake", verdict: provider.TestRed}
	tr := newTestTracker()
	reg := fakeRegistry{"fake": drv, "noprobe": &noTesterDriver{name: "noprobe"}}
	p := NewProber(tr, reg, true, 4)

	optedOut := &provider.Account{ID: 1, Name: "opt-out", Provider: "fake", HealthProbeEnabled: boolp(false)}
	noTester := &provider.Account{ID: 2, Name: "no-tester", Provider: "noprobe"}
	snap := []*provider.Account{optedOut, noTester}
	for i := 0; i < 10; i++ {
		p.probeAll(context.Background(), snap)
	}
	if !tr.IsAvailable(1) {
		t.Errorf("opted-out account must not be probed/ejected")
	}
	if !tr.IsAvailable(2) {
		t.Errorf("account whose driver has no Tester must not be ejected")
	}
	p.mu.Lock()
	n := len(p.state)
	p.mu.Unlock()
	if n != 0 {
		t.Errorf("no probe state should be recorded for skipped accounts, got %d", n)
	}
}

// Global default false means a nil opt-in account is not probed.
func TestProberGlobalDefaultOff(t *testing.T) {
	drv := &fakeDriver{name: "fake", verdict: provider.TestRed}
	tr := newTestTracker()
	p := NewProber(tr, fakeRegistry{"fake": drv}, false, 4) // global OFF
	snap := []*provider.Account{{ID: 1, Name: "a", Provider: "fake"}} // nil opt-in
	for i := 0; i < 10; i++ {
		p.probeAll(context.Background(), snap)
	}
	if !tr.IsAvailable(1) {
		t.Fatalf("with global default off and no per-account opt-in, account must not be probed")
	}
}

// State for an account that leaves the pool must be garbage-collected.
func TestProberGCsRemovedAccounts(t *testing.T) {
	drv := &fakeDriver{name: "fake", verdict: provider.TestRed}
	tr := newTestTracker()
	p := NewProber(tr, fakeRegistry{"fake": drv}, true, 4)
	p.probeAll(context.Background(), []*provider.Account{{ID: 1, Name: "a", Provider: "fake"}})
	p.mu.Lock()
	had := len(p.state)
	p.mu.Unlock()
	if had == 0 {
		t.Fatalf("precondition: state should exist for the probed account")
	}
	// Next pass without that account → its state is GC'd.
	p.probeAll(context.Background(), nil)
	p.mu.Lock()
	n := len(p.state)
	p.mu.Unlock()
	if n != 0 {
		t.Errorf("state for a removed account should be GC'd, got %d", n)
	}
}
