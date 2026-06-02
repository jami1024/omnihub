package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProviderGroup is one row of the provider_groups table plus a derived
// count of how many accounts reference it.
type ProviderGroup struct {
	ID             int64
	Name           string
	CostMultiplier float64
	Description    string
	AccountCount   int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ErrGroupNotFound is returned when a single-row lookup misses.
var ErrGroupNotFound = errors.New("provider group not found")

// ErrGroupNameTaken is returned on a UNIQUE(name) collision.
var ErrGroupNameTaken = errors.New("provider group name already in use")

// ProviderGroupRepo persists provider groups.
type ProviderGroupRepo struct {
	pool *pgxpool.Pool
}

// NewProviderGroupRepo wires the repository onto an existing pool.
func NewProviderGroupRepo(pool *pgxpool.Pool) *ProviderGroupRepo {
	return &ProviderGroupRepo{pool: pool}
}

// List returns every group ordered by name, each with its current
// account count (a LEFT JOIN so empty groups still appear).
func (r *ProviderGroupRepo) List(ctx context.Context) ([]ProviderGroup, error) {
	const q = `
        SELECT g.id, g.name, g.cost_multiplier, g.description,
               COUNT(a.id), g.created_at, g.updated_at
          FROM provider_groups g
          LEFT JOIN accounts a ON a.group_id = g.id
         GROUP BY g.id
         ORDER BY g.name ASC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query provider groups: %w", err)
	}
	defer rows.Close()

	var out []ProviderGroup
	for rows.Next() {
		var g ProviderGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.CostMultiplier, &g.Description,
			&g.AccountCount, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider group: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetByID fetches one group (account count included).
func (r *ProviderGroupRepo) GetByID(ctx context.Context, id int64) (*ProviderGroup, error) {
	const q = `
        SELECT g.id, g.name, g.cost_multiplier, g.description,
               COUNT(a.id), g.created_at, g.updated_at
          FROM provider_groups g
          LEFT JOIN accounts a ON a.group_id = g.id
         WHERE g.id = $1
         GROUP BY g.id`
	var g ProviderGroup
	err := r.pool.QueryRow(ctx, q, id).Scan(&g.ID, &g.Name, &g.CostMultiplier,
		&g.Description, &g.AccountCount, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("query provider group %d: %w", id, err)
	}
	return &g, nil
}

// GroupInsertParams carries the columns needed to create a group.
type GroupInsertParams struct {
	Name           string
	CostMultiplier float64
	Description    string
}

// Insert creates a group and returns its id. Duplicate names are
// mapped to ErrGroupNameTaken.
func (r *ProviderGroupRepo) Insert(ctx context.Context, p GroupInsertParams) (int64, error) {
	const q = `
        INSERT INTO provider_groups (name, cost_multiplier, description)
        VALUES ($1, $2, $3)
        RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, q, p.Name, p.CostMultiplier, p.Description).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrGroupNameTaken
		}
		return 0, fmt.Errorf("insert provider group %q: %w", p.Name, err)
	}
	return id, nil
}

// GroupUpdateParams is a full replace of a group's mutable columns.
type GroupUpdateParams struct {
	Name           string
	CostMultiplier float64
	Description    string
}

// Update replaces the mutable columns of the group identified by id.
func (r *ProviderGroupRepo) Update(ctx context.Context, id int64, p GroupUpdateParams) error {
	const q = `
        UPDATE provider_groups
           SET name = $1, cost_multiplier = $2, description = $3, updated_at = NOW()
         WHERE id = $4`
	tag, err := r.pool.Exec(ctx, q, p.Name, p.CostMultiplier, p.Description, id)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrGroupNameTaken
		}
		return fmt.Errorf("update provider group %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGroupNotFound
	}
	return nil
}

// Delete removes a group. Accounts referencing it are un-grouped by the
// ON DELETE SET NULL foreign key.
func (r *ProviderGroupRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM provider_groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete provider group %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGroupNotFound
	}
	return nil
}
