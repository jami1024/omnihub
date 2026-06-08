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

// ProbeConfig tunes the active background prober. It is intentionally separate
// from the circuit-breaker Config: probe de-bouncing decides when probe
// verdicts become breaker events; the breaker then decides when to open.
type ProbeConfig struct {
	GlobalDefault  bool
	Interval       time.Duration
	Concurrency    int
	RedThreshold   int
	GreenThreshold int
	Timeout        time.Duration
	SlowThreshold  time.Duration
}

// DefaultProbeConfig returns the historical defaults used before gateway
// settings became runtime-configurable.
func DefaultProbeConfig() ProbeConfig {
	return ProbeConfig{
		GlobalDefault:  false,
		Interval:       60 * time.Second,
		Concurrency:    4,
		RedThreshold:   probeRedThreshold,
		GreenThreshold: probeGreenThreshold,
		Timeout:        probeTimeout,
		SlowThreshold:  provider.DefaultSlowThreshold,
	}
}

// NormalizeProbeConfig clamps unsafe or invalid values to production-safe
// ranges. It is used both for env parsing and DB-backed runtime settings.
func NormalizeProbeConfig(cfg ProbeConfig) ProbeConfig {
	def := DefaultProbeConfig()
	if cfg.Interval < 10*time.Second {
		cfg.Interval = def.Interval
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.Concurrency > 16 {
		cfg.Concurrency = 16
	}
	if cfg.RedThreshold < 1 {
		cfg.RedThreshold = def.RedThreshold
	}
	if cfg.GreenThreshold < 1 {
		cfg.GreenThreshold = def.GreenThreshold
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.SlowThreshold <= 0 {
		cfg.SlowThreshold = def.SlowThreshold
	}
	return cfg
}

// ProbeConfigSource provides the latest prober settings. A nil source or
// zero-valued config falls back to DefaultProbeConfig().
type ProbeConfigSource func() ProbeConfig

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
	tracker  *Tracker
	registry ProbeRegistry
	config   ProbeConfigSource

	mu    sync.Mutex
	state map[int64]*probeState
}

// NewProber wires a prober. global is the OMNIHUB_HEALTH_PROBE_ENABLED
// default applied to accounts that don't override; concurrency caps how
// many upstream probes run at once (clamped to 1..16).
func NewProber(tracker *Tracker, registry ProbeRegistry, global bool, concurrency int) *Prober {
	cfg := NormalizeProbeConfig(ProbeConfig{GlobalDefault: global, Concurrency: concurrency})
	cfg.Interval = DefaultProbeConfig().Interval
	cfg.Timeout = DefaultProbeConfig().Timeout
	cfg.RedThreshold = DefaultProbeConfig().RedThreshold
	cfg.GreenThreshold = DefaultProbeConfig().GreenThreshold
	cfg.SlowThreshold = DefaultProbeConfig().SlowThreshold
	return NewProberWithConfig(tracker, registry, func() ProbeConfig { return cfg })
}

// NewProberWithConfig wires a prober to a dynamic config source.
func NewProberWithConfig(tracker *Tracker, registry ProbeRegistry, source ProbeConfigSource) *Prober {
	return &Prober{
		tracker:  tracker,
		registry: registry,
		config:   source,
		state:    make(map[int64]*probeState),
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
		if interval <= 0 {
			interval = p.currentConfig().Interval
		}
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				p.probeAll(ctx, snapshot())
				timer.Reset(p.currentConfig().Interval)
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
	cfg := p.currentConfig()
	live := make(map[int64]struct{}, len(accounts))
	var targets []*provider.Account
	for _, a := range accounts {
		live[a.ID] = struct{}{}
		if !p.shouldProbe(a, cfg) {
			continue
		}
		if _, ok := p.tester(a); ok {
			targets = append(targets, a)
		}
	}

	results := make([]probeResult, 0, len(targets))
	var rmu sync.Mutex
	sem := make(chan struct{}, cfg.Concurrency)
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
			cctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
			defer cancel()
			r := tester.Test(cctx, a)
			if r.Status == provider.TestGreen && time.Duration(r.LatencyMs)*time.Millisecond > cfg.SlowThreshold {
				r.Status = provider.TestYellow
				r.Message = "reachable but slow"
			}
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
			if s.consecGreens >= cfg.GreenThreshold {
				// RecordSuccess on a closed account just zeroes failure
				// noise; on open/half-open it walks the breaker toward
				// closed — recovery before real traffic.
				p.tracker.RecordSuccess(r.id)
			}
		case provider.TestRed:
			s.consecGreens = 0
			s.consecReds++
			if s.consecReds >= cfg.RedThreshold {
				// Feed RecordFailure EVERY sustained-red tick (not once)
				// so the breaker's own FailureThreshold is eventually
				// crossed and the account opens.
				p.tracker.RecordFailure(r.id, errors.New("health probe: "+r.msg))
				if s.consecReds == cfg.RedThreshold {
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
func (p *Prober) currentConfig() ProbeConfig {
	if p == nil || p.config == nil {
		return DefaultProbeConfig()
	}
	return NormalizeProbeConfig(p.config())
}

func (p *Prober) shouldProbe(a *provider.Account, cfg ProbeConfig) bool {
	if a == nil {
		return false
	}
	if a.HealthProbeEnabled != nil {
		return *a.HealthProbeEnabled
	}
	return cfg.GlobalDefault
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
