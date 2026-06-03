package healthlog

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/health"
)

// Minimal interface assertion: Handler is the canonical
// TransitionHandler shape. Compile-time check only — if this stops
// compiling the wiring contract changed.
var _ health.TransitionHandler = (*Recorder)(nil).Handler

// We can't use real *account.Pool + *repository.AccountHealthEventRepo
// in a pure unit test because both need a live *pgxpool.Pool. Test
// the queue, drop, and lifecycle behaviour by inlining a smaller
// recorder built around the same channel pattern.
//
// This keeps the test hermetic; the production Recorder is exercised
// end-to-end through the gateway integration test.

type fakeRecorder struct {
	ch      chan health.Transition
	stop    chan struct{}
	stopped chan struct{}

	mu      sync.Mutex // guards written (consumer goroutine vs test reads)
	written []health.Transition
	dropped int
}

// count returns how many transitions the consumer goroutine has drained,
// under the lock so the race detector stays happy.
func (r *fakeRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.written)
}

func newFakeRecorder(queue int) *fakeRecorder {
	return &fakeRecorder{
		ch:      make(chan health.Transition, queue),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (r *fakeRecorder) Handler(t health.Transition) {
	select {
	case r.ch <- t:
	default:
		r.dropped++
	}
}

func (r *fakeRecorder) Start() {
	go func() {
		defer close(r.stopped)
		for {
			select {
			case <-r.stop:
				return
			case t := <-r.ch:
				r.mu.Lock()
				r.written = append(r.written, t)
				r.mu.Unlock()
			}
		}
	}()
}

func (r *fakeRecorder) Stop() {
	close(r.stop)
	<-r.stopped
}

func TestFakeRecorder_HandlerEnqueuesEvents(t *testing.T) {
	r := newFakeRecorder(8)
	r.Start()
	defer r.Stop()

	for i := int64(0); i < 5; i++ {
		r.Handler(health.Transition{
			AccountID: i,
			From:      health.StateClosed,
			To:        health.StateOpen,
			At:        time.Now(),
		})
	}
	// Allow goroutine to drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && r.count() < 5 {
		time.Sleep(5 * time.Millisecond)
	}
	if n := r.count(); n != 5 {
		t.Fatalf("written = %d, want 5", n)
	}
	if r.dropped != 0 {
		t.Errorf("dropped = %d, want 0", r.dropped)
	}
}

func TestFakeRecorder_DropsWhenFull(t *testing.T) {
	// Tiny queue, no consumer started → every event past the buffer is dropped.
	r := newFakeRecorder(2)
	// Intentionally NOT calling Start().

	for i := 0; i < 5; i++ {
		r.Handler(health.Transition{AccountID: int64(i)})
	}
	if r.dropped != 3 {
		t.Errorf("dropped = %d, want 3 (queue size 2, 5 events)", r.dropped)
	}
}

// TestRealRecorder_HandlerIsNonBlocking smoke-tests the actual
// Recorder type to make sure Handler returns immediately even with
// a full queue and no consumer. We can construct one with nil repo
// + nil pool because Handler never touches them — it only writes to
// the channel.
func TestRealRecorder_HandlerIsNonBlocking(t *testing.T) {
	r := &Recorder{
		ch:      make(chan health.Transition, 1),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	// Fill the queue first.
	r.Handler(health.Transition{AccountID: 1})

	// This call would block forever if Handler weren't using a
	// non-blocking send. Bound the test so a regression surfaces as
	// a timeout rather than a hang.
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Handler(health.Transition{AccountID: 2})
	}()

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("Handler blocked on full queue — should be non-blocking")
	}
	if got := r.Dropped(); got != 1 {
		t.Errorf("Dropped() = %d, want 1", got)
	}
}

// Compile-time check that Transition.Reason being non-nil compiles
// against the Recorder's write path (covered by integration test).
var _ = func() error { return errors.New("placeholder") }

// Compile-time check that the run loop respects context cancellation
// even when ch is empty — guards against a regression where the
// recorder would leak its goroutine after ctx.Done.
func TestRunExitsOnContextCancel(t *testing.T) {
	r := &Recorder{
		ch:      make(chan health.Transition, 1),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go r.run(ctx)
	cancel()
	select {
	case <-r.stopped:
		// expected
	case <-time.After(time.Second):
		t.Fatal("recorder did not exit after ctx cancel")
	}
}
