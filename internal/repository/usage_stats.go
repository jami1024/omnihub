package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jami1024/omnihub/internal/service/pricing"
)

// Usage aggregation queries over message_requests, powering the admin
// dashboard. All of them filter on created_at >= since and rely on the
// existing created_at / model indexes. Rows with NULL cost_usd
// (unknown-model requests) contribute 0 to the cost sums via COALESCE.

// UsageTotals is the headline summary for a time window.
type UsageTotals struct {
	Requests            int64   `json:"requests"`
	CostUSD             float64 `json:"cost_usd"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	Errors              int64   `json:"errors"`
}

// DailyUsage is one calendar-day bucket (UTC) of the time series.
type DailyUsage struct {
	Day          time.Time `json:"day"`
	Requests     int64     `json:"requests"`
	CostUSD      float64   `json:"cost_usd"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
}

// ModelUsage aggregates one model across the window.
type ModelUsage struct {
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
}

// keyFilter is the optional "scope to these key names" clause. When keys
// is nil/empty the queries cover all traffic (the admin dashboard); when
// it's a user's key names the same shape powers the portal, scoped to
// just that user. An empty (non-nil) slice yields no rows.
//
//	SumUsageSinceFor(ctx, since, nil)            // all traffic
//	SumUsageSinceFor(ctx, since, []string{...})  // one user's keys

// SumUsageSince returns the aggregate totals for all requests at or
// after `since`.
func (r *MessageRequestRepo) SumUsageSince(ctx context.Context, since time.Time) (UsageTotals, error) {
	return r.SumUsageSinceFor(ctx, since, nil)
}

// SumUsageSinceFor scopes SumUsageSince to a set of key names (nil = all).
// A request counts as an error when status_code is >= 400 or null.
func (r *MessageRequestRepo) SumUsageSinceFor(ctx context.Context, since time.Time, keys []string) (UsageTotals, error) {
	if keys != nil && len(keys) == 0 {
		return UsageTotals{}, nil
	}
	q := `
        SELECT
            COUNT(*),
            COALESCE(SUM(cost_usd), 0)::float8,
            COALESCE(SUM(input_tokens), 0),
            COALESCE(SUM(output_tokens), 0),
            COALESCE(SUM(cache_creation_input_tokens), 0),
            COALESCE(SUM(cache_read_input_tokens), 0),
            COUNT(*) FILTER (WHERE status_code IS NULL OR status_code >= 400)
        FROM message_requests
        WHERE created_at >= $1` + keyClause(keys)
	args := argsWithKeys(since, keys)
	var t UsageTotals
	err := r.pool.QueryRow(ctx, q, args...).Scan(
		&t.Requests, &t.CostUSD,
		&t.InputTokens, &t.OutputTokens,
		&t.CacheCreationTokens, &t.CacheReadTokens,
		&t.Errors,
	)
	if err != nil {
		return UsageTotals{}, fmt.Errorf("sum usage since %s: %w", since, err)
	}
	return t, nil
}

// DailyUsageSince returns one row per UTC calendar day for all traffic.
func (r *MessageRequestRepo) DailyUsageSince(ctx context.Context, since time.Time) ([]DailyUsage, error) {
	return r.DailyUsageSinceFor(ctx, since, nil)
}

// DailyUsageSinceFor scopes DailyUsageSince to a set of key names (nil =
// all). Oldest day first; empty days are absent (the caller fills gaps).
func (r *MessageRequestRepo) DailyUsageSinceFor(ctx context.Context, since time.Time, keys []string) ([]DailyUsage, error) {
	if keys != nil && len(keys) == 0 {
		return nil, nil
	}
	q := `
        SELECT
            date_trunc('day', created_at AT TIME ZONE 'UTC') AS day,
            COUNT(*),
            COALESCE(SUM(cost_usd), 0)::float8,
            COALESCE(SUM(input_tokens), 0),
            COALESCE(SUM(output_tokens), 0)
        FROM message_requests
        WHERE created_at >= $1` + keyClause(keys) + `
        GROUP BY day
        ORDER BY day ASC`
	rows, err := r.pool.Query(ctx, q, argsWithKeys(since, keys)...)
	if err != nil {
		return nil, fmt.Errorf("daily usage since %s: %w", since, err)
	}
	defer rows.Close()

	var out []DailyUsage
	for rows.Next() {
		var d DailyUsage
		if err := rows.Scan(&d.Day, &d.Requests, &d.CostUSD, &d.InputTokens, &d.OutputTokens); err != nil {
			return nil, fmt.Errorf("scan daily usage: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UsageByModelSince returns per-model aggregates for all traffic.
func (r *MessageRequestRepo) UsageByModelSince(ctx context.Context, since time.Time) ([]ModelUsage, error) {
	return r.UsageByModelSinceFor(ctx, since, nil)
}

// UsageByModelSinceFor scopes UsageByModelSince to a set of key names
// (nil = all), ordered by cost descending.
func (r *MessageRequestRepo) UsageByModelSinceFor(ctx context.Context, since time.Time, keys []string) ([]ModelUsage, error) {
	if keys != nil && len(keys) == 0 {
		return nil, nil
	}
	q := `
        SELECT
            model,
            COUNT(*),
            COALESCE(SUM(cost_usd), 0)::float8,
            COALESCE(SUM(input_tokens), 0),
            COALESCE(SUM(output_tokens), 0)
        FROM message_requests
        WHERE created_at >= $1` + keyClause(keys) + `
        GROUP BY model
        ORDER BY SUM(cost_usd) DESC NULLS LAST, COUNT(*) DESC`
	rows, err := r.pool.Query(ctx, q, argsWithKeys(since, keys)...)
	if err != nil {
		return nil, fmt.Errorf("usage by model since %s: %w", since, err)
	}
	defer rows.Close()

	var out []ModelUsage
	for rows.Next() {
		var m ModelUsage
		if err := rows.Scan(&m.Model, &m.Requests, &m.CostUSD, &m.InputTokens, &m.OutputTokens); err != nil {
			return nil, fmt.Errorf("scan model usage: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RequestLogRow is one row of a user's per-request usage log — the
// non-sensitive subset of message_requests safe to show an end user
// (no upstream account / provider / internal request id).
type RequestLogRow struct {
	CreatedAt                time.Time
	KeyName                  string
	Model                    string // actual_model when known, else the requested model
	StatusCode               *int
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
	CostUSD                  float64
	BilledUSD                *float64
	PlanBilledUSD            *float64
	WalletBilledUSD          *float64
	CostBreakdown            *pricing.Breakdown
	DurationMs               *int64
	Error                    string // truncated error_message for failed requests
}

// ListByKeyNames returns one page of message_requests scoped to the given
// key names (a user's OWN keys), newest first, plus the total matching
// count for pagination. An empty names slice returns NO rows — the portal
// must always pass the authenticated user's own key names so this can
// never widen to all traffic. key_name is globally UNIQUE, so name-based
// scoping cannot leak across users.
func (r *MessageRequestRepo) ListByKeyNames(ctx context.Context, names []string, since time.Time, limit, offset int) ([]RequestLogRow, int, error) {
	if len(names) == 0 {
		return []RequestLogRow{}, 0, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM message_requests WHERE created_at >= $1 AND key_name = ANY($2)`,
		since, names).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count requests: %w", err)
	}

	const q = `
        SELECT created_at, COALESCE(key_name, ''),
               COALESCE(NULLIF(actual_model, ''), model),
               status_code, input_tokens, output_tokens,
               cache_creation_input_tokens, cache_read_input_tokens,
               COALESCE(cost_usd, 0)::float8, billed_usd::float8,
               plan_billed_usd::float8, wallet_billed_usd::float8, cost_breakdown,
               duration_ms,
               COALESCE(error_message, '')
          FROM message_requests
         WHERE created_at >= $1 AND key_name = ANY($2)
         ORDER BY created_at DESC
         LIMIT $3 OFFSET $4`
	rows, err := r.pool.Query(ctx, q, since, names, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list requests: %w", err)
	}
	defer rows.Close()

	out := []RequestLogRow{}
	for rows.Next() {
		var row RequestLogRow
		var breakdownJSON []byte
		if err := rows.Scan(&row.CreatedAt, &row.KeyName, &row.Model, &row.StatusCode,
			&row.InputTokens, &row.OutputTokens, &row.CacheCreationInputTokens, &row.CacheReadInputTokens,
			&row.CostUSD, &row.BilledUSD, &row.PlanBilledUSD, &row.WalletBilledUSD, &breakdownJSON,
			&row.DurationMs, &row.Error); err != nil {
			return nil, 0, fmt.Errorf("scan request row: %w", err)
		}
		if len(breakdownJSON) > 0 {
			var breakdown pricing.Breakdown
			if err := json.Unmarshal(breakdownJSON, &breakdown); err != nil {
				return nil, 0, fmt.Errorf("decode cost_breakdown: %w", err)
			}
			row.CostBreakdown = &breakdown
		}
		if len(row.Error) > 300 {
			row.Error = row.Error[:300]
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// keyClause appends "AND key_name = ANY($2)" when scoping to a user's
// keys, or "" for all traffic. Pair with argsWithKeys.
func keyClause(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return " AND key_name = ANY($2)"
}

// argsWithKeys builds the query args: just `since` for all-traffic, or
// `since, keys` when scoping.
func argsWithKeys(since time.Time, keys []string) []any {
	if len(keys) == 0 {
		return []any{since}
	}
	return []any{since, keys}
}
