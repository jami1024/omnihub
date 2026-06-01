package pricing

import (
	"sync"

	"github.com/jami1024/omnihub/internal/service/usage"
)

// Calculator is the read port the gateway handlers depend on for
// pricing. Both the static Table and the hot-reloadable Pool satisfy
// it, so a handler written against the interface works whether prices
// come from the built-in defaults or the DB-backed pool.
type Calculator interface {
	Calculate(model string, u usage.Usage) (Breakdown, bool)
}

// Pool holds the live price Table behind an RWMutex so the LISTEN/NOTIFY
// refresher can swap in a new table (built-in defaults overlaid with the
// model_prices rows) without racing the request hot path. The table is
// replaced wholesale, never mutated in place, so a reader that grabs the
// current map under the read lock can safely use it after unlocking.
type Pool struct {
	mu  sync.RWMutex
	tbl Table
}

// NewPool seeds the pool with an initial table (typically Default()).
func NewPool(initial Table) *Pool {
	if initial == nil {
		initial = Table{}
	}
	return &Pool{tbl: initial}
}

// Calculate delegates to the current table.
func (p *Pool) Calculate(model string, u usage.Usage) (Breakdown, bool) {
	p.mu.RLock()
	t := p.tbl
	p.mu.RUnlock()
	return t.Calculate(model, u)
}

// Replace swaps the live table. A nil table is treated as empty.
func (p *Pool) Replace(t Table) {
	if t == nil {
		t = Table{}
	}
	p.mu.Lock()
	p.tbl = t
	p.mu.Unlock()
}

// Size reports how many price rows are currently live (for startup logs).
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.tbl)
}

// Overlay returns a new Table that is `base` with every entry from
// `over` layered on top (over wins on key collisions). Neither input is
// mutated. Used to build the effective table from built-in defaults
// (base) plus the DB rows (over).
func Overlay(base, over Table) Table {
	out := make(Table, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}
