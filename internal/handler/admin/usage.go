package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// usageStore is the slice of repository.MessageRequestRepo the usage
// dashboard depends on, narrowed for testability.
type usageStore interface {
	SumUsageSince(ctx context.Context, since time.Time) (repository.UsageTotals, error)
	DailyUsageSince(ctx context.Context, since time.Time) ([]repository.DailyUsage, error)
	UsageByModelSince(ctx context.Context, since time.Time) ([]repository.ModelUsage, error)
}

const (
	defaultUsageDays = 7
	maxUsageDays     = 90
)

// nowFunc is overridable in tests; production uses the wall clock.
var nowFunc = time.Now

// UsageHandler returns GET /admin/api/usage?days=N — the dashboard's
// single fetch: headline totals, a daily time series, and a per-model
// breakdown for the trailing N days (UTC, default 7, capped at 90).
func UsageHandler(store usageStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		days := parseDays(c.Query("days"))
		// Anchor the window at the start of the UTC day N-1 days ago so
		// the series always spans whole calendar days (matching the
		// date_trunc buckets), rather than a ragged partial first day.
		now := nowFunc().UTC()
		startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		since := startOfToday.AddDate(0, 0, -(days - 1))

		ctx := c.Request.Context()
		totals, err := store.SumUsageSince(ctx, since)
		if err != nil {
			slog.Error("admin: usage totals failed", "err", err.Error())
			writeInternal(c, "could not load usage totals")
			return
		}
		daily, err := store.DailyUsageSince(ctx, since)
		if err != nil {
			slog.Error("admin: usage daily failed", "err", err.Error())
			writeInternal(c, "could not load usage series")
			return
		}
		byModel, err := store.UsageByModelSince(ctx, since)
		if err != nil {
			slog.Error("admin: usage by-model failed", "err", err.Error())
			writeInternal(c, "could not load usage by model")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"window_days": days,
			"since":       since,
			"summary":     totals,
			"daily":       fillDailyGaps(daily, since, startOfToday),
			"by_model":    emptyIfNil(byModel),
		})
	}
}

// parseDays clamps the query value into [1, maxUsageDays], falling back
// to the default on a missing or malformed value.
func parseDays(raw string) int {
	if raw == "" {
		return defaultUsageDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultUsageDays
	}
	if n > maxUsageDays {
		return maxUsageDays
	}
	return n
}

// fillDailyGaps returns one entry per calendar day in [since, lastDay],
// inserting zero rows for days the query returned nothing — so the chart
// x-axis is continuous instead of skipping idle days.
func fillDailyGaps(rows []repository.DailyUsage, since, lastDay time.Time) []repository.DailyUsage {
	byDay := make(map[string]repository.DailyUsage, len(rows))
	for _, r := range rows {
		byDay[r.Day.UTC().Format("2006-01-02")] = r
	}
	var out []repository.DailyUsage
	for d := since; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if got, ok := byDay[key]; ok {
			out = append(out, got)
		} else {
			out = append(out, repository.DailyUsage{Day: d})
		}
	}
	return out
}

func emptyIfNil(m []repository.ModelUsage) []repository.ModelUsage {
	if m == nil {
		return []repository.ModelUsage{}
	}
	return m
}
