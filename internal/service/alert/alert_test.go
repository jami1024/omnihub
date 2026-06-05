package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/health"
)

type fakeNotifier struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func (f *fakeNotifier) Name() string { return "fake" }

func (f *fakeNotifier) Send(_ context.Context, e Event) error {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
	if f.ch != nil {
		f.ch <- e
	}
	return nil
}

func openTransition(id int64) health.Transition {
	return health.Transition{
		AccountID:    id,
		From:         health.StateClosed,
		To:           health.StateOpen,
		FailureCount: 5,
		At:           time.Unix(1000, 0),
	}
}

func TestNewReturnsNilWithoutNotifiers(t *testing.T) {
	if a := New(nil, nil, 0); a != nil {
		t.Fatal("New with no notifiers should return nil")
	}
}

func TestHandlerDeliversOpenAlert(t *testing.T) {
	fn := &fakeNotifier{ch: make(chan Event, 4)}
	a := New([]Notifier{fn}, nil, time.Minute)
	a.Start(context.Background())
	defer a.Stop()

	a.Handler(openTransition(7))

	select {
	case e := <-fn.ch:
		if e.Level != LevelWarning {
			t.Errorf("level = %q, want warning", e.Level)
		}
		if e.AccountID != 7 {
			t.Errorf("account id = %d, want 7", e.AccountID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no alert delivered for circuit OPEN")
	}
}

func TestHandlerDeliversRecoveryAlert(t *testing.T) {
	fn := &fakeNotifier{ch: make(chan Event, 4)}
	a := New([]Notifier{fn}, nil, time.Minute)
	a.Start(context.Background())
	defer a.Stop()

	a.Handler(health.Transition{AccountID: 3, From: health.StateHalfOpen, To: health.StateClosed, At: time.Unix(1000, 0)})

	select {
	case e := <-fn.ch:
		if e.Level != LevelInfo {
			t.Errorf("level = %q, want info", e.Level)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no alert delivered for recovery")
	}
}

func TestHandlerIgnoresNonAlertTransitions(t *testing.T) {
	fn := &fakeNotifier{ch: make(chan Event, 4)}
	a := New([]Notifier{fn}, nil, time.Minute)
	a.Start(context.Background())
	defer a.Stop()

	// open -> half-open is a probe attempt, not alert-worthy.
	a.Handler(health.Transition{AccountID: 1, From: health.StateOpen, To: health.StateHalfOpen, At: time.Unix(1000, 0)})

	select {
	case e := <-fn.ch:
		t.Fatalf("unexpected alert for open->half-open: %+v", e)
	case <-time.After(200 * time.Millisecond):
		// good — nothing delivered
	}
}

func TestThrottleSuppressesDuplicates(t *testing.T) {
	fn := &fakeNotifier{ch: make(chan Event, 8)}
	a := New([]Notifier{fn}, nil, time.Minute)
	fixed := time.Unix(5000, 0)
	a.now = func() time.Time { return fixed }
	a.Start(context.Background())
	defer a.Stop()

	a.Handler(openTransition(9))
	a.Handler(openTransition(9)) // identical, within throttle window

	// First must arrive.
	select {
	case <-fn.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("first alert not delivered")
	}
	// Second must be suppressed.
	select {
	case e := <-fn.ch:
		t.Fatalf("throttle failed: duplicate delivered %+v", e)
	case <-time.After(200 * time.Millisecond):
	}

	// After the window elapses, an identical alert flows again.
	a.now = func() time.Time { return fixed.Add(2 * time.Minute) }
	a.Handler(openTransition(9))
	select {
	case <-fn.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("alert not delivered after throttle window elapsed")
	}
}

func TestConfigNotifiers(t *testing.T) {
	cfg := Config{WebhookURL: "http://w", FeishuURL: "http://f", DingTalkURL: "http://d"}
	ns := cfg.Notifiers()
	if len(ns) != 3 {
		t.Fatalf("notifier count = %d, want 3", len(ns))
	}
	if New((Config{}).Notifiers(), nil, 0) != nil {
		t.Fatal("empty config should yield a nil Alerter")
	}
}
