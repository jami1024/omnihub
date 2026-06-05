package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/redemption"
)

// RedemptionRepo persists prepaid gift codes. Only the code hash is
// stored; redeeming a code credits the user's wallet in the same
// transaction so a code can never be redeemed twice.
type RedemptionRepo struct {
	pool *pgxpool.Pool
}

// NewRedemptionRepo wires the repository onto an existing pgx pool.
func NewRedemptionRepo(pool *pgxpool.Pool) *RedemptionRepo {
	return &RedemptionRepo{pool: pool}
}

// ErrRedemptionInvalid is returned when a code does not exist, was already
// redeemed, or has expired — deliberately indistinguishable so a caller
// cannot probe which codes exist.
var ErrRedemptionInvalid = errors.New("redemption code invalid, already used, or expired")

// GenerateBatch inserts count codes each worth amountUSD and returns the
// cleartext codes (shown exactly once) plus the batch id.
func (r *RedemptionRepo) GenerateBatch(ctx context.Context, count int, amountUSD float64, expiresAt *time.Time, createdBy string) ([]string, string, error) {
	batchID := redemption.BatchID()
	const q = `
        INSERT INTO redemption_codes (code_hash, amount_usd, batch_id, expires_at, created_by)
        VALUES ($1, $2, $3, $4, NULLIF($5, ''))`
	codes := make([]string, 0, count)
	for len(codes) < count {
		code := redemption.Generate()
		_, err := r.pool.Exec(ctx, q, redemption.HashOf(code), amountUSD, batchID, expiresAt, createdBy)
		if err != nil {
			if isUniqueViolation(err) {
				continue // astronomically unlikely hash collision — just retry
			}
			return nil, "", fmt.Errorf("insert redemption code: %w", err)
		}
		codes = append(codes, code)
	}
	return codes, batchID, nil
}

// Redeem marks a code used and credits the user's wallet atomically.
// Returns the credited amount, or ErrRedemptionInvalid.
func (r *RedemptionRepo) Redeem(ctx context.Context, code string, userID int64) (float64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin redeem tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var amount float64
	err = tx.QueryRow(ctx, `
        UPDATE redemption_codes
           SET status = 'redeemed', redeemed_by = $2, redeemed_at = NOW()
         WHERE code_hash = $1
           AND status = 'unused'
           AND (expires_at IS NULL OR expires_at > NOW())
        RETURNING amount_usd::float8`,
		redemption.HashOf(code), userID).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrRedemptionInvalid
	}
	if err != nil {
		return 0, fmt.Errorf("redeem code: %w", err)
	}

	if _, err := tx.Exec(ctx, `
        INSERT INTO wallet_ledger (user_id, kind, amount_usd, note, created_by)
        VALUES ($1, 'redeem', $2, 'redeemed code', 'self')`,
		userID, amount); err != nil {
		return 0, fmt.Errorf("credit wallet on redeem: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit redeem: %w", err)
	}
	return amount, nil
}

// RedemptionBatch is the admin-facing summary of one generation batch.
type RedemptionBatch struct {
	BatchID   string
	AmountUSD float64
	Total     int
	Redeemed  int
	ExpiresAt *time.Time
	CreatedBy string
	CreatedAt time.Time
}

// ListBatches returns recent batches with redeemed counts, newest first.
func (r *RedemptionRepo) ListBatches(ctx context.Context, limit int) ([]RedemptionBatch, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
        SELECT COALESCE(batch_id, ''), MAX(amount_usd)::float8, COUNT(*),
               COUNT(*) FILTER (WHERE status = 'redeemed'),
               MAX(expires_at), COALESCE(MAX(created_by), ''), MAX(created_at)
          FROM redemption_codes
         GROUP BY batch_id
         ORDER BY MAX(created_at) DESC
         LIMIT $1`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query redemption batches: %w", err)
	}
	defer rows.Close()

	out := []RedemptionBatch{}
	for rows.Next() {
		var b RedemptionBatch
		if err := rows.Scan(&b.BatchID, &b.AmountUSD, &b.Total, &b.Redeemed,
			&b.ExpiresAt, &b.CreatedBy, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan redemption batch: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
