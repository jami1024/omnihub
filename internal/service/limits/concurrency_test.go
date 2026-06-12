package limits

import (
	"testing"

	"github.com/jami1024/omnihub/internal/service/provider"
)

func TestConcurrencyGuard(t *testing.T) {
	g := NewConcurrencyGuard()
	a := &provider.Account{ID: 1, MaxConcurrency: 2}

	if !g.TryAcquire(1, 2) || !g.TryAcquire(1, 2) {
		t.Fatal("two slots should be available")
	}
	if g.TryAcquire(1, 2) {
		t.Fatal("third acquire must fail at max=2")
	}
	if !g.AtCap(a) {
		t.Fatal("AtCap should report saturation")
	}
	g.Release(1)
	if g.AtCap(a) {
		t.Fatal("released slot should clear AtCap")
	}
	if !g.TryAcquire(1, 2) {
		t.Fatal("slot should be reusable after release")
	}

	// Unlimited accounts always acquire but are still counted.
	free := &provider.Account{ID: 2, MaxConcurrency: 0}
	for i := 0; i < 100; i++ {
		if !g.TryAcquire(2, 0) {
			t.Fatal("unlimited account must always acquire")
		}
	}
	if g.AtCap(free) {
		t.Fatal("unlimited account is never at cap")
	}
	if g.InFlight(2) != 100 {
		t.Fatalf("inflight: %d", g.InFlight(2))
	}

	// Double release clamps at zero.
	g.Release(99)
	if g.InFlight(99) != 0 {
		t.Fatal("release below zero must clamp")
	}
}
