package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/crypto"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// ProxyRepo persists upstream egress proxies (migration 0038). The
// password is encrypted at rest with the injected cipher, mirroring
// account credentials.
type ProxyRepo struct {
	pool   *pgxpool.Pool
	cipher *crypto.Cipher
}

// NewProxyRepo wires the repository onto an existing pool. cipher may be
// a disabled cipher (encryption off / passthrough).
func NewProxyRepo(pool *pgxpool.Pool, cipher *crypto.Cipher) *ProxyRepo {
	return &ProxyRepo{pool: pool, cipher: cipher}
}

// ErrProxyNotFound is returned when a single-row lookup misses.
var ErrProxyNotFound = errors.New("proxy not found")

// ErrProxyNameTaken is returned on a UNIQUE(name) collision.
var ErrProxyNameTaken = errors.New("proxy name already in use")

const proxyColumns = `id, name, protocol, host, port,
        COALESCE(username, ''), COALESCE(password, ''), status,
        expires_at, fallback_mode, backup_proxy_id`

func (r *ProxyRepo) scanProxy(row pgx.Row) (*provider.Proxy, error) {
	var (
		p           provider.Proxy
		passwordEnc string
	)
	if err := row.Scan(
		&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port,
		&p.Username, &passwordEnc, &p.Status,
		&p.ExpiresAt, &p.FallbackMode, &p.BackupProxyID,
	); err != nil {
		return nil, err
	}
	pw, err := r.cipher.DecryptString(passwordEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt password for proxy %q: %w", p.Name, err)
	}
	p.Password = pw
	return &p, nil
}

// ListAll returns every proxy ordered by name.
func (r *ProxyRepo) ListAll(ctx context.Context) ([]*provider.Proxy, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+proxyColumns+` FROM proxies ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("query proxies: %w", err)
	}
	defer rows.Close()

	var out []*provider.Proxy
	for rows.Next() {
		p, err := r.scanProxy(rows)
		if err != nil {
			return nil, fmt.Errorf("scan proxy row: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetByID fetches one proxy.
func (r *ProxyRepo) GetByID(ctx context.Context, id int64) (*provider.Proxy, error) {
	p, err := r.scanProxy(r.pool.QueryRow(ctx, `SELECT `+proxyColumns+` FROM proxies WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProxyNotFound
		}
		return nil, fmt.Errorf("query proxy %d: %w", id, err)
	}
	return p, nil
}

// ProxyParams carries the mutable columns for create/update.
type ProxyParams struct {
	Name          string
	Protocol      string
	Host          string
	Port          int
	Username      string
	Password      string
	Status        string
	ExpiresAt     *time.Time
	FallbackMode  string
	BackupProxyID *int64
}

// Insert creates a proxy and returns its id. Duplicate names map to
// ErrProxyNameTaken.
func (r *ProxyRepo) Insert(ctx context.Context, p ProxyParams) (int64, error) {
	pwEnc, err := r.cipher.EncryptString(p.Password)
	if err != nil {
		return 0, fmt.Errorf("encrypt password: %w", err)
	}
	const q = `
        INSERT INTO proxies (name, protocol, host, port, username, password,
                             status, expires_at, fallback_mode, backup_proxy_id)
        VALUES ($1, COALESCE(NULLIF($2, ''), 'http'), $3, $4, $5, $6,
                COALESCE(NULLIF($7, ''), 'active'), $8, COALESCE(NULLIF($9, ''), 'none'), $10)
        RETURNING id`
	var id int64
	err = r.pool.QueryRow(ctx, q,
		p.Name, p.Protocol, p.Host, p.Port, p.Username, pwEnc,
		p.Status, p.ExpiresAt, p.FallbackMode, p.BackupProxyID,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrProxyNameTaken
		}
		return 0, fmt.Errorf("insert proxy %q: %w", p.Name, err)
	}
	return id, nil
}

// Update replaces the mutable columns of the proxy identified by id.
func (r *ProxyRepo) Update(ctx context.Context, id int64, p ProxyParams) error {
	pwEnc, err := r.cipher.EncryptString(p.Password)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	const q = `
        UPDATE proxies SET
            name = $1, protocol = COALESCE(NULLIF($2, ''), 'http'),
            host = $3, port = $4, username = $5, password = $6,
            status = COALESCE(NULLIF($7, ''), 'active'),
            expires_at = $8, fallback_mode = COALESCE(NULLIF($9, ''), 'none'),
            backup_proxy_id = $10, updated_at = NOW()
         WHERE id = $11`
	tag, err := r.pool.Exec(ctx, q,
		p.Name, p.Protocol, p.Host, p.Port, p.Username, pwEnc,
		p.Status, p.ExpiresAt, p.FallbackMode, p.BackupProxyID, id,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrProxyNameTaken
		}
		return fmt.Errorf("update proxy %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProxyNotFound
	}
	return nil
}

// Delete removes a proxy. Accounts and backup references pointing at it
// are nulled by the ON DELETE SET NULL foreign keys.
func (r *ProxyRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM proxies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete proxy %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProxyNotFound
	}
	return nil
}
