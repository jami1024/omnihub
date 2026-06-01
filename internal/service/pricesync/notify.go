package pricesync

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultNotifyChannel is the channel the trigger from migration 0013
// publishes to.
const DefaultNotifyChannel = "omnihub_model_prices_changed"

// RefreshFunc is the callback invoked on each notification.
type RefreshFunc func(context.Context) error

// Listener subscribes to model_prices NOTIFY events and re-overlays the
// price pool the moment a row changes. Mirrors blockedip.Listener so the
// reconnect/backoff behaviour stays consistent across pools.
type Listener struct {
	pool    *pgxpool.Pool
	channel string
	refresh RefreshFunc
}

// NewListener builds a listener. Empty channel falls back to the default.
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
		slog.Error("model_prices notify listener disconnected; reconnecting",
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
	slog.Info("listening for model_prices changes", "channel", l.channel)

	for {
		notif, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := l.refresh(refreshCtx); err != nil {
			slog.Error("model_prices pool refresh after notification failed", "err", err.Error())
		} else {
			slog.Info("model_prices pool refreshed", "trigger", "notification", "op", notif.Payload)
		}
		cancel()
	}
}
