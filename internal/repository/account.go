package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// AccountRepo persists upstream provider accounts.
//
// Credentials are stored as cleartext JSONB in this MVP. Operators
// must protect the database accordingly (network isolation, OS-level
// disk encryption, restricted role grants). A future commit will add
// envelope encryption on the credentials column once the admin API
// lands and accounts can be edited by non-DBA users.
type AccountRepo struct {
	pool *pgxpool.Pool
}

// NewAccountRepo wires the repository onto an existing pgx pool.
func NewAccountRepo(pool *pgxpool.Pool) *AccountRepo {
	return &AccountRepo{pool: pool}
}

// ErrAccountNotFound is returned when a single-row lookup misses.
var ErrAccountNotFound = errors.New("account not found")

// ErrAccountNameTaken is returned when an insert or rename collides
// with the UNIQUE(name) constraint. Callers map it to a 409 so the
// admin UI can flag the field rather than surfacing a raw DB error.
var ErrAccountNameTaken = errors.New("account name already in use")

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ListEnabled returns every row with enabled = TRUE. Disabled accounts
// are excluded so the resolver sees only routable upstreams.
func (r *AccountRepo) ListEnabled(ctx context.Context) ([]*provider.Account, error) {
	const q = `
        SELECT id, name, provider, weight, priority, cost_multiplier,
               COALESCE(base_url, ''), credentials,
               circuit_failure_threshold, circuit_open_duration_ms, circuit_half_open_success,
               model_redirects, daily_usd_limit, total_usd_limit
          FROM accounts
         WHERE enabled = TRUE
         ORDER BY priority ASC, id ASC`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query accounts: %w", err)
	}
	defer rows.Close()

	var out []*provider.Account
	for rows.Next() {
		var (
			a                provider.Account
			credentialsRaw   []byte
			multiplier       float64
			failureThreshold *int
			openDurationMs   *int64
			halfOpenSuccess  *int
			redirectsRaw     []byte
		)
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Provider, &a.Weight, &a.Priority, &multiplier,
			&a.BaseURL, &credentialsRaw,
			&failureThreshold, &openDurationMs, &halfOpenSuccess,
			&redirectsRaw, &a.DailyUSDLimit, &a.TotalUSDLimit,
		); err != nil {
			return nil, fmt.Errorf("scan account row: %w", err)
		}
		a.CostMultiplier = multiplier
		applyCircuitOverrides(&a, failureThreshold, openDurationMs, halfOpenSuccess)
		if err := decodeCredentials(&a, credentialsRaw); err != nil {
			return nil, err
		}
		if err := decodeRedirects(&a, redirectsRaw); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// applyCircuitOverrides materialises the three nullable per-account
// circuit columns onto the Account struct. The DB stores duration as
// milliseconds (BIGINT); we convert to time.Duration here so callers
// never see the encoding detail.
func applyCircuitOverrides(a *provider.Account, failureThreshold *int, openDurationMs *int64, halfOpenSuccess *int) {
	a.CircuitFailureThreshold = failureThreshold
	a.CircuitHalfOpenSuccess = halfOpenSuccess
	if openDurationMs != nil {
		d := time.Duration(*openDurationMs) * time.Millisecond
		a.CircuitOpenDuration = &d
	}
}

func decodeCredentials(a *provider.Account, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.Credentials); err != nil {
		return fmt.Errorf("decode credentials for account %q: %w", a.Name, err)
	}
	return nil
}

// decodeRedirects unmarshals the model_redirects JSONB array onto the
// account. An empty / "[]" payload leaves ModelRedirects nil.
func decodeRedirects(a *provider.Account, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.ModelRedirects); err != nil {
		return fmt.Errorf("decode model_redirects for account %q: %w", a.Name, err)
	}
	return nil
}

// CountAll returns the total number of rows regardless of enabled
// flag. Used by the bootstrap path to decide whether to seed from
// environment variables on first boot.
func (r *AccountRepo) CountAll(ctx context.Context) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// InsertParams carries every column needed to create an account row.
// Defaults the caller wants applied (weight, enabled, multiplier)
// must be set explicitly; the repository performs no defaulting so
// the SQL columns and Go fields stay in 1:1 correspondence.
type InsertParams struct {
	Name           string
	Provider       string
	Enabled        bool
	Weight         int
	Priority       int
	CostMultiplier float64
	BaseURL        string
	Credentials    map[string]string

	// Per-account circuit-breaker overrides. Nil writes NULL (the
	// account uses the env-driven global default).
	CircuitFailureThreshold *int
	CircuitOpenDuration     *time.Duration
	CircuitHalfOpenSuccess  *int

	// Routing extras. ModelRedirects nil/empty writes '[]'; the USD
	// limits nil writes NULL ("no cap").
	ModelRedirects []provider.ModelRedirect
	DailyUSDLimit  *float64
	TotalUSDLimit  *float64
}

// marshalRedirects encodes a redirect rule set for the model_redirects
// JSONB column, defaulting nil/empty to "[]" (never SQL NULL — the
// column is NOT NULL).
func marshalRedirects(rules []provider.ModelRedirect) ([]byte, error) {
	if len(rules) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(rules)
}

// ListAll returns every row regardless of enabled flag, ordered by id.
// Used by the CLI list command and admin views.
func (r *AccountRepo) ListAll(ctx context.Context) ([]*provider.Account, []bool, error) {
	const q = `
        SELECT id, name, provider, enabled, weight, priority, cost_multiplier,
               COALESCE(base_url, ''), credentials,
               circuit_failure_threshold, circuit_open_duration_ms, circuit_half_open_success,
               model_redirects, daily_usd_limit, total_usd_limit
          FROM accounts
         ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, nil, fmt.Errorf("query accounts: %w", err)
	}
	defer rows.Close()

	var (
		accounts []*provider.Account
		flags    []bool
	)
	for rows.Next() {
		var (
			a                provider.Account
			enabled          bool
			credentialsRaw   []byte
			multiplier       float64
			failureThreshold *int
			openDurationMs   *int64
			halfOpenSuccess  *int
			redirectsRaw     []byte
		)
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Provider, &enabled,
			&a.Weight, &a.Priority, &multiplier,
			&a.BaseURL, &credentialsRaw,
			&failureThreshold, &openDurationMs, &halfOpenSuccess,
			&redirectsRaw, &a.DailyUSDLimit, &a.TotalUSDLimit,
		); err != nil {
			return nil, nil, fmt.Errorf("scan account row: %w", err)
		}
		a.CostMultiplier = multiplier
		applyCircuitOverrides(&a, failureThreshold, openDurationMs, halfOpenSuccess)
		if err := decodeCredentials(&a, credentialsRaw); err != nil {
			return nil, nil, err
		}
		if err := decodeRedirects(&a, redirectsRaw); err != nil {
			return nil, nil, err
		}
		accounts = append(accounts, &a)
		flags = append(flags, enabled)
	}
	return accounts, flags, rows.Err()
}

// SetEnabled flips the enabled flag for the named account. Returns
// ErrAccountNotFound when no row matches.
func (r *AccountRepo) SetEnabled(ctx context.Context, name string, enabled bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE accounts SET enabled = $1, updated_at = NOW() WHERE name = $2`,
		enabled, name)
	if err != nil {
		return fmt.Errorf("update enabled for %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// Delete hard-deletes the account row by name. Returns
// ErrAccountNotFound when no row matches.
func (r *AccountRepo) Delete(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM accounts WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete account %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// Insert creates a new account row and returns its id. Duplicate
// names are rejected by the UNIQUE constraint.
func (r *AccountRepo) Insert(ctx context.Context, p InsertParams) (int64, error) {
	credentialsJSON, err := json.Marshal(p.Credentials)
	if err != nil {
		return 0, fmt.Errorf("encode credentials: %w", err)
	}

	var openDurationMs *int64
	if p.CircuitOpenDuration != nil {
		ms := p.CircuitOpenDuration.Milliseconds()
		openDurationMs = &ms
	}

	redirectsJSON, err := marshalRedirects(p.ModelRedirects)
	if err != nil {
		return 0, fmt.Errorf("encode model_redirects: %w", err)
	}

	const q = `
        INSERT INTO accounts (
            name, provider, enabled, weight, priority,
            cost_multiplier, base_url, credentials,
            circuit_failure_threshold, circuit_open_duration_ms, circuit_half_open_success,
            model_redirects, daily_usd_limit, total_usd_limit
        )
        VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10, $11, $12, $13, $14)
        RETURNING id`

	var id int64
	err = r.pool.QueryRow(ctx, q,
		p.Name, p.Provider, p.Enabled, p.Weight, p.Priority,
		p.CostMultiplier, p.BaseURL, credentialsJSON,
		p.CircuitFailureThreshold, openDurationMs, p.CircuitHalfOpenSuccess,
		redirectsJSON, p.DailyUSDLimit, p.TotalUSDLimit,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrAccountNameTaken
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrAccountNotFound
		}
		return 0, fmt.Errorf("insert account %q: %w", p.Name, err)
	}
	return id, nil
}

// GetByID fetches a single account by primary key, returning the row,
// its enabled flag, and ErrAccountNotFound when no row matches.
func (r *AccountRepo) GetByID(ctx context.Context, id int64) (*provider.Account, bool, error) {
	const q = `
        SELECT id, name, provider, enabled, weight, priority, cost_multiplier,
               COALESCE(base_url, ''), credentials,
               circuit_failure_threshold, circuit_open_duration_ms, circuit_half_open_success,
               model_redirects, daily_usd_limit, total_usd_limit
          FROM accounts
         WHERE id = $1`
	var (
		a                provider.Account
		enabled          bool
		credentialsRaw   []byte
		multiplier       float64
		failureThreshold *int
		openDurationMs   *int64
		halfOpenSuccess  *int
		redirectsRaw     []byte
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.Name, &a.Provider, &enabled,
		&a.Weight, &a.Priority, &multiplier,
		&a.BaseURL, &credentialsRaw,
		&failureThreshold, &openDurationMs, &halfOpenSuccess,
		&redirectsRaw, &a.DailyUSDLimit, &a.TotalUSDLimit,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrAccountNotFound
		}
		return nil, false, fmt.Errorf("query account %d: %w", id, err)
	}
	a.CostMultiplier = multiplier
	applyCircuitOverrides(&a, failureThreshold, openDurationMs, halfOpenSuccess)
	if err := decodeCredentials(&a, credentialsRaw); err != nil {
		return nil, false, err
	}
	if err := decodeRedirects(&a, redirectsRaw); err != nil {
		return nil, false, err
	}
	return &a, enabled, nil
}

// UpdateParams carries the full set of mutable account columns. The
// admin API submits every metadata field (a PUT-style replace), so the
// repository performs no per-field defaulting. Credentials are the one
// exception: a nil map leaves the stored credentials untouched (the
// admin UI never reads secrets back, so an edit that doesn't re-enter
// them must not wipe them). Circuit overrides follow the Insert rule —
// nil writes NULL ("use the env-driven global default").
type UpdateParams struct {
	Name           string
	Provider       string
	Enabled        bool
	Weight         int
	Priority       int
	CostMultiplier float64
	BaseURL        string
	Credentials    map[string]string

	CircuitFailureThreshold *int
	CircuitOpenDuration     *time.Duration
	CircuitHalfOpenSuccess  *int

	// Routing extras (PUT-style replace, same as the other columns).
	ModelRedirects []provider.ModelRedirect
	DailyUSDLimit  *float64
	TotalUSDLimit  *float64
}

// Update replaces the mutable columns of the account identified by id.
// Returns ErrAccountNotFound when no row matches and ErrAccountNameTaken
// when the new name collides with another account.
func (r *AccountRepo) Update(ctx context.Context, id int64, p UpdateParams) error {
	// nil credentials → pass SQL NULL so COALESCE keeps the existing
	// JSONB. A non-nil (possibly empty) map is marshalled and replaces it.
	var credentialsJSON []byte
	if p.Credentials != nil {
		b, err := json.Marshal(p.Credentials)
		if err != nil {
			return fmt.Errorf("encode credentials: %w", err)
		}
		credentialsJSON = b
	}

	var openDurationMs *int64
	if p.CircuitOpenDuration != nil {
		ms := p.CircuitOpenDuration.Milliseconds()
		openDurationMs = &ms
	}

	redirectsJSON, err := marshalRedirects(p.ModelRedirects)
	if err != nil {
		return fmt.Errorf("encode model_redirects: %w", err)
	}

	const q = `
        UPDATE accounts SET
            name = $1, provider = $2, enabled = $3, weight = $4, priority = $5,
            cost_multiplier = $6, base_url = NULLIF($7, ''),
            credentials = COALESCE($8, credentials),
            circuit_failure_threshold = $9, circuit_open_duration_ms = $10,
            circuit_half_open_success = $11,
            model_redirects = $12, daily_usd_limit = $13, total_usd_limit = $14,
            updated_at = NOW()
         WHERE id = $15`

	tag, err := r.pool.Exec(ctx, q,
		p.Name, p.Provider, p.Enabled, p.Weight, p.Priority,
		p.CostMultiplier, p.BaseURL, credentialsJSON,
		p.CircuitFailureThreshold, openDurationMs, p.CircuitHalfOpenSuccess,
		redirectsJSON, p.DailyUSDLimit, p.TotalUSDLimit,
		id,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAccountNameTaken
		}
		return fmt.Errorf("update account %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// DeleteByID hard-deletes the account with the given primary key. The
// admin API addresses accounts by id (a stable handle that survives
// renames); the CLI's Delete-by-name stays for backward compatibility.
func (r *AccountRepo) DeleteByID(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete account %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}
