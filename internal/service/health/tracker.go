// Package health implements a per-account circuit breaker so the
// resolver can isolate misbehaving upstreams and the handler can
// retry on transient failures with a different account.
//
// State machine (one instance per account):
//
//	  ┌────────┐ failures ≥ threshold  ┌──────┐
//	  │ Closed │ ─────────────────────▶│ Open │
//	  └────────┘                       └───┬──┘
//	      ▲                                │ time > openUntil
//	      │ N consecutive successes        ▼
//	  ┌───┴────────┐ first failure   ┌────────────┐
//	  │ Half-Open  │ ◀──────────────│ Half-Open   │
//	  └────────────┘                 └────────────┘
//	         │ N successes
//	         ▼
//	    Closed (reset counters)
//
// The model and defaults follow claude-code-hub's circuit breaker.
// State is kept in process memory only — this is acceptable for a
// single-instance MVP. Multi-instance deployments will share state
// via Redis in a follow-up commit.
package health

import (
	"errors"
	"sync"
	"time"
)

// CircuitState enumerates the three positions of the per-account
// state machine.
type CircuitState string

const (
	StateClosed   CircuitState = "closed"
	StateOpen     CircuitState = "open"
	StateHalfOpen CircuitState = "half-open"
)

// Config tunes how aggressively the breaker trips and recovers.
type Config struct {
	// FailureThreshold is the number of consecutive failures (in the
	// closed state) that flips the breaker to open. 0 disables the
	// breaker entirely.
	FailureThreshold int
	// OpenDuration is how long the breaker stays open before allowing
	// a half-open trial request.
	OpenDuration time.Duration
	// HalfOpenSuccessThreshold is the number of successes required in
	// the half-open state to fully close the breaker.
	HalfOpenSuccessThreshold int
}

// DefaultConfig returns sensible production defaults: 5 failures
// → 30 s cooldown → 1 success closes.
func DefaultConfig() Config {
	return Config{
		FailureThreshold:         5,
		OpenDuration:             30 * time.Second,
		HalfOpenSuccessThreshold: 1,
	}
}

// Disabled reports whether the configuration turns the breaker off.
// A disabled tracker always reports accounts as available.
func (c Config) Disabled() bool { return c.FailureThreshold <= 0 }

// Snapshot is a read-only view of one account's health, useful for
// /readyz output and admin UIs.
type Snapshot struct {
	State           CircuitState
	FailureCount    int
	LastFailure     time.Time
	OpenUntil       time.Time
	HalfOpenSuccess int
}

// ConfigLookup resolves the effective configuration for one account.
// Returning a zero Config means "fall back to the tracker default".
// The lookup runs on every IsAvailable / RecordFailure / RecordSuccess
// call so the implementation should be fast (typically O(1) map
// access into the account pool).
type ConfigLookup func(accountID int64) Config

// Tracker is the per-process circuit breaker registry. It is safe
// for concurrent use.
type Tracker struct {
	defaultConfig Config
	lookup        ConfigLookup    // nil = always use defaultConfig
	now           func() time.Time // pluggable clock for tests

	mu     sync.Mutex
	states map[int64]*accountState
}

// accountState is the per-account mutable record. Access is guarded
// by Tracker.mu — there is no per-account lock because contention
// per account is minimal (a few transitions per minute).
type accountState struct {
	state           CircuitState
	failureCount    int
	lastFailure     time.Time
	openUntil       time.Time
	halfOpenSuccess int
}

// New constructs a tracker with the given default configuration. A
// zero Config falls back to DefaultConfig.
//
// Per-account overrides become active by calling SetConfigLookup.
// Without a lookup every account shares the default.
func New(cfg Config) *Tracker {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	return &Tracker{
		defaultConfig: cfg,
		now:           time.Now,
		states:        make(map[int64]*accountState),
	}
}

// SetConfigLookup installs a per-account config resolver. Pass nil to
// remove a previously installed lookup. Safe to call after the
// tracker is in use; subsequent operations consult the new lookup.
func (t *Tracker) SetConfigLookup(lookup ConfigLookup) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lookup = lookup
}

// configFor returns the effective config for one account.
// Caller must hold t.mu.
func (t *Tracker) configFor(accountID int64) Config {
	if t.lookup == nil {
		return t.defaultConfig
	}
	cfg := t.lookup(accountID)
	if cfg == (Config{}) {
		return t.defaultConfig
	}
	return cfg
}

// IsAvailable reports whether the resolver should consider this
// account for routing. The check has a side effect: when the
// configured open duration has elapsed, the breaker transitions to
// half-open and the call returns true (allowing a trial request).
func (t *Tracker) IsAvailable(accountID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	cfg := t.configFor(accountID)
	if cfg.Disabled() {
		return true
	}

	s, ok := t.states[accountID]
	if !ok {
		return true
	}
	if s.state == StateClosed || s.state == StateHalfOpen {
		return true
	}

	// State == Open. Check if we've cooled off enough to allow a
	// trial request.
	if t.now().After(s.openUntil) {
		s.state = StateHalfOpen
		s.halfOpenSuccess = 0
		return true
	}
	return false
}

// RecordSuccess clears failure counters or, when in half-open, moves
// the breaker toward closed.
func (t *Tracker) RecordSuccess(accountID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg := t.configFor(accountID)
	if cfg.Disabled() {
		return
	}
	s := t.getOrCreate(accountID)

	switch s.state {
	case StateClosed:
		// A successful call resets transient failure noise.
		s.failureCount = 0
		s.lastFailure = time.Time{}
	case StateHalfOpen:
		s.halfOpenSuccess++
		if s.halfOpenSuccess >= cfg.HalfOpenSuccessThreshold {
			s.state = StateClosed
			s.failureCount = 0
			s.lastFailure = time.Time{}
			s.openUntil = time.Time{}
			s.halfOpenSuccess = 0
		}
	case StateOpen:
		// Should not happen — IsAvailable should have moved Open to
		// Half-Open before the caller had a chance to make a request.
		// Treat as half-open success for resilience.
		s.state = StateHalfOpen
		s.halfOpenSuccess = 1
		if s.halfOpenSuccess >= cfg.HalfOpenSuccessThreshold {
			s.state = StateClosed
			s.failureCount = 0
			s.openUntil = time.Time{}
			s.halfOpenSuccess = 0
		}
	}
}

// RecordFailure increments the per-account failure counter and opens
// the breaker if the threshold is crossed. A failure in half-open
// state immediately re-opens the breaker.
func (t *Tracker) RecordFailure(accountID int64, _ error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg := t.configFor(accountID)
	if cfg.Disabled() {
		return
	}
	s := t.getOrCreate(accountID)

	now := t.now()
	s.failureCount++
	s.lastFailure = now

	switch s.state {
	case StateClosed:
		if s.failureCount >= cfg.FailureThreshold {
			s.state = StateOpen
			s.openUntil = now.Add(cfg.OpenDuration)
			s.halfOpenSuccess = 0
		}
	case StateHalfOpen:
		// Trial failed: re-open immediately.
		s.state = StateOpen
		s.openUntil = now.Add(cfg.OpenDuration)
		s.halfOpenSuccess = 0
	case StateOpen:
		// Already open; refresh openUntil so back-to-back failure
		// storms extend the cooldown.
		s.openUntil = now.Add(cfg.OpenDuration)
	}
}

// Snapshot returns the current state of one account. The zero
// Snapshot is returned for accounts that have never been recorded.
func (t *Tracker) Snapshot(accountID int64) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.states[accountID]
	if !ok {
		return Snapshot{State: StateClosed}
	}
	return Snapshot{
		State:           s.state,
		FailureCount:    s.failureCount,
		LastFailure:     s.lastFailure,
		OpenUntil:       s.openUntil,
		HalfOpenSuccess: s.halfOpenSuccess,
	}
}

// Reset forces an account back to closed. Useful for admin
// operations ("the upstream came back, force the breaker closed").
func (t *Tracker) Reset(accountID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.states, accountID)
}

func (t *Tracker) getOrCreate(accountID int64) *accountState {
	s, ok := t.states[accountID]
	if !ok {
		s = &accountState{state: StateClosed}
		t.states[accountID] = s
	}
	return s
}

// ErrAllUnavailable is the canonical error code the handler uses when
// every candidate account is in the open state.
var ErrAllUnavailable = errors.New("all candidate accounts are unavailable")
