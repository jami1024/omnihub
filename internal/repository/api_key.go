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

// ErrApiKeyNameTaken is returned when an insert or rename collides with
// the UNIQUE(name) constraint. The admin API maps it to a 409 so the UI
// can flag the field. (The key_hash UNIQUE constraint can also raise
// 23505, but admin-created keys are generated from 32 random bytes, so
// a hash collision is not a realistic outcome of normal use.)
var ErrApiKeyNameTaken = errors.New("api key name already in use")

// ListEnabled returns every enabled key ready for the in-memory
// pool. Disabled keys are excluded so the auth guard cannot
// accidentally authenticate someone whose key was revoked.
func (r *ApiKeyRepo) ListEnabled(ctx context.Context) ([]*apikey.Key, error) {
	const q = `
        SELECT k.id, k.name, k.key_hash, COALESCE(k.label, ''),
               k.daily_usd_limit, k.rpm_limit, k.allowed_models, k.user_id,
               COALESCE(u.price_ratio, 1.0)::float8, k.billing_mode
          FROM api_keys k
          LEFT JOIN users u ON u.id = k.user_id
         WHERE k.enabled = TRUE
         ORDER BY k.id ASC`
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
               daily_usd_limit, rpm_limit, allowed_models, billing_mode
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
			k           apikey.Key
			enabled     bool
			allowedJSON []byte
			dailyLimit  *float64
			rpmLimit    *int
			billingMode string
		)
		if err := rows.Scan(
			&k.ID, &k.Name, &k.Hash, &k.Label, &enabled,
			&dailyLimit, &rpmLimit, &allowedJSON, &billingMode,
		); err != nil {
			return nil, fmt.Errorf("scan api_key: %w", err)
		}
		k.Enabled = enabled
		k.DailyUSDLimit = dailyLimit
		k.RPMLimit = rpmLimit
		k.BillingMode = apikey.BillingMode(billingMode)
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

// ApiKeyInsertParams carries everything Insert needs. UserID is the
// owning portal user; nil means an admin/system key (no portal owner).
type ApiKeyInsertParams struct {
	Name          string
	Hash          string
	Label         string
	Enabled       bool
	DailyUSDLimit *float64
	RPMLimit      *int
	AllowedModels []string
	UserID        *int64
	BillingMode   apikey.BillingMode // "" defaults to payg at the DB layer
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
            daily_usd_limit, rpm_limit, allowed_models, user_id, billing_mode
        )
        VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8, COALESCE(NULLIF($9, ''), 'payg'))
        RETURNING id`

	var id int64
	err := r.pool.QueryRow(ctx, q,
		p.Name, p.Hash, p.Label, p.Enabled,
		p.DailyUSDLimit, p.RPMLimit, allowedJSON, p.UserID, string(p.BillingMode),
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrApiKeyNameTaken
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrApiKeyNotFound
		}
		return 0, fmt.Errorf("insert api_key %q: %w", p.Name, err)
	}
	return id, nil
}

// ListByUser returns every key owned by a portal user, newest id first.
func (r *ApiKeyRepo) ListByUser(ctx context.Context, userID int64) ([]*apikey.Key, error) {
	const q = `
        SELECT id, name, key_hash, COALESCE(label, ''), enabled,
               daily_usd_limit, rpm_limit, allowed_models, billing_mode
          FROM api_keys
         WHERE user_id = $1
         ORDER BY id DESC`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("query api_keys for user %d: %w", userID, err)
	}
	defer rows.Close()
	var out []*apikey.Key
	for rows.Next() {
		var (
			k           apikey.Key
			enabled     bool
			allowedJSON []byte
			dailyLimit  *float64
			rpmLimit    *int
			billingMode string
		)
		if err := rows.Scan(&k.ID, &k.Name, &k.Hash, &k.Label, &enabled,
			&dailyLimit, &rpmLimit, &allowedJSON, &billingMode); err != nil {
			return nil, fmt.Errorf("scan api_key: %w", err)
		}
		k.Enabled, k.DailyUSDLimit, k.RPMLimit = enabled, dailyLimit, rpmLimit
		k.BillingMode = apikey.BillingMode(billingMode)
		if err := decodeAllowedModels(&k, allowedJSON); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

// DeleteByIDOwnedBy hard-deletes a key only when it belongs to userID,
// so one portal user can never delete another's key. Returns
// ErrApiKeyNotFound when the id doesn't exist or isn't theirs.
func (r *ApiKeyRepo) DeleteByIDOwnedBy(ctx context.Context, id, userID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete api_key %d for user %d: %w", id, userID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrApiKeyNotFound
	}
	return nil
}

// GetByID fetches a single key by primary key, with its enabled flag and
// metadata. The cleartext value and hash are not part of the admin
// surface, so callers project only what the UI may see.
func (r *ApiKeyRepo) GetByID(ctx context.Context, id int64) (*apikey.Key, error) {
	const q = `
        SELECT id, name, key_hash, COALESCE(label, ''), enabled,
               daily_usd_limit, rpm_limit, allowed_models, billing_mode
          FROM api_keys
         WHERE id = $1`
	var (
		k           apikey.Key
		enabled     bool
		allowedJSON []byte
		dailyLimit  *float64
		rpmLimit    *int
		billingMode string
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&k.ID, &k.Name, &k.Hash, &k.Label, &enabled,
		&dailyLimit, &rpmLimit, &allowedJSON, &billingMode,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("query api_key %d: %w", id, err)
	}
	k.Enabled = enabled
	k.DailyUSDLimit = dailyLimit
	k.RPMLimit = rpmLimit
	k.BillingMode = apikey.BillingMode(billingMode)
	if err := decodeAllowedModels(&k, allowedJSON); err != nil {
		return nil, err
	}
	return &k, nil
}

// ApiKeyUpdateParams carries the mutable metadata of a key. The key
// value (and thus key_hash) is deliberately immutable — rotating a
// secret is a delete + create, never an in-place edit — so it is absent
// here. Nil limit pointers write NULL ("no limit"); a nil/empty
// AllowedModels writes NULL ("all models").
type ApiKeyUpdateParams struct {
	Name          string
	Label         string
	Enabled       bool
	DailyUSDLimit *float64
	RPMLimit      *int
	AllowedModels []string
	BillingMode   apikey.BillingMode // "" defaults to payg at the DB layer
}

// UpdateMeta replaces the mutable columns of the key identified by id.
// Returns ErrApiKeyNotFound when no row matches and ErrApiKeyNameTaken
// when the new name collides with another key.
func (r *ApiKeyRepo) UpdateMeta(ctx context.Context, id int64, p ApiKeyUpdateParams) error {
	var allowedJSON []byte
	if len(p.AllowedModels) > 0 {
		b, err := json.Marshal(p.AllowedModels)
		if err != nil {
			return fmt.Errorf("encode allowed_models: %w", err)
		}
		allowedJSON = b
	}

	const q = `
        UPDATE api_keys SET
            name = $1, label = NULLIF($2, ''), enabled = $3,
            daily_usd_limit = $4, rpm_limit = $5, allowed_models = $6,
            billing_mode = COALESCE(NULLIF($7, ''), 'payg'),
            updated_at = NOW()
         WHERE id = $8`

	tag, err := r.pool.Exec(ctx, q,
		p.Name, p.Label, p.Enabled,
		p.DailyUSDLimit, p.RPMLimit, allowedJSON,
		string(p.BillingMode), id,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrApiKeyNameTaken
		}
		return fmt.Errorf("update api_key %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrApiKeyNotFound
	}
	return nil
}

// DeleteByID hard-deletes the key with the given primary key. The admin
// API addresses keys by id (stable across renames); the CLI's
// Delete-by-name stays for backward compatibility.
func (r *ApiKeyRepo) DeleteByID(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete api_key %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrApiKeyNotFound
	}
	return nil
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
		userID      *int64
		priceRatio  float64
		billingMode string
	)
	if err := rows.Scan(
		&k.ID, &k.Name, &k.Hash, &k.Label,
		&dailyLimit, &rpmLimit, &allowedJSON, &userID, &priceRatio, &billingMode,
	); err != nil {
		return nil, fmt.Errorf("scan api_key: %w", err)
	}
	k.DailyUSDLimit = dailyLimit
	k.RPMLimit = rpmLimit
	k.UserID = userID
	k.PriceRatio = priceRatio
	k.BillingMode = apikey.BillingMode(billingMode)
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
