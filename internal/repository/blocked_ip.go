package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/blockedip"
)

// BlockedIPRepo persists the blocked_ips table (per-IP policies).
type BlockedIPRepo struct {
	pool *pgxpool.Pool
}

// NewBlockedIPRepo wires the repository onto an existing pgx pool.
func NewBlockedIPRepo(pool *pgxpool.Pool) *BlockedIPRepo {
	return &BlockedIPRepo{pool: pool}
}

// ErrBlockedIPNotFound is returned when an update/delete misses.
var ErrBlockedIPNotFound = errors.New("blocked ip not found")

// ErrBlockedIPExists is returned when an insert collides with the
// primary key (the IP is already in the table).
var ErrBlockedIPExists = errors.New("blocked ip already exists")

// ListAll returns every IP policy. Used by the in-memory pool's
// Refresh; the list is always small enough to fit in memory.
func (r *BlockedIPRepo) ListAll(ctx context.Context) ([]blockedip.Policy, error) {
	const q = `
        SELECT ip,
               COALESCE(reason, ''),
               COALESCE(rpm_limit, 0),
               COALESCE(tpm_limit, 0),
               COALESCE(concurrent_limit, 0)
          FROM blocked_ips`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query blocked_ips: %w", err)
	}
	defer rows.Close()

	var out []blockedip.Policy
	for rows.Next() {
		var p blockedip.Policy
		if err := rows.Scan(&p.IP, &p.Reason, &p.RPMLimit, &p.TPMLimit, &p.ConcurrentLimit); err != nil {
			return nil, fmt.Errorf("scan blocked_ips: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// BlockedIPRecord is the admin-facing view of one blocked_ips row. It
// carries the audit columns (created_at, created_by) the hot-path
// blockedip.Policy deliberately omits, and represents the limit columns
// as nullable pointers so the admin UI can tell "no cap" (NULL) apart
// from an explicit value.
type BlockedIPRecord struct {
	IP              string
	Reason          string
	RPMLimit        *int
	TPMLimit        *int64
	ConcurrentLimit *int
	CreatedAt       time.Time
	CreatedBy       string
}

// BlockedIPParams carries the mutable columns for insert/update. A nil
// limit writes NULL ("no cap"); when all three are nil the row is a
// hard block (403).
type BlockedIPParams struct {
	Reason          string
	RPMLimit        *int
	TPMLimit        *int64
	ConcurrentLimit *int
}

// ListRecords returns every row with its audit columns, newest first.
// Separate from ListAll (which feeds the hot-path pool) so the admin
// surface can show created_at / created_by without widening Policy.
func (r *BlockedIPRepo) ListRecords(ctx context.Context) ([]BlockedIPRecord, error) {
	const q = `
        SELECT ip, COALESCE(reason, ''),
               rpm_limit, tpm_limit, concurrent_limit,
               created_at, COALESCE(created_by, '')
          FROM blocked_ips
         ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query blocked_ips: %w", err)
	}
	defer rows.Close()

	var out []BlockedIPRecord
	for rows.Next() {
		var rec BlockedIPRecord
		if err := rows.Scan(
			&rec.IP, &rec.Reason,
			&rec.RPMLimit, &rec.TPMLimit, &rec.ConcurrentLimit,
			&rec.CreatedAt, &rec.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("scan blocked_ips: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Insert adds a new IP policy. Returns ErrBlockedIPExists when the IP is
// already present (primary-key collision).
func (r *BlockedIPRepo) Insert(ctx context.Context, ip string, p BlockedIPParams, createdBy string) error {
	const q = `
        INSERT INTO blocked_ips (ip, reason, rpm_limit, tpm_limit, concurrent_limit, created_by)
        VALUES ($1, NULLIF($2, ''), $3, $4, $5, NULLIF($6, ''))`
	_, err := r.pool.Exec(ctx, q,
		ip, p.Reason, p.RPMLimit, p.TPMLimit, p.ConcurrentLimit, createdBy)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrBlockedIPExists
		}
		return fmt.Errorf("insert blocked_ip %q: %w", ip, err)
	}
	return nil
}

// Update replaces the policy columns of an existing IP. created_at and
// created_by are immutable. Returns ErrBlockedIPNotFound when no row
// matches.
func (r *BlockedIPRepo) Update(ctx context.Context, ip string, p BlockedIPParams) error {
	const q = `
        UPDATE blocked_ips SET
            reason = NULLIF($2, ''),
            rpm_limit = $3, tpm_limit = $4, concurrent_limit = $5
         WHERE ip = $1`
	tag, err := r.pool.Exec(ctx, q,
		ip, p.Reason, p.RPMLimit, p.TPMLimit, p.ConcurrentLimit)
	if err != nil {
		return fmt.Errorf("update blocked_ip %q: %w", ip, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBlockedIPNotFound
	}
	return nil
}

// Delete removes an IP policy. Returns ErrBlockedIPNotFound when no row
// matches.
func (r *BlockedIPRepo) Delete(ctx context.Context, ip string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM blocked_ips WHERE ip = $1`, ip)
	if err != nil {
		return fmt.Errorf("delete blocked_ip %q: %w", ip, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBlockedIPNotFound
	}
	return nil
}
