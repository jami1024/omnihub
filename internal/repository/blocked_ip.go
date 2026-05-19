package repository

import (
	"context"
	"fmt"

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
