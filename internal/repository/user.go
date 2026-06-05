package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepo persists end-user accounts (the portal's source of truth).
// Distinct from AdminUserRepo, which holds console operators.
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo wires the repository onto an existing pgx pool.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

// ErrUserNotFound is returned when a single-row lookup misses.
var ErrUserNotFound = errors.New("user not found")

// ErrUsernameTaken is returned when signup collides with UNIQUE(username).
var ErrUsernameTaken = errors.New("username already taken")

// User is one row of the users table. PasswordHash is bcrypt-encoded.
type User struct {
	ID           int64
	Username     string
	Email        string
	PasswordHash string
	Enabled      bool
	CreatedAt    time.Time
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var (
		u     User
		email *string
	)
	if err := row.Scan(&u.ID, &u.Username, &email, &u.PasswordHash, &u.Enabled, &u.CreatedAt); err != nil {
		return nil, err
	}
	if email != nil {
		u.Email = *email
	}
	return &u, nil
}

const userColumns = `id, username, email, password_hash, enabled, created_at`

// GetByUsername fetches a user by login handle.
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user %q: %w", username, err)
	}
	return u, nil
}

// GetByID fetches a user by primary key.
func (r *UserRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user %d: %w", id, err)
	}
	return u, nil
}

// UserInsertParams carries everything signup needs (password pre-hashed).
type UserInsertParams struct {
	Username     string
	Email        string
	PasswordHash string
}

// Insert creates a user. Returns ErrUsernameTaken on a UNIQUE collision.
func (r *UserRepo) Insert(ctx context.Context, p UserInsertParams) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
        INSERT INTO users (username, email, password_hash)
        VALUES ($1, NULLIF($2, ''), $3) RETURNING id`,
		p.Username, p.Email, p.PasswordHash,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrUsernameTaken
		}
		return 0, fmt.Errorf("insert user %q: %w", p.Username, err)
	}
	return id, nil
}

// UserStat enriches a user row with the admin-management aggregates:
// how many keys they own and how much they've spent in the last 30 days.
type UserStat struct {
	User
	KeyCount   int     `json:"key_count"`
	Spend30d   float64 `json:"spend_30d"`
	PriceRatio float64 `json:"price_ratio"`
}

// ListWithStats returns every user with their key count and 30-day
// spend, newest first. The spend joins message_requests on key_name =
// the key's name (portal keys carry no separate label, so this
// attributes cleanly); the (key_name, created_at) index serves it.
func (r *UserRepo) ListWithStats(ctx context.Context) ([]UserStat, error) {
	const q = `
        SELECT u.id, u.username, u.email, u.enabled, u.created_at,
               COUNT(DISTINCT k.id) AS key_count,
               COALESCE(SUM(mr.cost_usd), 0)::float8 AS spend_30d,
               u.price_ratio::float8
          FROM users u
          LEFT JOIN api_keys k ON k.user_id = u.id
          LEFT JOIN message_requests mr
                 ON mr.key_name = k.name
                AND mr.created_at > NOW() - INTERVAL '30 days'
         GROUP BY u.id, u.username, u.email, u.enabled, u.created_at, u.price_ratio
         ORDER BY u.created_at DESC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query users with stats: %w", err)
	}
	defer rows.Close()
	var out []UserStat
	for rows.Next() {
		var (
			s     UserStat
			email *string
		)
		if err := rows.Scan(&s.ID, &s.Username, &email, &s.Enabled, &s.CreatedAt, &s.KeyCount, &s.Spend30d, &s.PriceRatio); err != nil {
			return nil, fmt.Errorf("scan user stat: %w", err)
		}
		if email != nil {
			s.Email = *email
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetEnabled toggles a user's enabled flag (a disabled user can't log in).
func (r *UserRepo) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET enabled = $2, updated_at = NOW() WHERE id = $1`, id, enabled)
	if err != nil {
		return fmt.Errorf("set user %d enabled: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetPriceRatio updates a user's billing markup (sell price = cost ×
// ratio). A ratio of 1.0 bills at cost; 0 makes their usage free.
func (r *UserRepo) SetPriceRatio(ctx context.Context, id int64, ratio float64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET price_ratio = $2, updated_at = NOW() WHERE id = $1`, id, ratio)
	if err != nil {
		return fmt.Errorf("set user %d price_ratio: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// DeleteByID hard-deletes a user. Their keys survive but become unowned
// (api_keys.user_id → NULL via the FK's ON DELETE SET NULL).
func (r *UserRepo) DeleteByID(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete user %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ListAll returns every user (admin management view), newest first.
func (r *UserRepo) ListAll(ctx context.Context) ([]*User, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
