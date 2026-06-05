package limits

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

type stubBalance struct {
	mu     sync.Mutex
	values map[int64]float64
	calls  int
}

func (s *stubBalance) Balance(_ context.Context, userID int64) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.values[userID], nil
}

func (s *stubBalance) set(userID int64, v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[userID] = v
}

func (s *stubBalance) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestBalanceGuard_ColdLoadThenCached(t *testing.T) {
	src := &stubBalance{values: map[int64]float64{7: 12.50}}
	g := NewBalanceGuard(src, time.Hour)
	ctx := context.Background()

	if v, _ := g.Balance(ctx, 7); v != 12.50 {
		t.Fatalf("cold balance = %v, want 12.50", v)
	}
	// Within TTL: served from cache, no second query.
	if v, _ := g.Balance(ctx, 7); v != 12.50 {
		t.Fatalf("cached balance = %v, want 12.50", v)
	}
	if n := src.callCount(); n != 1 {
		t.Fatalf("source queries = %d, want 1", n)
	}
}

func TestBalanceGuard_ChargeDebitsInMemory(t *testing.T) {
	src := &stubBalance{values: map[int64]float64{7: 10.00}}
	g := NewBalanceGuard(src, time.Hour)
	ctx := context.Background()

	_, _ = g.Balance(ctx, 7) // seed
	g.Charge(7, 4.00)
	if v, _ := g.Balance(ctx, 7); v != 6.00 {
		t.Fatalf("after charge balance = %v, want 6.00", v)
	}
	g.Credit(7, 5.00)
	if v, _ := g.Balance(ctx, 7); v != 11.00 {
		t.Fatalf("after credit balance = %v, want 11.00", v)
	}
}

func TestBalanceGuard_StaleWhileRevalidate(t *testing.T) {
	src := &stubBalance{values: map[int64]float64{7: 10.00}}
	g := NewBalanceGuard(src, 10*time.Millisecond)
	ctx := context.Background()

	if v, _ := g.Balance(ctx, 7); v != 10.00 {
		t.Fatalf("seed = %v, want 10.00", v)
	}
	src.set(7, 3.00)
	time.Sleep(15 * time.Millisecond)
	// Post-TTL: stale value served immediately, refresh in background.
	if v, _ := g.Balance(ctx, 7); v != 10.00 {
		t.Fatalf("stale read = %v, want 10.00", v)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if v, _ := g.Balance(ctx, 7); v == 3.00 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh did not update balance to 3.00")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCheck_InsufficientBalanceRejects exercises the limiter gate end to
// end: a key owned by a user whose balance is <= 0 is rejected 402.
func TestCheck_InsufficientBalanceRejects(t *testing.T) {
	src := &stubBalance{values: map[int64]float64{42: 0}}
	l := New(nil, nil)
	l.SetBalanceGuard(NewBalanceGuard(src, time.Hour))

	uid := int64(42)
	k := &apikey.Key{Name: "broke", UserID: &uid}
	r := l.Check(context.Background(), k, "m")
	if r == nil || r.Status != 402 || r.Type != "insufficient_balance" {
		t.Fatalf("expected 402 insufficient_balance, got %+v", r)
	}

	// A key with no owner (admin/system key) is never balance-gated.
	if r := l.Check(context.Background(), &apikey.Key{Name: "sys"}, "m"); r != nil {
		t.Fatalf("ownerless key should bypass balance gate, got %+v", r)
	}

	// A funded user passes.
	src.set(42, 5.00)
	g2 := NewBalanceGuard(src, time.Hour)
	l2 := New(nil, nil)
	l2.SetBalanceGuard(g2)
	if r := l2.Check(context.Background(), k, "m"); r != nil {
		t.Fatalf("funded user should pass, got %+v", r)
	}
}
