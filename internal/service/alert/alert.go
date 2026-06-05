// Package alert delivers operational notifications about account health
// to external channels (generic webhook, Feishu, DingTalk). It exists so
// an operator learns the moment a shared upstream account trips its
// circuit breaker — without watching logs or a dashboard.
//
// The Alerter mirrors service/healthlog's hot-path discipline: the
// TransitionHandler hook only drops an Event into a bounded channel and
// returns, while a background goroutine fans it out to the configured
// notifiers. A per-(title, account) throttle suppresses flap storms.
package alert

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/health"
)

const (
	// queueSize bounds in-flight alerts; a brief flap storm is absorbed
	// rather than back-pressuring the breaker.
	queueSize = 128

	// sendTimeout caps a single notifier delivery.
	sendTimeout = 10 * time.Second

	// defaultThrottle is the minimum gap between two identical alerts
	// (same title + account) when the caller passes 0.
	defaultThrottle = 5 * time.Minute
)

// Level classifies an alert's severity for the notifier formatting.
type Level string

const (
	LevelWarning Level = "warning"
	LevelInfo    Level = "info"
)

// Event is one normalized alert ready for delivery.
type Event struct {
	Level       Level
	Title       string
	Text        string
	AccountID   int64
	AccountName string
	At          time.Time
}

// throttleKey identifies "the same alert" for de-duplication.
func (e Event) throttleKey() string {
	return fmt.Sprintf("%s|%d", e.Title, e.AccountID)
}

// TestEvent builds a synthetic alert for the admin "test send" action so
// an operator can verify a channel before relying on it.
func TestEvent(channelName string) Event {
	return Event{
		Level: LevelInfo,
		Title: "test alert",
		Text:  fmt.Sprintf("Test alert from OmniHub for channel %q. If you can read this, delivery works.", channelName),
		At:    time.Now(),
	}
}

// Notifier delivers an Event to one external channel. Implementations
// must be safe for concurrent use.
type Notifier interface {
	Send(ctx context.Context, e Event) error
	Name() string
}

// Alerter fans health transitions out to notifiers asynchronously. The
// notifier set is resolved from source on each delivery, so channels
// added or disabled at runtime (via the DB-backed pool) take effect
// without a restart.
type Alerter struct {
	source   func() []Notifier
	pool     *account.Pool // resolves account names; may be nil
	throttle time.Duration
	now      func() time.Time

	ch      chan Event
	stop    chan struct{}
	stopped chan struct{}
	dropped atomic.Int64

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// New builds an Alerter whose notifier set comes from source (evaluated
// per delivery). Returns nil when source is nil, so callers can treat
// "alerting disabled" as a nil Alerter. pool may be nil (account names
// degrade to the numeric id). A throttle of 0 uses defaultThrottle.
func New(source func() []Notifier, pool *account.Pool, throttle time.Duration) *Alerter {
	if source == nil {
		return nil
	}
	if throttle <= 0 {
		throttle = defaultThrottle
	}
	return &Alerter{
		source:   source,
		pool:     pool,
		throttle: throttle,
		now:      time.Now,
		ch:       make(chan Event, queueSize),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
		lastSent: make(map[string]time.Time),
	}
}

// NotifierNames lists the currently-resolved channels, for startup
// logging. The set may grow/shrink later as channels are managed at runtime.
func (a *Alerter) NotifierNames() []string {
	ns := a.source()
	names := make([]string, 0, len(ns))
	for _, n := range ns {
		names = append(names, n.Name())
	}
	return names
}

// Start spawns the delivery goroutine. Returns immediately.
func (a *Alerter) Start(ctx context.Context) { go a.run(ctx) }

// Stop drains pending alerts and waits for the goroutine to exit.
func (a *Alerter) Stop() {
	select {
	case <-a.stop:
	default:
		close(a.stop)
	}
	<-a.stopped
}

// Dropped reports how many alerts were dropped because the queue was full.
func (a *Alerter) Dropped() int64 { return a.dropped.Load() }

// Handler is the health.TransitionHandler hook. Non-blocking: it maps the
// transition to an Event (if alert-worthy) and enqueues it. Alert-worthy
// transitions are: any → OPEN (account down) and half-open → CLOSED
// (account recovered). Other transitions (e.g. open → half-open probe)
// are ignored.
func (a *Alerter) Handler(t health.Transition) {
	name := a.accountName(t.AccountID)
	var e Event
	switch {
	case t.To == health.StateOpen:
		reason := "no error recorded"
		if t.Reason != nil {
			reason = t.Reason.Error()
		}
		e = Event{
			Level: LevelWarning,
			Title: fmt.Sprintf("account %q circuit OPEN", name),
			Text: fmt.Sprintf("Account %q (id=%d) tripped OPEN after %d consecutive failures. Last error: %s.",
				name, t.AccountID, t.FailureCount, reason),
		}
	case t.To == health.StateClosed && t.From == health.StateHalfOpen:
		e = Event{
			Level: LevelInfo,
			Title: fmt.Sprintf("account %q recovered", name),
			Text:  fmt.Sprintf("Account %q (id=%d) recovered and is back in rotation.", name, t.AccountID),
		}
	default:
		return
	}
	e.AccountID = t.AccountID
	e.AccountName = name
	e.At = t.At
	a.enqueue(e)
}

// enqueue applies throttling then drops the event into the channel
// without blocking.
func (a *Alerter) enqueue(e Event) {
	if a.throttledNow(e) {
		return
	}
	select {
	case a.ch <- e:
	default:
		n := a.dropped.Add(1)
		slog.Warn("alert dropped: queue full", "total_dropped", n, "title", e.Title)
	}
}

// throttledNow reports whether an identical alert was sent within the
// throttle window, recording the timestamp when it is allowed through.
func (a *Alerter) throttledNow(e Event) bool {
	key := e.throttleKey()
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if last, ok := a.lastSent[key]; ok && now.Sub(last) < a.throttle {
		return true
	}
	a.lastSent[key] = now
	return false
}

func (a *Alerter) accountName(id int64) string {
	if a.pool != nil {
		if acc := a.pool.ByID(id); acc != nil && acc.Name != "" {
			return acc.Name
		}
	}
	return fmt.Sprintf("id=%d", id)
}

func (a *Alerter) run(ctx context.Context) {
	defer close(a.stopped)
	for {
		select {
		case <-a.stop:
			a.drain(ctx)
			return
		case <-ctx.Done():
			a.drain(context.Background())
			return
		case e := <-a.ch:
			a.deliver(ctx, e)
		}
	}
}

func (a *Alerter) drain(ctx context.Context) {
	for {
		select {
		case e := <-a.ch:
			a.deliver(ctx, e)
		default:
			return
		}
	}
}

// deliver sends one event to every currently-configured notifier. A
// failing notifier is logged but does not block the others.
func (a *Alerter) deliver(ctx context.Context, e Event) {
	for _, n := range a.source() {
		sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
		if err := n.Send(sendCtx, e); err != nil {
			slog.Error("alert delivery failed",
				"notifier", n.Name(), "title", e.Title, "err", err.Error())
		}
		cancel()
	}
}
