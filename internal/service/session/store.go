package session

import (
	"context"
	"sync"
	"time"
)

// DefaultTTL is the default lifetime of a sticky binding. 5 minutes
// matches the default Anthropic prompt-cache TTL so a binding
// outlives at least one cache window.
const DefaultTTL = 5 * time.Minute

// Store maps session keys to upstream account IDs with TTL eviction.
// Bindings are kept in memory only; a future commit will move them
// to Redis for multi-instance sharing.
//
// The store is safe for concurrent use.
type Store struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	accountID int64
	expiresAt time.Time
}

// New returns a Store. ttl <= 0 falls back to DefaultTTL.
func New(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Store{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]entry),
	}
}

// Get returns the account id previously bound to key, or (0, false)
// if no binding exists or the binding has expired. An expired
// binding is dropped opportunistically.
func (s *Store) Get(key string) (int64, bool) {
	if key == "" {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return 0, false
	}
	if s.now().After(e.expiresAt) {
		delete(s.entries, key)
		return 0, false
	}
	return e.accountID, true
}

// Bind associates key with accountID for the configured TTL.
// Subsequent Get calls within the TTL return the same accountID.
// Calling Bind with the same key extends the binding.
func (s *Store) Bind(key string, accountID int64) {
	if key == "" || accountID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = entry{
		accountID: accountID,
		expiresAt: s.now().Add(s.ttl),
	}
}

// Drop removes any binding for key. Idempotent.
func (s *Store) Drop(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}

// Size reports the number of live bindings (including any expired
// entries that have not yet been swept). Useful for /readyz and
// observability.
func (s *Store) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Start spawns a goroutine that periodically sweeps expired entries
// until ctx is cancelled. Without sweeping the map grows in
// proportion to the request rate during outages; sweeping bounds
// memory to roughly TTL × request rate.
func (s *Store) Start(ctx context.Context, sweepInterval time.Duration) {
	if sweepInterval <= 0 {
		sweepInterval = s.ttl
	}
	go func() {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweep()
			}
		}
	}()
}

func (s *Store) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, e := range s.entries {
		if now.After(e.expiresAt) {
			delete(s.entries, k)
		}
	}
}
