package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate applies every embedded SQL file in lexical order that has
// not already been recorded in the schema_migrations table.
//
// The runner is forward-only by design: there is no down path. Each
// migration file becomes its own transaction; a failure aborts that
// migration and surfaces the error to the caller, leaving prior
// migrations applied. Re-running Migrate is safe — applied files are
// skipped.
//
// File naming: migrations/NNNN_short_name.sql, lexically sorted.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("db.Migrate: nil pool")
	}

	if err := ensureSchemaMigrationsTable(ctx, pool); err != nil {
		return fmt.Errorf("db.Migrate: bootstrap: %w", err)
	}

	files, err := listMigrations()
	if err != nil {
		return fmt.Errorf("db.Migrate: list: %w", err)
	}

	applied, err := loadApplied(ctx, pool)
	if err != nil {
		return fmt.Errorf("db.Migrate: load applied: %w", err)
	}

	for _, name := range files {
		if applied[name] {
			continue
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("db.Migrate: read %s: %w", name, err)
		}
		if err := applyOne(ctx, pool, name, string(body)); err != nil {
			return fmt.Errorf("db.Migrate: apply %s: %w", name, err)
		}
	}
	return nil
}

func ensureSchemaMigrationsTable(ctx context.Context, pool *pgxpool.Pool) error {
	const stmt = `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            name        VARCHAR(255) PRIMARY KEY,
            applied_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
        )`
	_, err := pool.Exec(ctx, stmt)
	return err
}

func listMigrations() ([]string, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".sql") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

func loadApplied(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

// applyOne runs a single migration file inside a transaction together
// with its bookkeeping insert. Either the migration commits both the
// schema change and the schema_migrations row, or neither lands.
func applyOne(ctx context.Context, pool *pgxpool.Pool, name, body string) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("record applied: %w", err)
	}
	return tx.Commit(ctx)
}
