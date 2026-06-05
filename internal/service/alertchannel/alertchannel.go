// Package alertchannel holds the in-memory pool of operator alert
// delivery channels, refreshed from the alert_channels table on a NOTIFY
// trigger (migration 0026). The gateway's Alerter reads the enabled
// channels from this pool at delivery time, so adding or disabling a
// channel in the admin UI takes effect without a restart.
package alertchannel

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Channel is one enabled delivery channel with its decrypted URL.
type Channel struct {
	ID      int64
	Type    string // webhook | feishu | dingtalk
	Name    string
	URL     string // decrypted
	Enabled bool
}

// Source is the read-only port the pool needs. Mirrors the blockedip /
// account Source pattern so tests can swap a stub in without Postgres.
type Source interface {
	ListEnabled(ctx context.Context) ([]Channel, error)
}

// Pool is the in-memory set of enabled alert channels.
type Pool struct {
	source Source

	mu       sync.RWMutex
	channels []Channel
}

// NewPool returns an empty pool. Call Refresh once or Start to populate it.
func NewPool(source Source) *Pool { return &Pool{source: source} }

// Refresh re-reads the source and atomically swaps the channel set. A
// failure leaves the previous view in place.
func (p *Pool) Refresh(ctx context.Context) error {
	chs, err := p.source.ListEnabled(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.channels = chs
	p.mu.Unlock()
	return nil
}

// Start runs Refresh once synchronously then re-refreshes on the given
// interval until ctx is cancelled. The initial error is returned;
// subsequent failures are logged only.
func (p *Pool) Start(ctx context.Context, interval time.Duration) error {
	if err := p.Refresh(ctx); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := p.Refresh(ctx); err != nil {
					slog.Error("alert_channels pool refresh failed", "err", err.Error())
				}
			}
		}
	}()
	return nil
}

// Enabled returns a copy of the current enabled channels.
func (p *Pool) Enabled() []Channel {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Channel, len(p.channels))
	copy(out, p.channels)
	return out
}

// Size reports the current enabled-channel count (diagnostic).
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.channels)
}
