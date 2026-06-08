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

// requestStore is the scoped slice of MessageRequestRepo the per-request
// log needs.
type requestStore interface {
	ListByKeyNames(ctx context.Context, names []string, since time.Time, limit, offset int) ([]repository.RequestLogRow, int, error)
}

// requestLogPageSize is the fixed page size for the portal request log.
const requestLogPageSize = 50

// RequestsHandler returns GET /portal/api/requests?days=N&page=P — a
// paginated, newest-first per-request log SCOPED to the authenticated
// user's own keys. A user with no keys gets an empty page (never another
// user's traffic).
func RequestsHandler(store requestStore, keys keyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := guard.UserID(c)
		days := parseDays(c.Query("days"))
		page := parsePage(c.Query("page"))

		owned, err := keys.ListByUser(c.Request.Context(), uid)
		if err != nil {
			slog.Error("portal: requests list keys failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not load requests")
			return
		}
		// Non-nil, scoped to THIS user's keys — empty when the user has
		// none, which the repo treats as "no rows", never all traffic.
		names := make([]string, 0, len(owned))
		for _, k := range owned {
			names = append(names, k.Name)
		}

		now := nowFunc().UTC()
		startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		since := startOfToday.AddDate(0, 0, -(days - 1))

		rows, total, err := store.ListByKeyNames(
			c.Request.Context(), names, since, requestLogPageSize, (page-1)*requestLogPageSize)
		if err != nil {
			slog.Error("portal: list requests failed", "uid", uid, "err", err.Error())
			writeInternal(c, "could not load requests")
			return
		}

		out := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			out = append(out, gin.H{
				"created_at":                  r.CreatedAt,
				"key_name":                    r.KeyName,
				"model":                       r.Model,
				"status_code":                 r.StatusCode,
				"input_tokens":                r.InputTokens,
				"output_tokens":               r.OutputTokens,
				"cache_creation_input_tokens": r.CacheCreationInputTokens,
				"cache_read_input_tokens":     r.CacheReadInputTokens,
				"cost_usd":                    r.CostUSD,
				"billed_usd":                  r.BilledUSD,
				"plan_billed_usd":             r.PlanBilledUSD,
				"wallet_billed_usd":           r.WalletBilledUSD,
				"cost_breakdown":              r.CostBreakdown,
				"duration_ms":                 r.DurationMs,
				"error":                       r.Error,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"window_days": days,
			"page":        page,
			"page_size":   requestLogPageSize,
			"total":       total,
			"requests":    out,
		})
	}
}

// parsePage clamps the 1-based page number; defaults to 1.
func parsePage(raw string) int {
	if raw == "" {
		return 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 1
	}
	return n
}
