package authguard

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// fakeParker records park/restore calls.
type fakeParker struct {
	mu       sync.Mutex
	parked   map[int64]string
	restored map[int64]bool
}

func newFakeParker() *fakeParker {
	return &fakeParker{parked: map[int64]string{}, restored: map[int64]bool{}}
}

func (p *fakeParker) Park(_ context.Context, id int64, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.parked[id] = reason
	return nil
}

func (p *fakeParker) Restore(_ context.Context, id int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restored[id] = true
	delete(p.parked, id)
	return nil
}

func (p *fakeParker) isParked(id int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.parked[id]
	return ok
}

// fakeTester returns a fixed verdict.
type fakeTester struct{ healthy bool }

func (t fakeTester) Test(context.Context, *provider.Account) bool { return t.healthy }

func apiKeyAccount(id int64) *provider.Account {
	return &provider.Account{ID: id, Name: "k", Provider: "anthropic", AuthType: "api_key"}
}

func TestParksAfterThreshold(t *testing.T) {
	p := newFakeParker()
	g := New(3, p)
	a := apiKeyAccount(1)
	ctx := context.Background()

	g.Record(ctx, a, 401)
	g.Record(ctx, a, 401)
	if p.isParked(1) {
		t.Fatal("must not park before threshold")
	}
	g.Record(ctx, a, 401) // 3rd → trips
	if !p.isParked(1) {
		t.Fatal("must park at threshold")
	}
}

func TestSuccessResetsStreak(t *testing.T) {
	p := newFakeParker()
	g := New(3, p)
	a := apiKeyAccount(1)
	ctx := context.Background()

	g.Record(ctx, a, 401)
	g.Record(ctx, a, 401)
	g.Record(ctx, a, 200) // resets
	g.Record(ctx, a, 401)
	g.Record(ctx, a, 401)
	if p.isParked(1) {
		t.Fatal("a success in between must reset the streak, preventing a park")
	}
}

func TestOAuthAccountsSkipped(t *testing.T) {
	p := newFakeParker()
	g := New(2, p)
	oauth := &provider.Account{ID: 9, Provider: "openai-codex", AuthType: "imported_oauth"}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		g.Record(ctx, oauth, 401)
	}
	if p.isParked(9) {
		t.Fatal("oauth accounts are the TokenManager's job; authguard must skip them")
	}
}

func TestOnly401And403Count(t *testing.T) {
	p := newFakeParker()
	g := New(2, p)
	a := apiKeyAccount(1)
	ctx := context.Background()
	// 500/429 must not count as auth failures.
	g.Record(ctx, a, 500)
	g.Record(ctx, a, 429)
	g.Record(ctx, a, 403) // 1st auth failure
	if p.isParked(1) {
		t.Fatal("non-auth statuses must not contribute to the streak")
	}
	g.Record(ctx, a, 401) // 2nd → trips
	if !p.isParked(1) {
		t.Fatal("403 then 401 should reach the threshold of 2")
	}
}

func TestRecoveryRestoresHealthyAccount(t *testing.T) {
	p := newFakeParker()
	g := New(1, p)
	a := apiKeyAccount(1)
	g.Record(context.Background(), a, 401) // park immediately (threshold 1)
	if !p.isParked(1) {
		t.Fatal("precondition: parked")
	}

	// A healthy probe restores it.
	g.sweep(context.Background(), []*provider.Account{a}, fakeTester{healthy: true})
	p.mu.Lock()
	restored := p.restored[1]
	p.mu.Unlock()
	if !restored {
		t.Fatal("a healthy probe must restore the account")
	}
	// And the guard forgets it (so it can be parked again later).
	g.mu.Lock()
	_, stillTripped := g.tripped[1]
	g.mu.Unlock()
	if stillTripped {
		t.Fatal("restored account must be cleared from tripped set")
	}
}

func TestRecoveryKeepsUnhealthyParked(t *testing.T) {
	p := newFakeParker()
	g := New(1, p)
	a := apiKeyAccount(1)
	g.Record(context.Background(), a, 401)

	g.sweep(context.Background(), []*provider.Account{a}, fakeTester{healthy: false})
	p.mu.Lock()
	restored := p.restored[1]
	p.mu.Unlock()
	if restored {
		t.Fatal("a still-failing probe must NOT restore the account")
	}
}

func TestRecoveryForgetsVanishedAccount(t *testing.T) {
	p := newFakeParker()
	g := New(1, p)
	g.Record(context.Background(), apiKeyAccount(1), 401)
	// Account no longer in the pool snapshot → forgotten, not restored.
	g.sweep(context.Background(), nil, fakeTester{healthy: true})
	g.mu.Lock()
	_, tripped := g.tripped[1]
	g.mu.Unlock()
	if tripped {
		t.Fatal("an account that left the pool must be dropped from tripped")
	}
}

func TestStartRecoveryRuns(t *testing.T) {
	p := newFakeParker()
	g := New(1, p)
	a := apiKeyAccount(1)
	g.Record(context.Background(), a, 401)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.StartRecovery(ctx, func() []*provider.Account { return []*provider.Account{a} },
		fakeTester{healthy: true}, 50*time.Millisecond)

	deadline := time.After(2 * time.Second)
	for {
		p.mu.Lock()
		ok := p.restored[1]
		p.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("recovery sweeper did not restore within the deadline")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
