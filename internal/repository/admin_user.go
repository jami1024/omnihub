package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/admin"
)

// AdminUserRepo persists web-admin login accounts. Distinct from
// ApiKeyRepo (which authenticates gateway traffic). Reads are
// per-request — there is no in-memory pool here, login volume is low
// enough that DB-on-every-login is fine and avoids the cache-coherency
// machinery required by api_keys / accounts.
type AdminUserRepo struct {
	pool *pgxpool.Pool
}

// NewAdminUserRepo wires the repository onto an existing pgx pool.
func NewAdminUserRepo(pool *pgxpool.Pool) *AdminUserRepo {
	return &AdminUserRepo{pool: pool}
}

// ErrAdminUserNotFound is returned when a single-row lookup misses.
var ErrAdminUserNotFound = errors.New("admin user not found")

// GetByUsername fetches one row by username. Used by the login handler.
func (r *AdminUserRepo) GetByUsername(ctx context.Context, username string) (*admin.User, error) {
	const q = `
        SELECT id, username, password_hash, enabled, created_at, updated_at
          FROM admin_users
         WHERE username = $1`
	var u admin.User
	err := r.pool.QueryRow(ctx, q, username).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Enabled, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAdminUserNotFound
		}
		return nil, fmt.Errorf("query admin_user %q: %w", username, err)
	}
	return &u, nil
}

// ListAll returns every admin user for the CLI `admin list` command.
// Password hashes are included so the CLI can surface a "this user
// exists" view; HTTP handlers MUST scrub the field before responding.
func (r *AdminUserRepo) ListAll(ctx context.Context) ([]*admin.User, error) {
	const q = `
        SELECT id, username, password_hash, enabled, created_at, updated_at
          FROM admin_users
         ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query admin_users: %w", err)
	}
	defer rows.Close()

	var out []*admin.User
	for rows.Next() {
		var u admin.User
		if err := rows.Scan(
			&u.ID, &u.Username, &u.PasswordHash, &u.Enabled, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admin_user: %w", err)
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

// CountAll returns the row count. Useful for "is the deployment
// bootstrapped" checks at startup.
func (r *AdminUserRepo) CountAll(ctx context.Context) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// AdminUserInsertParams carries everything Insert needs. The caller has
// already bcrypted the cleartext password.
type AdminUserInsertParams struct {
	Username     string
	PasswordHash string
	Enabled      bool
}

// Insert creates a new admin_users row.
func (r *AdminUserRepo) Insert(ctx context.Context, p AdminUserInsertParams) (int64, error) {
	const q = `
        INSERT INTO admin_users (username, password_hash, enabled)
        VALUES ($1, $2, $3)
        RETURNING id`
	var id int64
	if err := r.pool.QueryRow(ctx, q, p.Username, p.PasswordHash, p.Enabled).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert admin_user %q: %w", p.Username, err)
	}
	return id, nil
}

// SetEnabled toggles the enabled flag on the named admin.
func (r *AdminUserRepo) SetEnabled(ctx context.Context, username string, enabled bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE admin_users SET enabled = $1, updated_at = NOW() WHERE username = $2`,
		enabled, username,
	)
	if err != nil {
		return fmt.Errorf("update enabled for %q: %w", username, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAdminUserNotFound
	}
	return nil
}

// UpdatePassword replaces the bcrypt hash for an existing admin. The
// caller is responsible for bcrypting the new cleartext.
func (r *AdminUserRepo) UpdatePassword(ctx context.Context, username, hash string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE admin_users SET password_hash = $1, updated_at = NOW() WHERE username = $2`,
		hash, username,
	)
	if err != nil {
		return fmt.Errorf("update password for %q: %w", username, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAdminUserNotFound
	}
	return nil
}

// Delete hard-deletes an admin user by username.
func (r *AdminUserRepo) Delete(ctx context.Context, username string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM admin_users WHERE username = $1`, username)
	if err != nil {
		return fmt.Errorf("delete admin_user %q: %w", username, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAdminUserNotFound
	}
	return nil
}
