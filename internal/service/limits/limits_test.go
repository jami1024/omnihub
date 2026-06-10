package limits

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

type stubSource struct {
	mu     sync.Mutex
	calls  int
	values map[string]float64
	err    error
}

func (s *stubSource) SumCostByKey(_ context.Context, name string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return 0, s.err
	}
	return s.values[name], nil
}

// set updates a stubbed value under lock — the cache may read it from a
// background refresh goroutine concurrently with the test mutating it.
func (s *stubSource) set(name string, v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[name] = v
}

// callCount returns how many times the source was queried, under lock.
func (s *stubSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func keyWith(name string, dailyUSD float64, models []string) *apikey.Key {
	k := &apikey.Key{Name: name, AllowedModels: models}
	if dailyUSD > 0 {
		v := dailyUSD
		k.DailyUSDLimit = &v
	}
	return k
}

func TestCheck_AllowsWhenUnderCap(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 4.99}}
	l := New(NewSpendCache(src, time.Minute), nil)

	if r := l.Check(context.Background(), keyWith("alice", 5.00, nil), "claude-opus-4-7"); r != nil {
		t.Fatalf("expected pass, got reject: %+v", r)
	}
}

func TestCheck_RejectsAtCap(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 5.00}}
	l := New(NewSpendCache(src, time.Minute), nil)

	r := l.Check(context.Background(), keyWith("alice", 5.00, nil), "claude-opus-4-7")
	if r == nil {
		t.Fatal("expected reject at cap, got pass")
	}
	if r.Status != 429 {
		t.Fatalf("status = %d, want 429", r.Status)
	}
	if r.Type != "daily_limit_exceeded" {
		t.Fatalf("type = %q, want daily_limit_exceeded", r.Type)
	}
}

func TestCheck_RejectsModelNotInAllowList(t *testing.T) {
	l := New(nil, nil) // no cache needed; model check is sync
	k := keyWith("alice", 0, []string{"claude-haiku-4-5"})

	r := l.Check(context.Background(), k, "claude-opus-4-7")
	if r == nil {
		t.Fatal("expected reject, got pass")
	}
	if r.Status != 403 {
		t.Fatalf("status = %d, want 403", r.Status)
	}
	if r.Type != "model_not_allowed" {
		t.Fatalf("type = %q, want model_not_allowed", r.Type)
	}
}

func TestCheck_EmptyAllowListAllowsEveryModel(t *testing.T) {
	l := New(nil, nil)
	if r := l.Check(context.Background(), keyWith("alice", 0, nil), "anything-goes"); r != nil {
		t.Fatalf("empty allow-list should pass, got reject: %+v", r)
	}
}

func TestCheck_FailOpenOnDBError(t *testing.T) {
	src := &stubSource{err: errors.New("db down")}
	l := New(NewSpendCache(src, time.Minute), nil)
	k := keyWith("alice", 5.00, nil)

	if r := l.Check(context.Background(), k, "claude-opus-4-7"); r != nil {
		t.Fatalf("expected fail-open pass on DB error, got reject: %+v", r)
	}
}

func TestCheck_NilKeyBypasses(t *testing.T) {
	l := New(nil, nil)
	if r := l.Check(context.Background(), nil, "anything"); r != nil {
		t.Fatalf("nil key should bypass, got reject: %+v", r)
	}
}

func TestRecordSpend_FoldsIntoCachedTotal(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 1.00}}
	cache := NewSpendCache(src, time.Hour)
	l := New(cache, nil)
	ctx := context.Background()

	// First Check seeds cache with $1.00 from the source.
	if r := l.Check(ctx, keyWith("alice", 5.00, nil), "m"); r != nil {
		t.Fatalf("first check: %+v", r)
	}
	// Simulate a completed request worth $4.50; should push us past the
	// $5.00 cap WITHOUT another DB query.
	l.RecordSpend(keyWith("alice", 5.00, nil), 4.50)

	if r := l.Check(ctx, keyWith("alice", 5.00, nil), "m"); r == nil {
		t.Fatal("expected reject after RecordSpend pushed over cap")
	}
	if n := src.callCount(); n != 1 {
		t.Fatalf("source queries = %d, want 1 (in-memory increment should not re-query)", n)
	}
}

func TestSpendCache_RefreshesAfterTTL(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 1.00}}
	cache := NewSpendCache(src, 10*time.Millisecond)
	ctx := context.Background()

	if v, _ := cache.Spend(ctx, "alice"); v != 1.00 {
		t.Fatalf("first read = %.2f, want 1.00", v)
	}
	src.set("alice", 2.50)
	// Within TTL: cached value still returned.
	if v, _ := cache.Spend(ctx, "alice"); v != 1.00 {
		t.Fatalf("cached read = %.2f, want 1.00", v)
	}
	time.Sleep(15 * time.Millisecond)
	// Post-TTL: stale-while-revalidate serves the OLD value immediately
	// (never blocks on the source) and kicks off a background refresh.
	if v, _ := cache.Spend(ctx, "alice"); v != 1.00 {
		t.Fatalf("stale read = %.2f, want 1.00 (served while revalidating)", v)
	}
	// The background refresh lands shortly; a later read sees 2.50.
	deadline := time.Now().Add(time.Second)
	for {
		if v, _ := cache.Spend(ctx, "alice"); v == 2.50 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh did not update the cache to 2.50")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestCheck_RPMRejectsAfterBurstExhausted(t *testing.T) {
	rpm := NewRPMCache()
	l := New(nil, rpm)
	r := 3
	k := &apikey.Key{Name: "alice", RPMLimit: &r}

	for i := 0; i < r; i++ {
		if rej := l.Check(context.Background(), k, "m"); rej != nil {
			t.Fatalf("call %d unexpectedly rejected: %+v", i, rej)
		}
	}
	// Bucket is now empty; the next call must be 429 rate_limit_exceeded.
	rej := l.Check(context.Background(), k, "m")
	if rej == nil {
		t.Fatal("expected rate limit reject")
	}
	if rej.Status != 429 || rej.Type != "rate_limit_exceeded" {
		t.Fatalf("got %d / %q, want 429 / rate_limit_exceeded", rej.Status, rej.Type)
	}
}

func TestCheck_RPMNilLimitBypasses(t *testing.T) {
	l := New(nil, NewRPMCache())
	k := &apikey.Key{Name: "alice"} // RPMLimit nil
	for i := 0; i < 100; i++ {
		if rej := l.Check(context.Background(), k, "m"); rej != nil {
			t.Fatalf("call %d rejected with nil RPM: %+v", i, rej)
		}
	}
}

func TestCheck_RPMBucketRebuildsOnRateChange(t *testing.T) {
	rpm := NewRPMCache()
	l := New(nil, rpm)
	r := 2
	k := &apikey.Key{Name: "alice", RPMLimit: &r}

	// Exhaust the rpm=2 bucket.
	_ = l.Check(context.Background(), k, "m")
	_ = l.Check(context.Background(), k, "m")
	if rej := l.Check(context.Background(), k, "m"); rej == nil {
		t.Fatal("expected rate limit reject before rate bump")
	}

	// Operator bumps the limit to 10/min; next Check rebuilds the
	// bucket with the new rate and the request goes through.
	r2 := 10
	k.RPMLimit = &r2
	if rej := l.Check(context.Background(), k, "m"); rej != nil {
		t.Fatalf("expected pass after rate bump, got reject: %+v", rej)
	}
}

func TestSpendCache_AddIsNoOpForAbsentKey(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 0}}
	cache := NewSpendCache(src, time.Minute)
	cache.Add("alice", 10.00) // no prior Spend(), so this must NOT seed

	got, _ := cache.Spend(context.Background(), "alice")
	if got != 0 {
		t.Fatalf("Add without prior Spend leaked %.2f into the cache", got)
	}
}

func TestRecordBillingSpendRoutesPlanAndWalletPortions(t *testing.T) {
	src := &stubBalance{values: map[balanceKey]float64{
		{7, apikey.ModePayg}: 10,
		{7, apikey.ModePlan}: 10,
	}}
	guard := NewBalanceGuard(src, time.Hour)
	l := New(nil, nil)
	l.SetBalanceGuard(guard)
	uid := int64(7)
	k := &apikey.Key{Name: "alice", UserID: &uid, BillingMode: apikey.ModePlan}
	ctx := context.Background()
	if _, err := guard.Balance(ctx, 7, apikey.ModePayg); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Balance(ctx, 7, apikey.ModePlan); err != nil {
		t.Fatal(err)
	}
	// plan=3 (plan entry only), wallet=2 (both entries).
	l.RecordBillingSpend(k, 3, 2)
	if got, _ := guard.Balance(ctx, 7, apikey.ModePayg); got != 8 {
		t.Fatalf("payg balance = %.2f, want 8.00 (wallet -2)", got)
	}
	if got, _ := guard.Balance(ctx, 7, apikey.ModePlan); got != 5 {
		t.Fatalf("plan balance = %.2f, want 5.00 (plan -3, wallet -2)", got)
	}
}
