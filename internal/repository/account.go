package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/crypto"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// AccountRepo persists upstream provider accounts.
//
// Sensitive fields (credential VALUES, custom-header VALUES, and the
// proxy URL) are encrypted at rest with the injected cipher when an
// OMNIHUB_ENCRYPTION_KEY is configured. A nil/disabled cipher keeps the
// previous plaintext behaviour; legacy plaintext rows are read
// transparently and migrated by ReencryptSecrets at boot.
type AccountRepo struct {
	pool   *pgxpool.Pool
	cipher *crypto.Cipher
}

// NewAccountRepo wires the repository onto an existing pgx pool. cipher
// may be nil (encryption disabled / passthrough).
func NewAccountRepo(pool *pgxpool.Pool, cipher *crypto.Cipher) *AccountRepo {
	return &AccountRepo{pool: pool, cipher: cipher}
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

// isForeignKeyViolation reports whether err is a Postgres
// foreign-key-constraint violation (SQLSTATE 23503).
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// ListEnabled returns every row with enabled = TRUE. Disabled accounts
// are excluded so the resolver sees only routable upstreams.
func (r *AccountRepo) ListEnabled(ctx context.Context) ([]*provider.Account, error) {
	const q = `
        SELECT a.id, a.name, a.provider, a.weight, a.priority, a.cost_multiplier,
               COALESCE(a.base_url, ''), a.credentials,
               a.circuit_failure_threshold, a.circuit_open_duration_ms, a.circuit_half_open_success,
               a.model_redirects, a.daily_usd_limit, a.total_usd_limit,
               a.group_id, COALESCE(g.name, ''), COALESCE(g.cost_multiplier, 1)::float8,
               a.custom_headers, a.endpoints, a.health_probe_enabled,
               COALESCE(a.proxy_url, ''), a.param_overrides,
               a.active_windows, COALESCE(a.active_timezone, ''), a.forward_client_ip,
               a.auth_type, COALESCE(a.auth_plugin, ''), a.auth_status,
               COALESCE(a.auth_subject, ''), COALESCE(a.auth_email, ''), COALESCE(a.auth_plan, ''),
               a.auth_expires_at, a.last_refresh_at, COALESCE(a.refresh_error, ''),
               COALESCE(a.client_profile, ''), a.client_profile_config,
               a.max_concurrency, COALESCE(g.routing_policy, ''),
               a.proxy_id, a.allowed_models
          FROM accounts a
          LEFT JOIN provider_groups g ON a.group_id = g.id
         WHERE a.enabled = TRUE
         ORDER BY a.priority ASC, a.id ASC`

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
			groupMultiplier  float64
			headersRaw       []byte
			endpointsRaw     []byte

			paramsRaw        []byte
			windowsRaw       []byte
			profileConfigRaw []byte
			allowedModelsRaw []byte
		)
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Provider, &a.Weight, &a.Priority, &multiplier,
			&a.BaseURL, &credentialsRaw,
			&failureThreshold, &openDurationMs, &halfOpenSuccess,
			&redirectsRaw, &a.DailyUSDLimit, &a.TotalUSDLimit,
			&a.GroupID, &a.GroupName, &groupMultiplier,
			&headersRaw, &endpointsRaw, &a.HealthProbeEnabled, &a.ProxyURL, &paramsRaw, &windowsRaw, &a.ActiveTimezone, &a.ForwardClientIP,
			&a.AuthType, &a.AuthPlugin, &a.AuthStatus, &a.AuthSubject, &a.AuthEmail, &a.AuthPlan,
			&a.AuthExpiresAt, &a.LastRefreshAt, &a.RefreshError, &a.ClientProfile, &profileConfigRaw, &a.MaxConcurrency, &a.GroupRoutingPolicy, &a.ProxyID, &allowedModelsRaw,
		); err != nil {
			return nil, fmt.Errorf("scan account row: %w", err)
		}
		a.CostMultiplier = multiplier
		a.GroupCostMultiplier = groupMultiplier
		applyCircuitOverrides(&a, failureThreshold, openDurationMs, halfOpenSuccess)
		// Decode + decrypt. A failure here (e.g. an account whose secrets
		// can't be decrypted because the key is wrong/missing) must NOT
		// brick the whole routing pool — skip the unreadable account and
		// keep the rest serving. The resolver simply sees fewer upstreams.
		if err := r.decodeRow(&a, credentialsRaw, redirectsRaw, headersRaw, endpointsRaw, paramsRaw, windowsRaw, profileConfigRaw, allowedModelsRaw); err != nil {
			slog.Warn("skipping unreadable account in routing pool",
				"account", a.Name, "id", a.ID, "err", err.Error())
			continue
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// decodeRow runs every decode/decrypt step for one scanned account row.
// Returns the first error; callers decide whether to skip (routing pool)
// or surface it (single-row lookups).
func (r *AccountRepo) decodeRow(a *provider.Account, credentialsRaw, redirectsRaw, headersRaw, endpointsRaw, paramsRaw, windowsRaw, profileConfigRaw, allowedModelsRaw []byte) error {
	if err := decodeCredentials(a, credentialsRaw, r.cipher); err != nil {
		return err
	}
	if err := decodeRedirects(a, redirectsRaw); err != nil {
		return err
	}
	if err := decodeAccountAllowedModels(a, allowedModelsRaw); err != nil {
		return err
	}
	if err := decodeHeaders(a, headersRaw, r.cipher); err != nil {
		return err
	}
	if err := decodeEndpoints(a, endpointsRaw); err != nil {
		return err
	}
	if err := decodeParams(a, paramsRaw); err != nil {
		return err
	}
	if err := decodeWindows(a, windowsRaw); err != nil {
		return err
	}
	if err := decodeProfileConfig(a, profileConfigRaw); err != nil {
		return err
	}
	return decodeProxy(a, r.cipher)
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

func decodeCredentials(a *provider.Account, raw []byte, c *crypto.Cipher) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.Credentials); err != nil {
		return fmt.Errorf("decode credentials for account %q: %w", a.Name, err)
	}
	dec, err := c.DecryptMapValues(a.Credentials)
	if err != nil {
		return fmt.Errorf("decrypt credentials for account %q: %w", a.Name, err)
	}
	a.Credentials = dec
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

// decodeHeaders unmarshals the custom_headers JSONB object onto the
// account. An empty / "{}" payload leaves CustomHeaders nil.
func decodeHeaders(a *provider.Account, raw []byte, c *crypto.Cipher) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.CustomHeaders); err != nil {
		return fmt.Errorf("decode custom_headers for account %q: %w", a.Name, err)
	}
	dec, err := c.DecryptMapValues(a.CustomHeaders)
	if err != nil {
		return fmt.Errorf("decrypt custom_headers for account %q: %w", a.Name, err)
	}
	a.CustomHeaders = dec
	return nil
}

// decodeProxy decrypts the scanned proxy_url in place. Empty / legacy
// plaintext values pass through.
func decodeProxy(a *provider.Account, c *crypto.Cipher) error {
	if a.ProxyURL == "" {
		return nil
	}
	v, err := c.DecryptString(a.ProxyURL)
	if err != nil {
		return fmt.Errorf("decrypt proxy_url for account %q: %w", a.Name, err)
	}
	a.ProxyURL = v
	return nil
}

// marshalHeaders encodes a header map for the custom_headers JSONB
// column, defaulting nil/empty to "{}" (the column is NOT NULL).
func marshalHeaders(h map[string]string) ([]byte, error) {
	if len(h) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(h)
}

// decodeEndpoints unmarshals the endpoints JSONB array onto the account.
// An empty / "[]" payload leaves Endpoints nil.
func decodeEndpoints(a *provider.Account, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.Endpoints); err != nil {
		return fmt.Errorf("decode endpoints for account %q: %w", a.Name, err)
	}
	return nil
}

// marshalEndpoints encodes the additional-endpoints list for the
// endpoints JSONB column, defaulting nil/empty to "[]" (NOT NULL).
func marshalEndpoints(e []string) ([]byte, error) {
	if len(e) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(e)
}

// decodeAccountAllowedModels unmarshals the allowed_models JSONB array
// onto the account. An empty / "[]" payload leaves AllowedModels nil (no
// restriction). Named to avoid colliding with the api-key allow-list
// decoder in the same package.
func decodeAccountAllowedModels(a *provider.Account, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.AllowedModels); err != nil {
		return fmt.Errorf("decode allowed_models for account %q: %w", a.Name, err)
	}
	return nil
}

// marshalAllowedModels encodes the per-account model allow-list for the
// allowed_models JSONB column, defaulting nil/empty to "[]" (NOT NULL).
func marshalAllowedModels(m []string) ([]byte, error) {
	if len(m) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(m)
}

// decodeParams unmarshals the param_overrides JSONB object onto the
// account. An empty / "{}" payload leaves every override nil.
func decodeParams(a *provider.Account, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.ParamOverrides); err != nil {
		return fmt.Errorf("decode param_overrides for account %q: %w", a.Name, err)
	}
	return nil
}

// marshalParams encodes the param overrides for the param_overrides
// JSONB column. A no-op override set serialises to "{}" (NOT NULL).
func marshalParams(p provider.ParamOverrides) ([]byte, error) {
	if !p.Any() {
		return []byte("{}"), nil
	}
	return json.Marshal(p)
}

// decodeWindows unmarshals the active_windows JSONB array onto the
// account. An empty / "[]" payload leaves ActiveWindows nil.
func decodeWindows(a *provider.Account, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.ActiveWindows); err != nil {
		return fmt.Errorf("decode active_windows for account %q: %w", a.Name, err)
	}
	return nil
}

// marshalWindows encodes the active-window list for the active_windows
// JSONB column, defaulting nil/empty to "[]" (NOT NULL).
func marshalWindows(w []provider.ActiveWindow) ([]byte, error) {
	if len(w) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(w)
}

// decodeProfileConfig unmarshals the client_profile_config JSONB object
// onto the account. An empty / "{}" payload leaves it nil. Values are
// opaque profile knobs (no secrets) so they are not encrypted.
func decodeProfileConfig(a *provider.Account, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.ClientProfileConfig); err != nil {
		return fmt.Errorf("decode client_profile_config for account %q: %w", a.Name, err)
	}
	return nil
}

// marshalProfileConfig encodes the client profile config for the
// client_profile_config JSONB column, defaulting nil/empty to "{}"
// (the column is NOT NULL).
func marshalProfileConfig(c map[string]any) ([]byte, error) {
	if len(c) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(c)
}

// AuthRuntimeUpdate carries the runtime-maintained auth columns the
// TokenManager / auth plugins write. It deliberately cannot touch the
// admin-configured columns (auth_type, client_profile, weight, ...) so
// a token refresh can never clobber an admin edit, and vice versa.
// Zero values mean "keep the stored value" except Status (always
// written) and RefreshError ("" clears the stored error).
type AuthRuntimeUpdate struct {
	Credentials   map[string]string // nil = keep stored secrets
	Plugin        string            // "" = keep (set on first import)
	Status        string
	RefreshError  string
	ExpiresAt     *time.Time
	LastRefreshAt *time.Time
	Subject       string
	Email         string
	Plan          string
}

// UpdateAuthRuntime applies a TokenManager / auth-plugin state write to
// one account. Credentials, when present, replace the whole stored set
// (token refresh rotates several fields at once) and are encrypted the
// same way Insert/Update encrypt them.
func (r *AccountRepo) UpdateAuthRuntime(ctx context.Context, id int64, u AuthRuntimeUpdate) error {
	var credentialsJSON []byte // nil → SQL NULL → COALESCE keeps stored value
	if u.Credentials != nil {
		enc, err := r.cipher.EncryptMapValues(u.Credentials)
		if err != nil {
			return fmt.Errorf("encrypt credentials: %w", err)
		}
		credentialsJSON, err = json.Marshal(enc)
		if err != nil {
			return fmt.Errorf("encode credentials: %w", err)
		}
	}

	const q = `
        UPDATE accounts SET
            credentials = COALESCE($2, credentials),
            auth_plugin = COALESCE(NULLIF($3, ''), auth_plugin),
            auth_status = $4,
            refresh_error = NULLIF($5, ''),
            auth_expires_at = COALESCE($6, auth_expires_at),
            last_refresh_at = COALESCE($7, last_refresh_at),
            auth_subject = COALESCE(NULLIF($8, ''), auth_subject),
            auth_email = COALESCE(NULLIF($9, ''), auth_email),
            auth_plan = COALESCE(NULLIF($10, ''), auth_plan),
            updated_at = NOW()
         WHERE id = $1`

	tag, err := r.pool.Exec(ctx, q, id, credentialsJSON, u.Plugin, u.Status, u.RefreshError,
		u.ExpiresAt, u.LastRefreshAt, u.Subject, u.Email, u.Plan)
	if err != nil {
		return fmt.Errorf("update auth runtime: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
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
	// limits nil writes NULL ("no cap"); GroupID nil writes NULL
	// (ungrouped).
	ModelRedirects     []provider.ModelRedirect
	DailyUSDLimit      *float64
	TotalUSDLimit      *float64
	GroupID            *int64
	CustomHeaders      map[string]string
	Endpoints          []string
	HealthProbeEnabled *bool
	ProxyURL           string
	ParamOverrides     provider.ParamOverrides
	ActiveWindows      []provider.ActiveWindow
	ActiveTimezone     string
	ForwardClientIP    bool

	// AllowedModels is the per-account model allow-list (migration 0039).
	// Nil/empty writes '[]' (no restriction).
	AllowedModels []string

	// Upstream auth model (migration 0036). Only the admin-configurable
	// columns are set here; the runtime-maintained ones (auth_status,
	// auth_subject/email/plan, auth_expires_at, last_refresh_at,
	// refresh_error) default in SQL and are written by the TokenManager.
	AuthType            string
	AuthPlugin          string
	ClientProfile       string
	ClientProfileConfig map[string]any

	// MaxConcurrency caps in-flight requests through the account
	// (migration 0037). 0 = unlimited.
	MaxConcurrency int

	// ProxyID binds the account to a proxies row (migration 0038).
	// Nil = no pool binding (falls back to the inline ProxyURL).
	ProxyID *int64
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
        SELECT a.id, a.name, a.provider, a.enabled, a.weight, a.priority, a.cost_multiplier,
               COALESCE(a.base_url, ''), a.credentials,
               a.circuit_failure_threshold, a.circuit_open_duration_ms, a.circuit_half_open_success,
               a.model_redirects, a.daily_usd_limit, a.total_usd_limit,
               a.group_id, COALESCE(g.name, ''), COALESCE(g.cost_multiplier, 1)::float8,
               a.custom_headers, a.endpoints, a.health_probe_enabled,
               COALESCE(a.proxy_url, ''), a.param_overrides,
               a.active_windows, COALESCE(a.active_timezone, ''), a.forward_client_ip,
               a.auth_type, COALESCE(a.auth_plugin, ''), a.auth_status,
               COALESCE(a.auth_subject, ''), COALESCE(a.auth_email, ''), COALESCE(a.auth_plan, ''),
               a.auth_expires_at, a.last_refresh_at, COALESCE(a.refresh_error, ''),
               COALESCE(a.client_profile, ''), a.client_profile_config,
               a.max_concurrency, COALESCE(g.routing_policy, ''),
               a.proxy_id, a.allowed_models
          FROM accounts a
          LEFT JOIN provider_groups g ON a.group_id = g.id
         ORDER BY a.id ASC`
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
			groupMultiplier  float64
			headersRaw       []byte
			endpointsRaw     []byte

			paramsRaw        []byte
			windowsRaw       []byte
			profileConfigRaw []byte
			allowedModelsRaw []byte
		)
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Provider, &enabled,
			&a.Weight, &a.Priority, &multiplier,
			&a.BaseURL, &credentialsRaw,
			&failureThreshold, &openDurationMs, &halfOpenSuccess,
			&redirectsRaw, &a.DailyUSDLimit, &a.TotalUSDLimit,
			&a.GroupID, &a.GroupName, &groupMultiplier,
			&headersRaw, &endpointsRaw, &a.HealthProbeEnabled, &a.ProxyURL, &paramsRaw, &windowsRaw, &a.ActiveTimezone, &a.ForwardClientIP,
			&a.AuthType, &a.AuthPlugin, &a.AuthStatus, &a.AuthSubject, &a.AuthEmail, &a.AuthPlan,
			&a.AuthExpiresAt, &a.LastRefreshAt, &a.RefreshError, &a.ClientProfile, &profileConfigRaw, &a.MaxConcurrency, &a.GroupRoutingPolicy, &a.ProxyID, &allowedModelsRaw,
		); err != nil {
			return nil, nil, fmt.Errorf("scan account row: %w", err)
		}
		a.CostMultiplier = multiplier
		a.GroupCostMultiplier = groupMultiplier
		applyCircuitOverrides(&a, failureThreshold, openDurationMs, halfOpenSuccess)
		if err := r.decodeRow(&a, credentialsRaw, redirectsRaw, headersRaw, endpointsRaw, paramsRaw, windowsRaw, profileConfigRaw, allowedModelsRaw); err != nil {
			slog.Warn("skipping unreadable account in list", "account", a.Name, "id", a.ID, "err", err.Error())
			continue
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

// ReencryptSecrets migrates any account rows that still hold legacy
// plaintext secrets (credentials, custom-header values, proxy URL) to
// encrypted form. It is idempotent — rows already fully encrypted are
// skipped — and a no-op when encryption is disabled. Returns the number
// of rows rewritten. Safe to run at every boot.
func (r *AccountRepo) ReencryptSecrets(ctx context.Context) (int, error) {
	if !r.cipher.Enabled() {
		return 0, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, credentials, COALESCE(proxy_url, ''), custom_headers FROM accounts`)
	if err != nil {
		return 0, fmt.Errorf("scan accounts for re-encrypt: %w", err)
	}
	type row struct {
		id      int64
		name    string
		creds   map[string]string
		proxy   string
		headers map[string]string
	}
	var todo []row
	for rows.Next() {
		var (
			id         int64
			name       string
			credsRaw   []byte
			proxy      string
			headersRaw []byte
			creds      = map[string]string{}
			headers    = map[string]string{}
		)
		if err := rows.Scan(&id, &name, &credsRaw, &proxy, &headersRaw); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan row for re-encrypt: %w", err)
		}
		// A row whose JSONB can't unmarshal into map[string]string is
		// malformed (out-of-band write); skip it rather than risk
		// overwriting it with an empty object.
		if len(credsRaw) > 0 {
			if err := json.Unmarshal(credsRaw, &creds); err != nil {
				slog.Warn("re-encrypt: skipping row with unreadable credentials", "id", id, "err", err.Error())
				continue
			}
		}
		if len(headersRaw) > 0 {
			if err := json.Unmarshal(headersRaw, &headers); err != nil {
				slog.Warn("re-encrypt: skipping row with unreadable custom_headers", "id", id, "err", err.Error())
				continue
			}
		}
		vals := []string{proxy}
		for _, v := range creds {
			vals = append(vals, v)
		}
		for _, v := range headers {
			vals = append(vals, v)
		}
		if crypto.HasLegacyPlaintext(vals...) {
			todo = append(todo, row{id: id, name: name, creds: creds, proxy: proxy, headers: headers})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	// reencryptRow returns the three encrypted column values, or an error
	// if any secret can't be decrypted. decrypt-then-encrypt is correct
	// for both legacy plaintext (passthrough decrypt) and already-
	// encrypted values within a mixed row.
	reencryptRow := func(t row) (credsJSON, headersJSON []byte, proxyEnc string, err error) {
		decCreds, err := r.cipher.DecryptMapValues(t.creds)
		if err != nil {
			return nil, nil, "", err
		}
		encCreds, err := r.cipher.EncryptMapValues(decCreds)
		if err != nil {
			return nil, nil, "", err
		}
		if credsJSON, err = json.Marshal(encCreds); err != nil {
			return nil, nil, "", err
		}
		decHeaders, err := r.cipher.DecryptMapValues(t.headers)
		if err != nil {
			return nil, nil, "", err
		}
		encHeaders, err := r.cipher.EncryptMapValues(decHeaders)
		if err != nil {
			return nil, nil, "", err
		}
		if headersJSON, err = marshalHeaders(encHeaders); err != nil {
			return nil, nil, "", err
		}
		decProxy, err := r.cipher.DecryptString(t.proxy)
		if err != nil {
			return nil, nil, "", err
		}
		if proxyEnc, err = r.cipher.EncryptString(decProxy); err != nil {
			return nil, nil, "", err
		}
		return credsJSON, headersJSON, proxyEnc, nil
	}

	n, skipped := 0, 0
	for _, t := range todo {
		credsJSON, headersJSON, proxyEnc, err := reencryptRow(t)
		if err != nil {
			// A row that can't be re-encrypted (e.g. a partially-migrated
			// row whose marked value fails GCM auth) is logged and left
			// untouched — never overwrite it with partial data, and never
			// abort the whole migration / boot over one bad row.
			slog.Warn("re-encrypt: skipping account whose secrets could not be re-encrypted",
				"account", t.name, "id", t.id, "err", err.Error())
			skipped++
			continue
		}
		if _, err := r.pool.Exec(ctx,
			`UPDATE accounts SET credentials = $1, custom_headers = $2, proxy_url = NULLIF($3, '') WHERE id = $4`,
			credsJSON, headersJSON, proxyEnc, t.id); err != nil {
			return n, fmt.Errorf("rewrite secrets id=%d: %w", t.id, err)
		}
		slog.Info("encrypted account secrets at rest", "account", t.name, "id", t.id)
		n++
	}
	if skipped > 0 {
		slog.Warn("re-encrypt finished with skipped rows", "migrated", n, "skipped", skipped)
	}
	return n, nil
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
	encCreds, err := r.cipher.EncryptMapValues(p.Credentials)
	if err != nil {
		return 0, fmt.Errorf("encrypt credentials: %w", err)
	}
	credentialsJSON, err := json.Marshal(encCreds)
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
	encHeaders, err := r.cipher.EncryptMapValues(p.CustomHeaders)
	if err != nil {
		return 0, fmt.Errorf("encrypt custom_headers: %w", err)
	}
	headersJSON, err := marshalHeaders(encHeaders)
	if err != nil {
		return 0, fmt.Errorf("encode custom_headers: %w", err)
	}
	proxyEnc, err := r.cipher.EncryptString(p.ProxyURL)
	if err != nil {
		return 0, fmt.Errorf("encrypt proxy_url: %w", err)
	}
	endpointsJSON, err := marshalEndpoints(p.Endpoints)
	if err != nil {
		return 0, fmt.Errorf("encode endpoints: %w", err)
	}
	paramsJSON, err := marshalParams(p.ParamOverrides)
	if err != nil {
		return 0, fmt.Errorf("encode param_overrides: %w", err)
	}
	windowsJSON, err := marshalWindows(p.ActiveWindows)
	if err != nil {
		return 0, fmt.Errorf("encode active_windows: %w", err)
	}
	profileConfigJSON, err := marshalProfileConfig(p.ClientProfileConfig)
	if err != nil {
		return 0, fmt.Errorf("encode client_profile_config: %w", err)
	}
	allowedModelsJSON, err := marshalAllowedModels(p.AllowedModels)
	if err != nil {
		return 0, fmt.Errorf("encode allowed_models: %w", err)
	}

	const q = `
        INSERT INTO accounts (
            name, provider, enabled, weight, priority,
            cost_multiplier, base_url, credentials,
            circuit_failure_threshold, circuit_open_duration_ms, circuit_half_open_success,
            model_redirects, daily_usd_limit, total_usd_limit, group_id, custom_headers, endpoints,
            health_probe_enabled, proxy_url, param_overrides, active_windows, active_timezone,
            forward_client_ip,
            auth_type, auth_plugin, client_profile, client_profile_config,
            max_concurrency, proxy_id, allowed_models
        )
        VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, NULLIF($19, ''), $20, $21, NULLIF($22, ''), $23,
                COALESCE(NULLIF($24, ''), 'api_key'), NULLIF($25, ''), NULLIF($26, ''), $27,
                $28, $29, $30)
        RETURNING id`

	var id int64
	err = r.pool.QueryRow(ctx, q,
		p.Name, p.Provider, p.Enabled, p.Weight, p.Priority,
		p.CostMultiplier, p.BaseURL, credentialsJSON,
		p.CircuitFailureThreshold, openDurationMs, p.CircuitHalfOpenSuccess,
		redirectsJSON, p.DailyUSDLimit, p.TotalUSDLimit, p.GroupID, headersJSON, endpointsJSON,
		p.HealthProbeEnabled, proxyEnc, paramsJSON, windowsJSON, p.ActiveTimezone, p.ForwardClientIP,
		p.AuthType, p.AuthPlugin, p.ClientProfile, profileConfigJSON,
		p.MaxConcurrency, p.ProxyID, allowedModelsJSON,
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
        SELECT a.id, a.name, a.provider, a.enabled, a.weight, a.priority, a.cost_multiplier,
               COALESCE(a.base_url, ''), a.credentials,
               a.circuit_failure_threshold, a.circuit_open_duration_ms, a.circuit_half_open_success,
               a.model_redirects, a.daily_usd_limit, a.total_usd_limit,
               a.group_id, COALESCE(g.name, ''), COALESCE(g.cost_multiplier, 1)::float8,
               a.custom_headers, a.endpoints, a.health_probe_enabled,
               COALESCE(a.proxy_url, ''), a.param_overrides,
               a.active_windows, COALESCE(a.active_timezone, ''), a.forward_client_ip,
               a.auth_type, COALESCE(a.auth_plugin, ''), a.auth_status,
               COALESCE(a.auth_subject, ''), COALESCE(a.auth_email, ''), COALESCE(a.auth_plan, ''),
               a.auth_expires_at, a.last_refresh_at, COALESCE(a.refresh_error, ''),
               COALESCE(a.client_profile, ''), a.client_profile_config,
               a.max_concurrency, COALESCE(g.routing_policy, ''),
               a.proxy_id, a.allowed_models
          FROM accounts a
          LEFT JOIN provider_groups g ON a.group_id = g.id
         WHERE a.id = $1`
	var (
		a                provider.Account
		enabled          bool
		credentialsRaw   []byte
		multiplier       float64
		failureThreshold *int
		openDurationMs   *int64
		halfOpenSuccess  *int
		redirectsRaw     []byte
		groupMultiplier  float64
		headersRaw       []byte
		endpointsRaw     []byte

		paramsRaw        []byte
		windowsRaw       []byte
		profileConfigRaw []byte
		allowedModelsRaw []byte
	)
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.Name, &a.Provider, &enabled,
		&a.Weight, &a.Priority, &multiplier,
		&a.BaseURL, &credentialsRaw,
		&failureThreshold, &openDurationMs, &halfOpenSuccess,
		&redirectsRaw, &a.DailyUSDLimit, &a.TotalUSDLimit,
		&a.GroupID, &a.GroupName, &groupMultiplier,
		&headersRaw, &endpointsRaw, &a.HealthProbeEnabled, &a.ProxyURL, &paramsRaw, &windowsRaw, &a.ActiveTimezone, &a.ForwardClientIP,
		&a.AuthType, &a.AuthPlugin, &a.AuthStatus, &a.AuthSubject, &a.AuthEmail, &a.AuthPlan,
		&a.AuthExpiresAt, &a.LastRefreshAt, &a.RefreshError, &a.ClientProfile, &profileConfigRaw, &a.MaxConcurrency, &a.GroupRoutingPolicy, &a.ProxyID, &allowedModelsRaw,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrAccountNotFound
		}
		return nil, false, fmt.Errorf("query account %d: %w", id, err)
	}
	a.CostMultiplier = multiplier
	a.GroupCostMultiplier = groupMultiplier
	applyCircuitOverrides(&a, failureThreshold, openDurationMs, halfOpenSuccess)
	if err := r.decodeRow(&a, credentialsRaw, redirectsRaw, headersRaw, endpointsRaw, paramsRaw, windowsRaw, profileConfigRaw, allowedModelsRaw); err != nil {
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
	ModelRedirects     []provider.ModelRedirect
	DailyUSDLimit      *float64
	TotalUSDLimit      *float64
	GroupID            *int64
	CustomHeaders      map[string]string
	Endpoints          []string
	HealthProbeEnabled *bool
	ProxyURL           string
	ParamOverrides     provider.ParamOverrides
	ActiveWindows      []provider.ActiveWindow
	ActiveTimezone     string
	ForwardClientIP    bool

	// AllowedModels is the per-account model allow-list (migration 0039).
	// PUT-style replace; nil/empty writes '[]' (no restriction).
	AllowedModels []string

	// Upstream auth model (migration 0036). PUT-style replace of the
	// admin-configurable columns only. The runtime-maintained columns
	// (auth_status, auth_subject/email/plan, auth_expires_at,
	// last_refresh_at, refresh_error) are deliberately NOT updated here so
	// an admin edit never clobbers live TokenManager state.
	AuthType            string
	AuthPlugin          string
	ClientProfile       string
	ClientProfileConfig map[string]any

	// MaxConcurrency caps in-flight requests through the account
	// (migration 0037). 0 = unlimited.
	MaxConcurrency int

	// ProxyID binds the account to a proxies row (migration 0038).
	// Nil = no pool binding (falls back to the inline ProxyURL).
	ProxyID *int64
}

// Update replaces the mutable columns of the account identified by id.
// Returns ErrAccountNotFound when no row matches and ErrAccountNameTaken
// when the new name collides with another account.
func (r *AccountRepo) Update(ctx context.Context, id int64, p UpdateParams) error {
	// nil credentials → pass SQL NULL so COALESCE keeps the existing
	// JSONB. A non-nil (possibly empty) map is marshalled and replaces it.
	var credentialsJSON []byte
	if p.Credentials != nil {
		encCreds, err := r.cipher.EncryptMapValues(p.Credentials)
		if err != nil {
			return fmt.Errorf("encrypt credentials: %w", err)
		}
		b, err := json.Marshal(encCreds)
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
	encHeaders, err := r.cipher.EncryptMapValues(p.CustomHeaders)
	if err != nil {
		return fmt.Errorf("encrypt custom_headers: %w", err)
	}
	headersJSON, err := marshalHeaders(encHeaders)
	if err != nil {
		return fmt.Errorf("encode custom_headers: %w", err)
	}
	endpointsJSON, err := marshalEndpoints(p.Endpoints)
	if err != nil {
		return fmt.Errorf("encode endpoints: %w", err)
	}
	paramsJSON, err := marshalParams(p.ParamOverrides)
	if err != nil {
		return fmt.Errorf("encode param_overrides: %w", err)
	}
	windowsJSON, err := marshalWindows(p.ActiveWindows)
	if err != nil {
		return fmt.Errorf("encode active_windows: %w", err)
	}
	proxyEnc, err := r.cipher.EncryptString(p.ProxyURL)
	if err != nil {
		return fmt.Errorf("encrypt proxy_url: %w", err)
	}
	profileConfigJSON, err := marshalProfileConfig(p.ClientProfileConfig)
	if err != nil {
		return fmt.Errorf("encode client_profile_config: %w", err)
	}
	allowedModelsJSON, err := marshalAllowedModels(p.AllowedModels)
	if err != nil {
		return fmt.Errorf("encode allowed_models: %w", err)
	}

	const q = `
        UPDATE accounts SET
            name = $1, provider = $2, enabled = $3, weight = $4, priority = $5,
            cost_multiplier = $6, base_url = NULLIF($7, ''),
            credentials = COALESCE($8, credentials),
            circuit_failure_threshold = $9, circuit_open_duration_ms = $10,
            circuit_half_open_success = $11,
            model_redirects = $12, daily_usd_limit = $13, total_usd_limit = $14,
            group_id = $15, custom_headers = $16, endpoints = $17,
            health_probe_enabled = $18, proxy_url = NULLIF($19, ''),
            param_overrides = $20, active_windows = $21, active_timezone = NULLIF($22, ''),
            forward_client_ip = $23,
            auth_type = COALESCE(NULLIF($24, ''), 'api_key'), auth_plugin = NULLIF($25, ''),
            client_profile = NULLIF($26, ''), client_profile_config = $27,
            max_concurrency = $28, proxy_id = $29, allowed_models = $30,
            updated_at = NOW()
         WHERE id = $31`

	tag, err := r.pool.Exec(ctx, q,
		p.Name, p.Provider, p.Enabled, p.Weight, p.Priority,
		p.CostMultiplier, p.BaseURL, credentialsJSON,
		p.CircuitFailureThreshold, openDurationMs, p.CircuitHalfOpenSuccess,
		redirectsJSON, p.DailyUSDLimit, p.TotalUSDLimit, p.GroupID, headersJSON, endpointsJSON,
		p.HealthProbeEnabled, proxyEnc, paramsJSON, windowsJSON, p.ActiveTimezone, p.ForwardClientIP,
		p.AuthType, p.AuthPlugin, p.ClientProfile, profileConfigJSON,
		p.MaxConcurrency, p.ProxyID, allowedModelsJSON,
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
