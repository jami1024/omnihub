// Package db owns the gateway's PostgreSQL connection pool and the
// embedded migration set.
//
// The connection pool is created once at startup, kept alive for the
// lifetime of the process, and shared across all repositories. The
// migration runner is intentionally minimal: it walks the embedded
// migrations/*.sql files in lexical order, records each as applied in
// a schema_migrations table, and skips already-applied files. There is
// no rollback path — destructive changes must be designed forward-only.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the parameters for opening the connection pool.
type Config struct {
	// DSN is the PostgreSQL connection string, e.g.
	// "postgres://omnihub:omnihub@localhost:5432/omnihub?sslmode=disable".
	DSN string

	// MaxConns caps the pool size. Zero falls back to pgx default.
	MaxConns int32

	// MinConns is the minimum number of warm connections.
	MinConns int32

	// MaxConnLifetime forces a connection rotation after this age.
	// Zero falls back to pgx default (1 hour).
	MaxConnLifetime time.Duration

	// MaxConnIdleTime closes idle connections after this duration.
	// Zero falls back to pgx default (30 minutes).
	MaxConnIdleTime time.Duration
}

// Open connects to PostgreSQL, configures the pool, and verifies the
// connection with a Ping. The returned pool is safe for concurrent use.
//
// The caller owns the pool and must Close() it at shutdown.
func Open(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("db: DSN is empty")
	}

	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("db: parse DSN: %w", err)
	}

	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pcfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		pcfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		pcfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}
