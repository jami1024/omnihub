package account

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultNotifyChannel is the PostgreSQL channel name the trigger
// installed by migration 0006 publishes to.
const DefaultNotifyChannel = "omnihub_accounts_changed"

// RefreshFunc is the callback Listener invokes on every notification.
// In production this is Pool.Refresh; the indirection lets us unit-
// test the listener without a real account pool.
type RefreshFunc func(context.Context) error

// Listener subscribes to a PostgreSQL NOTIFY channel and triggers a
// refresh callback on every event. The listener holds one connection
// from the pool for the LISTEN session; reconnection is automatic
// with exponential backoff on transport errors.
//
// The listener is a complement to, not a replacement for, the
// periodic Pool.Refresh ticker: a dropped notification (during
// reconnect, or in deployments where the trigger has not been
// installed) is still picked up by the next tick.
type Listener struct {
	pool    *pgxpool.Pool
	channel string
	refresh RefreshFunc
}

// NewListener builds a listener wired against the given pool and
// channel. An empty channel falls back to DefaultNotifyChannel.
func NewListener(pool *pgxpool.Pool, channel string, refresh RefreshFunc) *Listener {
	if channel == "" {
		channel = DefaultNotifyChannel
	}
	return &Listener{pool: pool, channel: channel, refresh: refresh}
}

// Start spawns the listener goroutine. It returns immediately. The
// loop exits when ctx is cancelled; transient errors are logged and
// recovered from with backoff.
func (l *Listener) Start(ctx context.Context) {
	go l.run(ctx)
}

func (l *Listener) run(ctx context.Context) {
	const (
		minBackoff = 1 * time.Second
		maxBackoff = 30 * time.Second
	)
	backoff := minBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		err := l.listenOnce(ctx)
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		slog.Error("account notify listener disconnected; reconnecting",
			"err", err.Error(),
			"next_retry", backoff,
		)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		// Exponential backoff bounded at maxBackoff. Reset on success.
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// listenOnce acquires a dedicated connection, issues LISTEN, then
// blocks on WaitForNotification in a loop. Returns when the
// connection breaks or ctx is cancelled.
func (l *Listener) listenOnce(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Channel name is a compile-time constant in OmniHub; defence
	// against SQL injection is unnecessary here. If a future commit
	// makes the channel configurable, validate before interpolation.
	if _, err := conn.Exec(ctx, "LISTEN "+l.channel); err != nil {
		return err
	}
	slog.Info("listening for account changes", "channel", l.channel)

	for {
		notif, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		slog.Debug("account notification received",
			"channel", l.channel,
			"op", notif.Payload,
		)
		l.invokeRefresh(ctx)
	}
}

func (l *Listener) invokeRefresh(parent context.Context) {
	// The refresh runs synchronously inside the LISTEN loop. If the
	// callback hangs the listener stalls; bound it with a timeout so
	// a slow / dead DB cannot deadlock the goroutine.
	refreshCtx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	if err := l.refresh(refreshCtx); err != nil {
		slog.Error("account pool refresh after notification failed",
			"err", err.Error(),
		)
	}
}
