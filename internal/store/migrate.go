package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"
)

// migrationsFS embeds the SQL migration files shipped with the binary.
// The embed path is relative to this package directory.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate ensures the schema_migrations bookkeeping table exists and applies
// every embedded migration that has not yet been recorded, in ascending
// filename order.
//
// Each migration runs inside its own transaction together with the insert into
// schema_migrations, so a migration and its recording either both commit or
// both roll back. Migrate is idempotent: re-running it after success applies
// nothing.
func Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("store: migrate: nil db")
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	files, err := migrationFiles()
	if err != nil {
		return err
	}

	for _, name := range files {
		// 既に適用済みのマイグレーションはスキップして冪等性を保つ。
		if applied[name] {
			continue
		}
		if err := applyMigration(ctx, db, name); err != nil {
			return err
		}
	}

	return nil
}

// appliedVersions returns the set of migration versions already recorded.
func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations;`)
	if err != nil {
		return nil, fmt.Errorf("store: query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("store: scan schema_migrations: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate schema_migrations: %w", err)
	}
	return applied, nil
}

// migrationFiles returns the embedded migration filenames sorted ascending.
func migrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: read migrations dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	// ファイル名の昇順で適用順序を決定する(0001, 0002 ...)。
	sort.Strings(names)
	return names, nil
}

// applyMigration runs a single migration and records its version atomically.
func applyMigration(ctx context.Context, db *sql.DB, name string) error {
	content, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return fmt.Errorf("store: read migration %q: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx for %q: %w", name, err)
	}

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		// ロールバックは実行するが、本来のエラーを優先して返す。
		_ = tx.Rollback()
		return fmt.Errorf("store: exec migration %q: %w", name, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?);`,
		name, time.Now().UTC(),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: record migration %q: %w", name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migration %q: %w", name, err)
	}
	return nil
}
