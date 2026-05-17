// Package healthlog bridges in-process circuit-breaker transitions
// to the account_health_events table. It exists so operators can
// query account flapping history with plain SQL ("how many times
// did account X trip last week?") without standing up Prometheus
// or another metrics backend.
//
// Volume is low (a healthy account emits a handful of transitions
// per day; even a thrashing one tops out at a few per minute). A
// single background goroutine with a bounded channel keeps the
// hot-path emission free of DB latency, and excess events are
// dropped with a warning rather than allowed to back-pressure the
// breaker itself.
package healthlog

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/health"
)

const (
	// queueSize bounds in-flight transitions. Big enough that a
	// brief storm (~100 transitions in a few seconds during a
	// pathological flap) is absorbed without drops; small enough
	// that an extended DB outage doesn't leak goroutines.
	queueSize = 256

	// insertTimeout caps how long one transition write can take
	// before we move on. Five seconds is generous for an INSERT
	// of a single row; anything slower implies a sick DB and we
	// would rather drop further events than queue them.
	insertTimeout = 5 * time.Second
)

// Recorder persists health.Transition events asynchronously.
//
// Wiring is two-step at startup:
//
//	rec := healthlog.New(repo, accountPool)
//	rec.Start(ctx)
//	tracker.SetTransitionHandler(rec.Handler)
//
// Stop drains the channel synchronously so shutdown does not lose
// the trailing few events.
type Recorder struct {
	repo *repository.AccountHealthEventRepo
	pool *account.Pool

	ch      chan health.Transition
	stop    chan struct{}
	stopped chan struct{}

	dropped atomic.Int64
}

// New constructs a Recorder. repo and pool must be non-nil; without
// the pool we cannot resolve account names for the persisted rows.
func New(repo *repository.AccountHealthEventRepo, pool *account.Pool) *Recorder {
	return &Recorder{
		repo:    repo,
		pool:    pool,
		ch:      make(chan health.Transition, queueSize),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start spawns the writer goroutine. Returns immediately.
func (r *Recorder) Start(ctx context.Context) {
	go r.run(ctx)
}

// Stop signals the writer to exit and waits for it to drain any
// pending events. Safe to call multiple times.
func (r *Recorder) Stop() {
	select {
	case <-r.stop:
		// Already closed.
	default:
		close(r.stop)
	}
	<-r.stopped
}

// Dropped returns the cumulative number of transitions that were
// dropped because the channel was full. A non-zero value means
// either the DB is slower than the breaker is flapping, or the
// queue size is undersized for the deployment.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Handler is the health.TransitionHandler hook. Non-blocking — if
// the channel is full the event is dropped and counted; the
// caller (tracker, holding no locks) returns immediately.
func (r *Recorder) Handler(t health.Transition) {
	select {
	case r.ch <- t:
	default:
		n := r.dropped.Add(1)
		slog.Warn("health event dropped: recorder queue full",
			"total_dropped", n,
			"account_id", t.AccountID,
			"from", t.From,
			"to", t.To,
		)
	}
}

func (r *Recorder) run(ctx context.Context) {
	defer close(r.stopped)
	for {
		select {
		case <-r.stop:
			r.drain(ctx)
			return
		case <-ctx.Done():
			r.drain(context.Background())
			return
		case t := <-r.ch:
			r.write(ctx, t)
		}
	}
}

// drain writes any events still in the channel before the goroutine
// exits. Uses the provided ctx so a Stop during cancellation does
// not block indefinitely on a sick DB.
func (r *Recorder) drain(ctx context.Context) {
	for {
		select {
		case t := <-r.ch:
			r.write(ctx, t)
		default:
			return
		}
	}
}

func (r *Recorder) write(ctx context.Context, t health.Transition) {
	name := ""
	if a := r.pool.ByID(t.AccountID); a != nil {
		name = a.Name
	}
	var reason *string
	if t.Reason != nil {
		s := t.Reason.Error()
		reason = &s
	}

	writeCtx, cancel := context.WithTimeout(ctx, insertTimeout)
	defer cancel()

	err := r.repo.Insert(writeCtx, repository.AccountHealthEvent{
		CreatedAt:    t.At,
		AccountID:    t.AccountID,
		AccountName:  name,
		FromState:    string(t.From),
		ToState:      string(t.To),
		FailureCount: t.FailureCount,
		Reason:       reason,
	})
	if err != nil {
		slog.Error("health event insert failed",
			"account_id", t.AccountID,
			"from", t.From,
			"to", t.To,
			"err", err.Error(),
		)
	}
}
