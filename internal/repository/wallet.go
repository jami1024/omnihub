package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrWalletUserNotFound is returned by AddEntry when user_id references a
// user that does not exist (foreign-key violation).
var ErrWalletUserNotFound = errors.New("wallet user not found")

// WalletRepo persists the wallet_ledger table — the credit side of a
// user's prepaid balance. Consumption is NOT stored here; it is derived
// from message_requests at balance-computation time.
type WalletRepo struct {
	pool *pgxpool.Pool
}

// NewWalletRepo wires the repository onto an existing pgx pool.
func NewWalletRepo(pool *pgxpool.Pool) *WalletRepo {
	return &WalletRepo{pool: pool}
}

// Credits returns the lifetime sum of ledger amounts for a user (top-ups,
// redemptions, refunds, and adjustments — the latter may be negative).
func (r *WalletRepo) Credits(ctx context.Context, userID int64) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_usd), 0)::float8 FROM wallet_ledger WHERE user_id = $1`,
		userID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum credits for user %d: %w", userID, err)
	}
	return total, nil
}

// WalletEntry is one ledger row for display.
type WalletEntry struct {
	ID        int64
	Kind      string
	AmountUSD float64
	Note      string
	CreatedBy string
	CreatedAt time.Time
}

// AddEntry appends a credit event. kind is topup | redeem | refund |
// adjust; amountUSD is positive for a credit and may be negative for an
// adjustment.
func (r *WalletRepo) AddEntry(ctx context.Context, userID int64, kind string, amountUSD float64, note, createdBy string) error {
	const q = `
        INSERT INTO wallet_ledger (user_id, kind, amount_usd, note, created_by)
        VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''))`
	if _, err := r.pool.Exec(ctx, q, userID, kind, amountUSD, note, createdBy); err != nil {
		if isForeignKeyViolation(err) {
			return ErrWalletUserNotFound
		}
		return fmt.Errorf("insert wallet entry for user %d: %w", userID, err)
	}
	return nil
}

// ListEntries returns a user's ledger rows, newest first, capped at limit.
func (r *WalletRepo) ListEntries(ctx context.Context, userID int64, limit int) ([]WalletEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
        SELECT id, kind, amount_usd::float8, COALESCE(note, ''), COALESCE(created_by, ''), created_at
          FROM wallet_ledger
         WHERE user_id = $1
         ORDER BY created_at DESC
         LIMIT $2`
	rows, err := r.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query wallet_ledger for user %d: %w", userID, err)
	}
	defer rows.Close()

	out := []WalletEntry{}
	for rows.Next() {
		var e WalletEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.AmountUSD, &e.Note, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan wallet_ledger: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
