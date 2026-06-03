package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenMigrateIdempotent verifies that opening (which also migrates) and
// migrating again does not double-apply migrations and leaves the expected
// schema in place.
func TestOpenMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")

	db, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Open already ran Migrate once; run it again to confirm idempotency.
	require.NoError(t, Migrate(ctx, db))
	require.NoError(t, Migrate(ctx, db))

	// schema_migrations must contain one row per embedded migration
	// (0001_init .. 0006_metrics).
	var count int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations;`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 6, count)

	// The self table must exist.
	var name string
	err = db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'self';`,
	).Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "self", name)
}

// TestOpenReopenIdempotent verifies that reopening an existing database file
// applies no additional migrations.
func TestOpenReopenIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")

	db1, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	db2, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })

	var count int
	err = db2.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations;`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 6, count)
}

// TestSelfTableSingleRowConstraint verifies the CHECK (id = 1) constraint on
// the self identity table.
func TestSelfTableSingleRowConstraint(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")

	db, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(ctx, `INSERT INTO self (id) VALUES (1);`)
	require.NoError(t, err)

	// id != 1 must be rejected by the CHECK constraint.
	_, err = db.ExecContext(ctx, `INSERT INTO self (id) VALUES (2);`)
	require.Error(t, err)

	var bornAt sql.NullString
	err = db.QueryRowContext(ctx,
		`SELECT born_at FROM self WHERE id = 1;`).Scan(&bornAt)
	require.NoError(t, err)
	require.True(t, bornAt.Valid)
}
