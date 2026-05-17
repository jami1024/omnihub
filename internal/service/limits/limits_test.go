package limits

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

type stubSource struct {
	calls  int
	values map[string]float64
	err    error
}

func (s *stubSource) SumCostByKey(_ context.Context, name string) (float64, error) {
	s.calls++
	if s.err != nil {
		return 0, s.err
	}
	return s.values[name], nil
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
	l := New(NewSpendCache(src, time.Minute))

	if r := l.Check(context.Background(), keyWith("alice", 5.00, nil), "claude-opus-4-7"); r != nil {
		t.Fatalf("expected pass, got reject: %+v", r)
	}
}

func TestCheck_RejectsAtCap(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 5.00}}
	l := New(NewSpendCache(src, time.Minute))

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
	l := New(nil) // no cache needed; model check is sync
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
	l := New(nil)
	if r := l.Check(context.Background(), keyWith("alice", 0, nil), "anything-goes"); r != nil {
		t.Fatalf("empty allow-list should pass, got reject: %+v", r)
	}
}

func TestCheck_FailOpenOnDBError(t *testing.T) {
	src := &stubSource{err: errors.New("db down")}
	l := New(NewSpendCache(src, time.Minute))
	k := keyWith("alice", 5.00, nil)

	if r := l.Check(context.Background(), k, "claude-opus-4-7"); r != nil {
		t.Fatalf("expected fail-open pass on DB error, got reject: %+v", r)
	}
}

func TestCheck_NilKeyBypasses(t *testing.T) {
	l := New(nil)
	if r := l.Check(context.Background(), nil, "anything"); r != nil {
		t.Fatalf("nil key should bypass, got reject: %+v", r)
	}
}

func TestRecordSpend_FoldsIntoCachedTotal(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 1.00}}
	cache := NewSpendCache(src, time.Hour)
	l := New(cache)
	ctx := context.Background()

	// First Check seeds cache with $1.00 from the source.
	if r := l.Check(ctx, keyWith("alice", 5.00, nil), "m"); r != nil {
		t.Fatalf("first check: %+v", r)
	}
	// Simulate a completed request worth $4.50; should push us past the
	// $5.00 cap WITHOUT another DB query.
	l.RecordSpend("alice", 4.50)

	if r := l.Check(ctx, keyWith("alice", 5.00, nil), "m"); r == nil {
		t.Fatal("expected reject after RecordSpend pushed over cap")
	}
	if src.calls != 1 {
		t.Fatalf("source queries = %d, want 1 (in-memory increment should not re-query)", src.calls)
	}
}

func TestSpendCache_RefreshesAfterTTL(t *testing.T) {
	src := &stubSource{values: map[string]float64{"alice": 1.00}}
	cache := NewSpendCache(src, 10*time.Millisecond)
	ctx := context.Background()

	if v, _ := cache.Spend(ctx, "alice"); v != 1.00 {
		t.Fatalf("first read = %.2f, want 1.00", v)
	}
	src.values["alice"] = 2.50
	// Within TTL: stale value still returned.
	if v, _ := cache.Spend(ctx, "alice"); v != 1.00 {
		t.Fatalf("cached read = %.2f, want 1.00", v)
	}
	time.Sleep(15 * time.Millisecond)
	if v, _ := cache.Spend(ctx, "alice"); v != 2.50 {
		t.Fatalf("post-TTL read = %.2f, want 2.50", v)
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
