package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AccountHealthEvent captures one transition of an account's
// circuit-breaker state. Stored verbatim in account_health_events.
//
// AccountName is denormalised so deleting the corresponding account
// row does not break post-mortem queries. Reason is nullable and
// populated only for failure-driven transitions; cooldown expiry
// (open→half-open) carries no error.
type AccountHealthEvent struct {
	CreatedAt    time.Time
	AccountID    int64
	AccountName  string
	FromState    string
	ToState      string
	FailureCount int
	Reason       *string
}

// AccountHealthEventRepo is the persistence façade for the
// account_health_events table.
type AccountHealthEventRepo struct {
	pool *pgxpool.Pool
}

// NewAccountHealthEventRepo wires the repo onto an existing pgx pool.
func NewAccountHealthEventRepo(pool *pgxpool.Pool) *AccountHealthEventRepo {
	return &AccountHealthEventRepo{pool: pool}
}

// Insert persists a single transition. Designed for direct use from
// the background recorder goroutine — volume is low (a handful per
// account per day in healthy operation) so batching is not
// worthwhile.
func (r *AccountHealthEventRepo) Insert(ctx context.Context, ev AccountHealthEvent) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO account_health_events (
            created_at, account_id, account_name,
            from_state, to_state, failure_count, reason
        ) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ev.CreatedAt, ev.AccountID, ev.AccountName,
		ev.FromState, ev.ToState, ev.FailureCount, ev.Reason,
	)
	if err != nil {
		return fmt.Errorf("account_health_events insert: %w", err)
	}
	return nil
}

// ListRecent returns the most-recent N transitions for accountID,
// newest first. Used by admin tooling ("why did this account flap?").
func (r *AccountHealthEventRepo) ListRecent(ctx context.Context, accountID int64, limit int) ([]AccountHealthEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
        SELECT created_at, account_id, account_name,
               from_state, to_state, failure_count, reason
        FROM account_health_events
        WHERE account_id = $1
        ORDER BY created_at DESC
        LIMIT $2`,
		accountID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("account_health_events list: %w", err)
	}
	defer rows.Close()

	var out []AccountHealthEvent
	for rows.Next() {
		var ev AccountHealthEvent
		if err := rows.Scan(
			&ev.CreatedAt, &ev.AccountID, &ev.AccountName,
			&ev.FromState, &ev.ToState, &ev.FailureCount, &ev.Reason,
		); err != nil {
			return nil, fmt.Errorf("account_health_events scan: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
