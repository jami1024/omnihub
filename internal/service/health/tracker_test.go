package health

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClock advances under test control so cooldown timing is
// deterministic.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTracker(t *testing.T) (*Tracker, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tr := New(Config{
		FailureThreshold:         3,
		OpenDuration:             30 * time.Second,
		HalfOpenSuccessThreshold: 1,
	})
	tr.now = clock.Now
	return tr, clock
}

func TestUnknownAccountIsAvailable(t *testing.T) {
	tr := New(DefaultConfig())
	if !tr.IsAvailable(99) {
		t.Error("an account never seen should be available")
	}
}

func TestOpensAfterThreshold(t *testing.T) {
	tr, _ := newTracker(t)
	err := errors.New("boom")

	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, err)
	}

	if tr.IsAvailable(1) {
		t.Error("account should be unavailable after threshold failures")
	}
	if got := tr.Snapshot(1).State; got != StateOpen {
		t.Errorf("state: want open, got %s", got)
	}
}

func TestSuccessResetsClosedFailures(t *testing.T) {
	tr, _ := newTracker(t)
	tr.RecordFailure(1, errors.New("x"))
	tr.RecordFailure(1, errors.New("x"))
	tr.RecordSuccess(1)
	if got := tr.Snapshot(1).FailureCount; got != 0 {
		t.Errorf("success in closed should reset failure_count, got %d", got)
	}
}

func TestOpenTransitionsToHalfOpenAfterCooldown(t *testing.T) {
	tr, clock := newTracker(t)
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	if tr.IsAvailable(1) {
		t.Fatal("should be unavailable right after opening")
	}

	clock.Advance(31 * time.Second) // exceed OpenDuration
	if !tr.IsAvailable(1) {
		t.Fatal("should be available (half-open) after cooldown")
	}
	if got := tr.Snapshot(1).State; got != StateHalfOpen {
		t.Errorf("state: want half-open, got %s", got)
	}
}

func TestHalfOpenSuccessClosesCircuit(t *testing.T) {
	tr, clock := newTracker(t)
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	clock.Advance(31 * time.Second)
	_ = tr.IsAvailable(1) // transition to half-open

	tr.RecordSuccess(1)

	snap := tr.Snapshot(1)
	if snap.State != StateClosed {
		t.Errorf("state: want closed, got %s", snap.State)
	}
	if snap.FailureCount != 0 {
		t.Errorf("failure_count: want 0, got %d", snap.FailureCount)
	}
	if !snap.OpenUntil.IsZero() {
		t.Errorf("open_until should be cleared, got %v", snap.OpenUntil)
	}
}

func TestHalfOpenFailureReopens(t *testing.T) {
	tr, clock := newTracker(t)
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	openedAt := clock.Now()
	clock.Advance(31 * time.Second)
	_ = tr.IsAvailable(1) // half-open

	// Trial fails: should reopen with refreshed openUntil.
	tr.RecordFailure(1, errors.New("trial failed"))

	snap := tr.Snapshot(1)
	if snap.State != StateOpen {
		t.Errorf("state: want open, got %s", snap.State)
	}
	if !snap.OpenUntil.After(openedAt.Add(30 * time.Second)) {
		t.Errorf("openUntil should be in the future, got %v", snap.OpenUntil)
	}
}

func TestRepeatedFailuresInOpenExtendCooldown(t *testing.T) {
	tr, clock := newTracker(t)
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	openedAt := tr.Snapshot(1).OpenUntil

	clock.Advance(10 * time.Second)
	tr.RecordFailure(1, errors.New("more failures"))

	extended := tr.Snapshot(1).OpenUntil
	if !extended.After(openedAt) {
		t.Errorf("openUntil should advance on additional failures, got %v after %v", extended, openedAt)
	}
}

func TestDisabledConfigIsNoOp(t *testing.T) {
	tr := New(Config{FailureThreshold: 0})
	for i := 0; i < 100; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	if !tr.IsAvailable(1) {
		t.Error("disabled tracker should always report available")
	}
	if tr.Snapshot(1).State != StateClosed {
		t.Errorf("disabled tracker should leave state untouched")
	}
}

func TestPerAccountIsolation(t *testing.T) {
	tr, _ := newTracker(t)
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	if !tr.IsAvailable(2) {
		t.Error("failures on account 1 must not affect account 2")
	}
}

func TestResetClearsState(t *testing.T) {
	tr, _ := newTracker(t)
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	tr.Reset(1)
	if !tr.IsAvailable(1) {
		t.Error("Reset should restore availability")
	}
	if tr.Snapshot(1).FailureCount != 0 {
		t.Error("Reset should clear failure count")
	}
}

func TestTransitionHandlerEmitsClosedToOpen(t *testing.T) {
	tr, _ := newTracker(t)
	var got []Transition
	tr.SetTransitionHandler(func(t Transition) { got = append(got, t) })

	err := errors.New("boom")
	tr.RecordFailure(1, err) // 1/3 — no transition
	tr.RecordFailure(1, err) // 2/3 — no transition
	if len(got) != 0 {
		t.Fatalf("transitions before threshold = %d, want 0", len(got))
	}
	tr.RecordFailure(1, err) // 3/3 — closed → open

	if len(got) != 1 {
		t.Fatalf("transitions = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.From != StateClosed || ev.To != StateOpen {
		t.Errorf("transition %s → %s, want closed → open", ev.From, ev.To)
	}
	if ev.AccountID != 1 || ev.FailureCount != 3 {
		t.Errorf("ev = %+v", ev)
	}
	if ev.Reason == nil || ev.Reason.Error() != "boom" {
		t.Errorf("reason = %v, want \"boom\"", ev.Reason)
	}
}

func TestTransitionHandlerEmitsOpenToHalfOpenAndClosed(t *testing.T) {
	tr, clock := newTracker(t)
	var got []Transition
	tr.SetTransitionHandler(func(t Transition) { got = append(got, t) })

	err := errors.New("x")
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, err)
	}
	// transition 1: closed → open
	clock.Advance(31 * time.Second)
	_ = tr.IsAvailable(1) // transition 2: open → half-open (cooldown expired)
	tr.RecordSuccess(1)   // transition 3: half-open → closed (threshold=1)

	if len(got) != 3 {
		t.Fatalf("transitions = %d, want 3: %+v", len(got), got)
	}
	if got[0].From != StateClosed || got[0].To != StateOpen {
		t.Errorf("transition[0] = %s → %s", got[0].From, got[0].To)
	}
	if got[1].From != StateOpen || got[1].To != StateHalfOpen {
		t.Errorf("transition[1] = %s → %s", got[1].From, got[1].To)
	}
	if got[1].Reason != nil {
		t.Errorf("cooldown-expiry transition should have nil reason, got %v", got[1].Reason)
	}
	if got[2].From != StateHalfOpen || got[2].To != StateClosed {
		t.Errorf("transition[2] = %s → %s", got[2].From, got[2].To)
	}
}

func TestTransitionHandlerSilentOnNoStateChange(t *testing.T) {
	tr, _ := newTracker(t)
	var got []Transition
	tr.SetTransitionHandler(func(t Transition) { got = append(got, t) })

	// Already-open breaker absorbing extra failures: NO new transitions.
	for i := 0; i < 3; i++ {
		tr.RecordFailure(1, errors.New("x"))
	}
	tr.RecordFailure(1, errors.New("x")) // already open → open: no emit
	tr.RecordFailure(1, errors.New("x"))
	if len(got) != 1 {
		t.Errorf("expected only the initial closed→open transition, got %d: %+v", len(got), got)
	}
}

func TestConcurrentRecording(t *testing.T) {
	tr := New(DefaultConfig())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tr.RecordFailure(1, errors.New("x"))
				tr.RecordSuccess(1)
				tr.IsAvailable(1)
			}
		}()
	}
	wg.Wait()
	// No deadlock or race — that's the test. If `-race` is on, the
	// detector will surface any unguarded access.
}

func TestRateLimitCooldown(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	tr := New(DefaultConfig())
	tr.now = clock.Now

	tr.SetCooldown(7, clock.Now().Add(90*time.Second))
	if tr.IsAvailable(7) {
		t.Fatal("cooled-down account must be unavailable")
	}
	if until := tr.CooldownUntil(7); until.IsZero() {
		t.Fatal("CooldownUntil should report the active deadline")
	}

	clock.Advance(91 * time.Second)
	if !tr.IsAvailable(7) {
		t.Fatal("cooldown must lift exactly after the deadline")
	}
	if until := tr.CooldownUntil(7); !until.IsZero() {
		t.Fatalf("expired cooldown should read as zero, got %v", until)
	}

	// Cooldown is independent of circuit state: a healthy account with
	// no failures still parks.
	tr.SetCooldown(8, clock.Now().Add(time.Minute))
	tr.RecordSuccess(8)
	if tr.IsAvailable(8) {
		t.Fatal("success recording must not lift a rate-limit cooldown")
	}

	// Clearing via zero time.
	tr.SetCooldown(8, time.Time{})
	if !tr.IsAvailable(8) {
		t.Fatal("zero-time SetCooldown should clear the park")
	}
}
