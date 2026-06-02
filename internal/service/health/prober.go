package health

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// Probe tuning. These ship as constants (documented as future env
// knobs) so a single transient blip never ejects an account while a
// sustained outage still trips the breaker.
const (
	// probeRedThreshold is how many CONSECUTIVE red probes an account
	// must accumulate before red verdicts start feeding the circuit
	// breaker. Below this, reds are de-bounced (no tracker call).
	probeRedThreshold = 3
	// probeGreenThreshold is how many consecutive green probes before
	// green verdicts feed RecordSuccess.
	probeGreenThreshold = 1
	// probeTimeout bounds a single upstream probe.
	probeTimeout = 15 * time.Second
)

// ProbeRegistry is the slice of provider.Registry the prober needs: look
// up an account's driver so it can type-assert the optional Tester.
type ProbeRegistry interface {
	Get(name string) (provider.Driver, bool)
}

// probeState is the prober's OWN per-account de-bounce counters, kept
// separate from the circuit breaker's failure count so probe noise never
// pollutes request-driven health.
type probeState struct {
	consecReds   int
	consecGreens int
}

// Prober actively checks each opted-in account's upstream reachability in
// the background (GET /v1/models via the driver's provider.Tester) and
// feeds the verdict into the EXISTING circuit-breaker Tracker. Because
// the resolver already drops accounts where Tracker.IsAvailable is false,
// a sick upstream is taken out of rotation before user traffic hits it,
// and a recovered one is restored — all through one observability
// surface (the /admin Health page + account_health_events).
//
// Verdicts are de-bounced per account (probeState) and edge-triggered
// into the tracker so a single transient red never flips an account and
// a sustained outage reliably opens the breaker. Yellow (slow / 429) and
// accounts whose driver has no Tester (e.g. claude-platform) produce NO
// tracker call and never mark an account down.
type Prober struct {
	tracker     *Tracker
	registry    ProbeRegistry
	global      bool // default for accounts with HealthProbeEnabled == nil
	concurrency int

	mu    sync.Mutex
	state map[int64]*probeState
}

// NewProber wires a prober. global is the OMNIHUB_HEALTH_PROBE_ENABLED
// default applied to accounts that don't override; concurrency caps how
// many upstream probes run at once (clamped to 1..16).
func NewProber(tracker *Tracker, registry ProbeRegistry, global bool, concurrency int) *Prober {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 16 {
		concurrency = 16
	}
	return &Prober{
		tracker:     tracker,
		registry:    registry,
		global:      global,
		concurrency: concurrency,
		state:       make(map[int64]*probeState),
	}
}

// Start runs one immediate probe pass, then re-probes every interval
// until ctx is cancelled. snapshot supplies the current account set each
// tick (typically account.Pool.All), so added accounts and per-account
// opt-in edits are picked up automatically. The loop ALWAYS runs even
// when the global default is false, because a per-account
// HealthProbeEnabled=true must still be honoured.
func (p *Prober) Start(ctx context.Context, snapshot func() []*provider.Account, interval time.Duration) {
	if p == nil || p.tracker == nil || p.registry == nil {
		return
	}
	p.probeAll(ctx, snapshot())
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.probeAll(ctx, snapshot())
			}
		}
	}()
}

type probeResult struct {
	id     int64
	name   string
	status provider.TestStatus
	msg    string
}

// probeAll probes every opted-in, testable account once (bounded
// concurrency), then applies the verdicts to the tracker, then GCs state
// for accounts no longer present.
func (p *Prober) probeAll(ctx context.Context, accounts []*provider.Account) {
	live := make(map[int64]struct{}, len(accounts))
	var targets []*provider.Account
	for _, a := range accounts {
		live[a.ID] = struct{}{}
		if !p.shouldProbe(a) {
			continue
		}
		if _, ok := p.tester(a); ok {
			targets = append(targets, a)
		}
	}

	results := make([]probeResult, 0, len(targets))
	var rmu sync.Mutex
	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup
	for _, a := range targets {
		wg.Add(1)
		go func(a *provider.Account) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tester, ok := p.tester(a)
			if !ok {
				return
			}
			cctx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()
			r := tester.Test(cctx, a)
			if r.Status == provider.TestYellow {
				return // slow / rate-limited but reachable → never eject
			}
			rmu.Lock()
			results = append(results, probeResult{id: a.ID, name: a.Name, status: r.Status, msg: r.Message})
			rmu.Unlock()
		}(a)
	}
	wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range results {
		s := p.state[r.id]
		if s == nil {
			s = &probeState{}
			p.state[r.id] = s
		}
		switch r.status {
		case provider.TestGreen:
			s.consecReds = 0
			s.consecGreens++
			if s.consecGreens >= probeGreenThreshold {
				// RecordSuccess on a closed account just zeroes failure
				// noise; on open/half-open it walks the breaker toward
				// closed — recovery before real traffic.
				p.tracker.RecordSuccess(r.id)
			}
		case provider.TestRed:
			s.consecGreens = 0
			s.consecReds++
			if s.consecReds >= probeRedThreshold {
				// Feed RecordFailure EVERY sustained-red tick (not once)
				// so the breaker's own FailureThreshold is eventually
				// crossed and the account opens.
				p.tracker.RecordFailure(r.id, errors.New("health probe: "+r.msg))
				if s.consecReds == probeRedThreshold {
					slog.Warn("active health probe failing",
						"account", r.name, "consecutive_reds", s.consecReds, "reason", r.msg)
				}
			}
		}
	}
	// GC counters for accounts that left the pool (deleted / disabled).
	for id := range p.state {
		if _, ok := live[id]; !ok {
			delete(p.state, id)
		}
	}
}

// shouldProbe resolves the per-account opt-in against the global default.
// NOTE: provider.Account has no Enabled field — the pool only ever holds
// enabled rows — so there is nothing to check here beyond the opt-in.
func (p *Prober) shouldProbe(a *provider.Account) bool {
	if a == nil {
		return false
	}
	if a.HealthProbeEnabled != nil {
		return *a.HealthProbeEnabled
	}
	return p.global
}

// tester returns the account driver's optional connectivity Tester, or
// (nil,false) when the driver is unregistered or doesn't implement it.
func (p *Prober) tester(a *provider.Account) (provider.Tester, bool) {
	d, ok := p.registry.Get(a.Provider)
	if !ok {
		return nil, false
	}
	t, ok := d.(provider.Tester)
	return t, ok
}
