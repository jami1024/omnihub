package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
)

type fakeUsageStore struct {
	totals    repository.UsageTotals
	daily     []repository.DailyUsage
	byModel   []repository.ModelUsage
	lastSince time.Time
}

func (f *fakeUsageStore) SumUsageSince(_ context.Context, since time.Time) (repository.UsageTotals, error) {
	f.lastSince = since
	return f.totals, nil
}
func (f *fakeUsageStore) DailyUsageSince(_ context.Context, _ time.Time) ([]repository.DailyUsage, error) {
	return f.daily, nil
}
func (f *fakeUsageStore) UsageByModelSince(_ context.Context, _ time.Time) ([]repository.ModelUsage, error) {
	return f.byModel, nil
}

func newUsageEngine(store *fakeUsageStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/usage", handler.UsageHandler(store))
	return r
}

func TestUsageDefaultWindowAndGapFill(t *testing.T) {
	store := &fakeUsageStore{
		totals:  repository.UsageTotals{Requests: 10, CostUSD: 1.5},
		daily:   nil, // no traffic days at all
		byModel: nil,
	}
	rec := do(newUsageEngine(store), http.MethodGet, "/admin/api/usage", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		WindowDays int `json:"window_days"`
		Summary    struct {
			Requests int64   `json:"requests"`
			CostUSD  float64 `json:"cost_usd"`
		} `json:"summary"`
		Daily   []struct{ Day time.Time } `json:"daily"`
		ByModel []any                     `json:"by_model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.WindowDays != 7 {
		t.Errorf("window_days = %d, want default 7", resp.WindowDays)
	}
	// Gap fill must produce exactly 7 daily buckets even with no rows.
	if len(resp.Daily) != 7 {
		t.Errorf("daily buckets = %d, want 7 (gaps filled)", len(resp.Daily))
	}
	if resp.Summary.Requests != 10 || resp.Summary.CostUSD != 1.5 {
		t.Errorf("summary not forwarded: %+v", resp.Summary)
	}
	// by_model must serialize as [] not null.
	if resp.ByModel == nil {
		t.Error("by_model should be [] not null")
	}
}

func TestUsageClampsDays(t *testing.T) {
	cases := map[string]int{
		"days=1":   1,
		"days=90":  90,
		"days=999": 90, // clamped to max
		"days=0":   7,  // invalid → default
		"days=abc": 7,  // malformed → default
	}
	for query, want := range cases {
		t.Run(query, func(t *testing.T) {
			store := &fakeUsageStore{}
			rec := do(newUsageEngine(store), http.MethodGet, "/admin/api/usage?"+query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			var resp struct {
				WindowDays int       `json:"window_days"`
				Daily      []anyDate `json:"daily"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.WindowDays != want {
				t.Errorf("window_days = %d, want %d", resp.WindowDays, want)
			}
			if len(resp.Daily) != want {
				t.Errorf("daily buckets = %d, want %d", len(resp.Daily), want)
			}
		})
	}
}

type anyDate struct {
	Day time.Time `json:"day"`
}
