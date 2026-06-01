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
