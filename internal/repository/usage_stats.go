package repository

import (
	"context"
	"fmt"
	"time"
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

// SumUsageSince returns the aggregate totals for requests at or after
// `since`. A request counts as an error when status_code is >= 400 or
// null (the upstream call never produced a status).
func (r *MessageRequestRepo) SumUsageSince(ctx context.Context, since time.Time) (UsageTotals, error) {
	const q = `
        SELECT
            COUNT(*),
            COALESCE(SUM(cost_usd), 0)::float8,
            COALESCE(SUM(input_tokens), 0),
            COALESCE(SUM(output_tokens), 0),
            COALESCE(SUM(cache_creation_input_tokens), 0),
            COALESCE(SUM(cache_read_input_tokens), 0),
            COUNT(*) FILTER (WHERE status_code IS NULL OR status_code >= 400)
        FROM message_requests
        WHERE created_at >= $1`
	var t UsageTotals
	err := r.pool.QueryRow(ctx, q, since).Scan(
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

// DailyUsageSince returns one row per UTC calendar day in the window,
// oldest first. Days with no traffic are absent (the caller fills gaps
// for the chart's x-axis).
func (r *MessageRequestRepo) DailyUsageSince(ctx context.Context, since time.Time) ([]DailyUsage, error) {
	const q = `
        SELECT
            date_trunc('day', created_at AT TIME ZONE 'UTC') AS day,
            COUNT(*),
            COALESCE(SUM(cost_usd), 0)::float8,
            COALESCE(SUM(input_tokens), 0),
            COALESCE(SUM(output_tokens), 0)
        FROM message_requests
        WHERE created_at >= $1
        GROUP BY day
        ORDER BY day ASC`
	rows, err := r.pool.Query(ctx, q, since)
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

// UsageByModelSince returns per-model aggregates for the window, ordered
// by cost descending (most expensive first) so the dashboard can show
// the top spenders without re-sorting.
func (r *MessageRequestRepo) UsageByModelSince(ctx context.Context, since time.Time) ([]ModelUsage, error) {
	const q = `
        SELECT
            model,
            COUNT(*),
            COALESCE(SUM(cost_usd), 0)::float8,
            COALESCE(SUM(input_tokens), 0),
            COALESCE(SUM(output_tokens), 0)
        FROM message_requests
        WHERE created_at >= $1
        GROUP BY model
        ORDER BY SUM(cost_usd) DESC NULLS LAST, COUNT(*) DESC`
	rows, err := r.pool.Query(ctx, q, since)
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
