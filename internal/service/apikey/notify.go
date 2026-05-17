package apikey

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultNotifyChannel is the channel name the trigger installed by
// migration 0008 publishes to.
const DefaultNotifyChannel = "omnihub_api_keys_changed"

// RefreshFunc is the callback invoked on each notification.
type RefreshFunc func(context.Context) error

// Listener subscribes to api_keys NOTIFY events and drives the
// in-memory pool to re-read the table the moment a key is added,
// disabled, or deleted. Mirrors account.Listener line-for-line so
// the operational behaviour (reconnect with backoff, refresh timeout)
// stays consistent across the two domains.
type Listener struct {
	pool    *pgxpool.Pool
	channel string
	refresh RefreshFunc
}

// NewListener builds a listener wired against pool. Empty channel
// falls back to DefaultNotifyChannel.
func NewListener(pool *pgxpool.Pool, channel string, refresh RefreshFunc) *Listener {
	if channel == "" {
		channel = DefaultNotifyChannel
	}
	return &Listener{pool: pool, channel: channel, refresh: refresh}
}

// Start spawns the listener goroutine.
func (l *Listener) Start(ctx context.Context) { go l.run(ctx) }

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
		slog.Error("api_keys notify listener disconnected; reconnecting",
			"err", err.Error(), "next_retry", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (l *Listener) listenOnce(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+l.channel); err != nil {
		return err
	}
	slog.Info("listening for api_keys changes", "channel", l.channel)

	for {
		notif, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		slog.Debug("api_keys notification received",
			"channel", l.channel, "op", notif.Payload)
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := l.refresh(refreshCtx); err != nil {
			slog.Error("api_keys pool refresh after notification failed",
				"err", err.Error())
		}
		cancel()
	}
}
