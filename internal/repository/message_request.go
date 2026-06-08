// Package repository owns the persistence layer for OmniHub. Each
// type maps to one table; helpers expose just the operations the
// service layer needs (no generic CRUD).
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/pricing"
)

// MessageRequest captures a single completed inbound API call.
//
// One row is inserted per request, after the response has finished
// streaming or returned. Nullable columns are modelled as pointers so
// the writer can pass nil → NULL instead of zero values that would
// pollute analytics (e.g. duration_ms = 0 for a request that never
// finished).
type MessageRequest struct {
	CreatedAt time.Time // when the request entered the gateway

	// Request side
	KeyName *string // virtual API key label ("alice"); nil if auth disabled
	Method  string
	Path    string
	Model   string // model requested by the client
	Stream  bool

	// Provider side
	ProviderName string  // "anthropic", "claude-platform", ...
	AccountName  string  // upstream account that served the request
	ActualModel  *string // model upstream returned (may differ from request)

	// Outcome
	StatusCode        *int
	DurationMs        *int64
	TtfbMs            *int64 // time to first body byte; nil for non-streaming
	ErrorMessage      *string
	UpstreamRequestID *string // Anthropic's id field / x-request-id

	// Token usage
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64

	// CostUSD is the calculated upstream cost in US dollars, computed
	// from token counts and the per-model price table. Nil when the
	// model is not in the table (pricing.Calculate returned false).
	CostUSD *float64

	// BilledUSD is what the owning user is charged: CostUSD times their
	// price_ratio. Nil for ownerless keys / unpriced models (billed == cost).
	BilledUSD *float64

	// PlanBilledUSD and WalletBilledUSD split BilledUSD by payment source.
	// Nil means legacy row or billing split unavailable.
	PlanBilledUSD   *float64
	WalletBilledUSD *float64
	PlanGrantID     *int64

	// CostBreakdown carries the per-bucket detail (input / output /
	// cache_creation_5m / cache_creation_1h / cache_read / multiplier)
	// that backs cost_breakdown JSONB. Nil persists NULL.
	CostBreakdown *pricing.Breakdown

	// ClientIP is the immediate caller's IP as Gin computed it
	// (honouring trusted proxies). Nil when unavailable.
	ClientIP *string

	// UserAgent is the raw User-Agent header from the inbound
	// request. Nil when the client did not send one.
	UserAgent *string

	// SessionID is the value of the x-claude-code-session-id header
	// when present. Anchors a row to one Claude Code CLI session so
	// real-user counting works even behind shared NAT.
	SessionID *string
}

// MessageRequestRepo provides batched persistence for MessageRequest
// rows. Methods are safe for concurrent use.
type MessageRequestRepo struct {
	pool *pgxpool.Pool
}

// NewMessageRequestRepo wires the repository onto an existing pgx pool.
func NewMessageRequestRepo(pool *pgxpool.Pool) *MessageRequestRepo {
	return &MessageRequestRepo{pool: pool}
}

// InsertBatch persists every MessageRequest in batch in a single
// INSERT statement. Empty batches return immediately. On error no
// rows are committed (the statement is implicitly transactional).
//
// pgx already handles parameter limits internally and Postgres accepts
// up to 65 535 parameters per statement (~3 600 rows at 18 cols each).
// The WriteBuffer caps batch size well below that.
func (r *MessageRequestRepo) InsertBatch(ctx context.Context, batch []MessageRequest) error {
	if len(batch) == 0 {
		return nil
	}

	const colsPerRow = 27
	var sb strings.Builder
	sb.Grow(512 + len(batch)*64)
	sb.WriteString(`INSERT INTO message_requests (
        created_at, key_name, method, path, model, actual_model, stream,
        status_code, duration_ms, ttfb_ms, error_message,
        input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
        provider_name, account_name, upstream_request_id, cost_usd, cost_breakdown,
        client_ip, user_agent, session_id, billed_usd,
        plan_billed_usd, wallet_billed_usd, plan_grant_id
    ) VALUES `)

	args := make([]any, 0, len(batch)*colsPerRow)
	for i, m := range batch {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(")
		for j := 1; j <= colsPerRow; j++ {
			if j > 1 {
				sb.WriteString(",")
			}
			fmt.Fprintf(&sb, "$%d", len(args)+j)
		}
		sb.WriteString(")")

		var breakdownJSON []byte
		if m.CostBreakdown != nil {
			b, err := json.Marshal(m.CostBreakdown)
			if err != nil {
				return fmt.Errorf("marshal cost_breakdown: %w", err)
			}
			breakdownJSON = b
		}

		args = append(args,
			m.CreatedAt,
			m.KeyName, m.Method, m.Path, m.Model, m.ActualModel, m.Stream,
			m.StatusCode, m.DurationMs, m.TtfbMs, m.ErrorMessage,
			m.InputTokens, m.OutputTokens, m.CacheCreationInputTokens, m.CacheReadInputTokens,
			m.ProviderName, m.AccountName, m.UpstreamRequestID, m.CostUSD, breakdownJSON,
			m.ClientIP, m.UserAgent, m.SessionID, m.BilledUSD,
			m.PlanBilledUSD, m.WalletBilledUSD, m.PlanGrantID,
		)
	}

	if _, err := r.pool.Exec(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("message_requests bulk insert (%d rows): %w", len(batch), err)
	}
	return nil
}

// SumCostByKey returns the rolling 24h USD spend recorded for keyName,
// summed from cost_usd in message_requests. Rows with NULL cost (e.g.
// unknown-model requests where pricing.Calculate returned false) are
// excluded by SUM's NULL semantics; COALESCE turns "no matching rows"
// into 0 so the caller does not need to handle pgx.ErrNoRows.
//
// The query is served by the existing (key_name, created_at DESC) index.
func (r *MessageRequestRepo) SumCostByKey(ctx context.Context, keyName string) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(cost_usd), 0)::float8
        FROM message_requests
        WHERE key_name = $1
          AND created_at > NOW() - INTERVAL '24 hours'`,
		keyName).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum cost for %q: %w", keyName, err)
	}
	return total, nil
}

// SumCostByUser returns the LIFETIME request cost across every key owned
// by a portal user, joining api_keys on key_name. Used to derive a
// prepaid balance (credits minus this). Lifetime (not a rolling window)
// because prepaid credits are themselves lifetime.
func (r *MessageRequestRepo) SumCostByUser(ctx context.Context, userID int64) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(mr.cost_usd), 0)::float8
          FROM message_requests mr
          JOIN api_keys k ON k.name = mr.key_name
         WHERE k.user_id = $1`,
		userID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum cost for user %d: %w", userID, err)
	}
	return total, nil
}

// SumBilledByUser returns the LIFETIME amount charged to a portal user
// wallet across every key they own. New rows use wallet_billed_usd so
// plan-covered usage does not reduce wallet balance; legacy rows fall
// back to billed_usd / cost_usd.
func (r *MessageRequestRepo) SumBilledByUser(ctx context.Context, userID int64) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(COALESCE(mr.wallet_billed_usd, mr.billed_usd, mr.cost_usd)), 0)::float8
          FROM message_requests mr
          JOIN api_keys k ON k.name = mr.key_name
         WHERE k.user_id = $1`,
		userID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum billed for user %d: %w", userID, err)
	}
	return total, nil
}

// SumCostByAccount returns the rolling 24h USD spend recorded against
// the named upstream account. Mirrors SumCostByKey but groups on
// account_name; used to enforce per-account daily spend caps.
func (r *MessageRequestRepo) SumCostByAccount(ctx context.Context, accountName string) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(cost_usd), 0)::float8
        FROM message_requests
        WHERE account_name = $1
          AND created_at > NOW() - INTERVAL '24 hours'`,
		accountName).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum 24h cost for account %q: %w", accountName, err)
	}
	return total, nil
}

// TotalCostByAccount returns the lifetime USD spend recorded against the
// named upstream account (no time bound); used to enforce per-account
// total spend caps.
func (r *MessageRequestRepo) TotalCostByAccount(ctx context.Context, accountName string) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(cost_usd), 0)::float8
        FROM message_requests
        WHERE account_name = $1`,
		accountName).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum lifetime cost for account %q: %w", accountName, err)
	}
	return total, nil
}
