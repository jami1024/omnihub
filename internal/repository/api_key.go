package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

// ApiKeyRepo persists virtual API keys. Only the sha256 hex hash of
// the cleartext is stored — the cleartext is surfaced exactly once
// when the CLI generates a key.
type ApiKeyRepo struct {
	pool *pgxpool.Pool
}

// NewApiKeyRepo wires the repository onto an existing pgx pool.
func NewApiKeyRepo(pool *pgxpool.Pool) *ApiKeyRepo {
	return &ApiKeyRepo{pool: pool}
}

// ErrApiKeyNotFound is returned when a single-row lookup misses.
var ErrApiKeyNotFound = errors.New("api key not found")

// ListEnabled returns every enabled key ready for the in-memory
// pool. Disabled keys are excluded so the auth guard cannot
// accidentally authenticate someone whose key was revoked.
func (r *ApiKeyRepo) ListEnabled(ctx context.Context) ([]*apikey.Key, error) {
	const q = `
        SELECT id, name, key_hash, COALESCE(label, ''),
               daily_usd_limit, rpm_limit, allowed_models
          FROM api_keys
         WHERE enabled = TRUE
         ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query api_keys: %w", err)
	}
	defer rows.Close()

	var out []*apikey.Key
	for rows.Next() {
		k, err := scanApiKey(rows)
		if err != nil {
			return nil, err
		}
		k.Enabled = true
		out = append(out, k)
	}
	return out, rows.Err()
}

// ListAll returns every row (including disabled) for the CLI's
// `key list` command.
func (r *ApiKeyRepo) ListAll(ctx context.Context) ([]*apikey.Key, error) {
	const q = `
        SELECT id, name, key_hash, COALESCE(label, ''), enabled,
               daily_usd_limit, rpm_limit, allowed_models
          FROM api_keys
         ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query api_keys: %w", err)
	}
	defer rows.Close()

	var out []*apikey.Key
	for rows.Next() {
		var (
			k             apikey.Key
			enabled       bool
			allowedJSON   []byte
			dailyLimit    *float64
			rpmLimit      *int
		)
		if err := rows.Scan(
			&k.ID, &k.Name, &k.Hash, &k.Label, &enabled,
			&dailyLimit, &rpmLimit, &allowedJSON,
		); err != nil {
			return nil, fmt.Errorf("scan api_key: %w", err)
		}
		k.Enabled = enabled
		k.DailyUSDLimit = dailyLimit
		k.RPMLimit = rpmLimit
		if err := decodeAllowedModels(&k, allowedJSON); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

// CountAll counts every row regardless of enabled flag. Used by the
// bootstrap path to detect a fresh deployment.
func (r *ApiKeyRepo) CountAll(ctx context.Context) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ApiKeyInsertParams carries everything Insert needs.
type ApiKeyInsertParams struct {
	Name          string
	Hash          string
	Label         string
	Enabled       bool
	DailyUSDLimit *float64
	RPMLimit      *int
	AllowedModels []string
}

// Insert creates a new api_keys row.
func (r *ApiKeyRepo) Insert(ctx context.Context, p ApiKeyInsertParams) (int64, error) {
	var allowedJSON []byte
	if len(p.AllowedModels) > 0 {
		b, err := json.Marshal(p.AllowedModels)
		if err != nil {
			return 0, fmt.Errorf("encode allowed_models: %w", err)
		}
		allowedJSON = b
	}

	const q = `
        INSERT INTO api_keys (
            name, key_hash, label, enabled,
            daily_usd_limit, rpm_limit, allowed_models
        )
        VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7)
        RETURNING id`

	var id int64
	err := r.pool.QueryRow(ctx, q,
		p.Name, p.Hash, p.Label, p.Enabled,
		p.DailyUSDLimit, p.RPMLimit, allowedJSON,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrApiKeyNotFound
		}
		return 0, fmt.Errorf("insert api_key %q: %w", p.Name, err)
	}
	return id, nil
}

// SetEnabled toggles the enabled flag on the named key.
func (r *ApiKeyRepo) SetEnabled(ctx context.Context, name string, enabled bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET enabled = $1, updated_at = NOW() WHERE name = $2`,
		enabled, name)
	if err != nil {
		return fmt.Errorf("update enabled for %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrApiKeyNotFound
	}
	return nil
}

// Delete hard-deletes a key by name.
func (r *ApiKeyRepo) Delete(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM api_keys WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete api_key %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrApiKeyNotFound
	}
	return nil
}

// scanApiKey reads the ListEnabled column projection.
func scanApiKey(rows interface{ Scan(...any) error }) (*apikey.Key, error) {
	var (
		k           apikey.Key
		allowedJSON []byte
		dailyLimit  *float64
		rpmLimit    *int
	)
	if err := rows.Scan(
		&k.ID, &k.Name, &k.Hash, &k.Label,
		&dailyLimit, &rpmLimit, &allowedJSON,
	); err != nil {
		return nil, fmt.Errorf("scan api_key: %w", err)
	}
	k.DailyUSDLimit = dailyLimit
	k.RPMLimit = rpmLimit
	if err := decodeAllowedModels(&k, allowedJSON); err != nil {
		return nil, err
	}
	return &k, nil
}

func decodeAllowedModels(k *apikey.Key, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &k.AllowedModels); err != nil {
		return fmt.Errorf("decode allowed_models for %q: %w", k.Name, err)
	}
	return nil
}
