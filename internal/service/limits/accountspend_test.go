package limits

import (
	"context"
	"testing"

	"github.com/jami1024/omnihub/internal/service/provider"
)

type fakeSpend struct {
	daily map[string]float64
	total map[string]float64
}

func (f fakeSpend) SumCostByAccount(_ context.Context, name string) (float64, error) {
	return f.daily[name], nil
}
func (f fakeSpend) TotalCostByAccount(_ context.Context, name string) (float64, error) {
	return f.total[name], nil
}

func f64(v float64) *float64 { return &v }

func TestAccountGuardOverLimit(t *testing.T) {
	src := fakeSpend{
		daily: map[string]float64{"a": 9.0, "b": 2.0, "c": 0},
		total: map[string]float64{"a": 50, "b": 200, "c": 0},
	}
	g := NewAccountGuard(src)

	accounts := []*provider.Account{
		{ID: 1, Name: "a", DailyUSDLimit: f64(10)},               // 9 < 10 → ok
		{ID: 2, Name: "b", DailyUSDLimit: f64(1)},                // 2 >= 1 → over
		{ID: 3, Name: "c"},                                       // no cap → ok
		{ID: 4, Name: "a", TotalUSDLimit: f64(40)},               // 50 >= 40 → over
		{ID: 5, Name: "b", DailyUSDLimit: f64(100), TotalUSDLimit: f64(150)}, // total 200>=150 → over
	}
	g.Refresh(context.Background(), accounts)

	cases := []struct {
		a    *provider.Account
		want bool
	}{
		{accounts[0], false},
		{accounts[1], true},
		{accounts[2], false},
		{accounts[3], true},
		{accounts[4], true},
	}
	for _, tc := range cases {
		if got := g.OverLimit(tc.a); got != tc.want {
			t.Errorf("OverLimit(%s id=%d) = %v, want %v", tc.a.Name, tc.a.ID, got, tc.want)
		}
	}
}

// An unmeasured account (Refresh never ran, or query failed) is allowed.
func TestAccountGuardFailOpen(t *testing.T) {
	g := NewAccountGuard(fakeSpend{daily: map[string]float64{}, total: map[string]float64{}})
	a := &provider.Account{ID: 99, Name: "z", DailyUSDLimit: f64(1)}
	if g.OverLimit(a) {
		t.Error("expected unmeasured account to be allowed (fail-open)")
	}
	// Nil source → never blocks.
	if NewAccountGuard(nil).OverLimit(a) {
		t.Error("nil-source guard must never block")
	}
}
