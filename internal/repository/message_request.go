// Package repository owns the persistence layer for OmniHub. Each
// type maps to one table; helpers expose just the operations the
// service layer needs (no generic CRUD).
package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

	const colsPerRow = 18
	var sb strings.Builder
	sb.Grow(512 + len(batch)*64)
	sb.WriteString(`INSERT INTO message_requests (
        created_at, key_name, method, path, model, actual_model, stream,
        status_code, duration_ms, ttfb_ms, error_message,
        input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
        provider_name, account_name, upstream_request_id
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

		args = append(args,
			m.CreatedAt,
			m.KeyName, m.Method, m.Path, m.Model, m.ActualModel, m.Stream,
			m.StatusCode, m.DurationMs, m.TtfbMs, m.ErrorMessage,
			m.InputTokens, m.OutputTokens, m.CacheCreationInputTokens, m.CacheReadInputTokens,
			m.ProviderName, m.AccountName, m.UpstreamRequestID,
		)
	}

	if _, err := r.pool.Exec(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("message_requests bulk insert (%d rows): %w", len(batch), err)
	}
	return nil
}
