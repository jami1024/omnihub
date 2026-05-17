package apikey

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Source returns the current set of enabled keys. Mirrors the
// account.Source pattern so the pool can be unit-tested without a
// real Postgres dependency.
type Source interface {
	ListEnabled(ctx context.Context) ([]*Key, error)
}

// Pool is the in-memory cache of enabled virtual API keys, indexed
// by hash for O(1) auth-time lookup.
type Pool struct {
	source Source

	mu     sync.RWMutex
	byHash map[string]*Key
	byID   map[int64]*Key
}

// NewPool returns an empty pool backed by source. Call Refresh once
// before serving traffic; Start spawns a background refresher.
func NewPool(source Source) *Pool {
	return &Pool{
		source: source,
		byHash: make(map[string]*Key),
		byID:   make(map[int64]*Key),
	}
}

// Refresh re-reads the source and atomically swaps the indices. A
// refresh failure leaves the previous view in place so a transient DB
// blip cannot lock everyone out.
func (p *Pool) Refresh(ctx context.Context) error {
	keys, err := p.source.ListEnabled(ctx)
	if err != nil {
		return err
	}

	byHash := make(map[string]*Key, len(keys))
	byID := make(map[int64]*Key, len(keys))
	for _, k := range keys {
		byHash[k.Hash] = k
		byID[k.ID] = k
	}

	p.mu.Lock()
	p.byHash = byHash
	p.byID = byID
	p.mu.Unlock()
	return nil
}

// Start runs Refresh once synchronously, then re-refreshes every
// interval until ctx is cancelled. Returns the error from the
// initial refresh; subsequent failures are logged but do not abort.
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
					slog.Error("api_keys pool refresh failed", "err", err.Error())
				}
			}
		}
	}()
	return nil
}

// LookupByHash returns the key whose stored hash matches h, or nil
// if no such key exists in the current snapshot.
func (p *Pool) LookupByHash(h string) *Key {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byHash[h]
}

// LookupByID is the O(1) accessor for future Limits Guard work.
func (p *Pool) LookupByID(id int64) *Key {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byID[id]
}

// Size returns the number of enabled keys.
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.byHash)
}
