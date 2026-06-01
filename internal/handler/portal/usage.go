package portal

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/guard"
)

// usageStore is the scoped slice of MessageRequestRepo the portal needs.
type usageStore interface {
	SumUsageSinceFor(ctx context.Context, since time.Time, keys []string) (repository.UsageTotals, error)
	DailyUsageSinceFor(ctx context.Context, since time.Time, keys []string) ([]repository.DailyUsage, error)
	UsageByModelSinceFor(ctx context.Context, since time.Time, keys []string) ([]repository.ModelUsage, error)
}

var nowFunc = time.Now

// UsageHandler returns GET /portal/api/usage?days=N — totals, a daily
// series, and a per-model breakdown, scoped to the user's own keys. With
// no keys (an empty, non-nil scope) every figure is zero.
func UsageHandler(store usageStore, keys keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := guard.UserID(c)
		days := parseDays(c.Query("days"))

		owned, err := keys.ListByUser(c.Request.Context(), uid)
		if err != nil {
			slog.Error("portal: usage list keys failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not load usage")
			return
		}
		// Non-nil so the scoped queries cover "this user's keys", which is
		// nothing when the slice is empty (never all traffic).
		names := make([]string, 0, len(owned))
		for _, k := range owned {
			names = append(names, k.Name)
		}

		now := nowFunc().UTC()
		startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		since := startOfToday.AddDate(0, 0, -(days - 1))

		ctx := c.Request.Context()
		totals, err := store.SumUsageSinceFor(ctx, since, names)
		if err != nil {
			writeInternal(c, "could not load usage totals")
			return
		}
		daily, err := store.DailyUsageSinceFor(ctx, since, names)
		if err != nil {
			writeInternal(c, "could not load usage series")
			return
		}
		byModel, err := store.UsageByModelSinceFor(ctx, since, names)
		if err != nil {
			writeInternal(c, "could not load usage by model")
			return
		}
		if byModel == nil {
			byModel = []repository.ModelUsage{}
		}

		c.JSON(http.StatusOK, gin.H{
			"window_days": days,
			"summary":     totals,
			"daily":       fillDailyGaps(daily, since, startOfToday),
			"by_model":    byModel,
		})
	}
}

func parseDays(raw string) int {
	if raw == "" {
		return 7
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 7
	}
	if n > 90 {
		return 90
	}
	return n
}

// fillDailyGaps inserts zero rows for idle days so the chart x-axis is
// continuous from `since` through `lastDay`.
func fillDailyGaps(rows []repository.DailyUsage, since, lastDay time.Time) []repository.DailyUsage {
	byDay := make(map[string]repository.DailyUsage, len(rows))
	for _, r := range rows {
		byDay[r.Day.UTC().Format("2006-01-02")] = r
	}
	out := []repository.DailyUsage{}
	for d := since; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		if got, ok := byDay[d.Format("2006-01-02")]; ok {
			out = append(out, got)
		} else {
			out = append(out, repository.DailyUsage{Day: d})
		}
	}
	return out
}
