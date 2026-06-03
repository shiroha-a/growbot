package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMigration0003AppliesOnPopulatedSelf reproduces the in-place upgrade path:
// a self row already exists before 0003 runs. SQLite rejects ADD COLUMN with a
// non-constant DEFAULT (e.g. CURRENT_TIMESTAMP) on a populated table, so 0003
// must add updated_at with a constant default and backfill. This guards that.
func TestMigration0003AppliesOnPopulatedSelf(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "up.db")

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// 0001 相当の self テーブルを作り、行を1つ入れて「誕生済み」状態を再現する。
	_, err = db.ExecContext(ctx, `
		CREATE TABLE self (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			born_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO self (id) VALUES (1);`)
	require.NoError(t, err)

	// 0003 を適用し、既存行があってもエラーにならないことを確認する。
	migration, err := os.ReadFile("migrations/0003_psyche.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)

	// updated_at 列が追加されていること。
	var hasCol int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('self') WHERE name = 'updated_at'`).Scan(&hasCol))
	require.Equal(t, 1, hasCol)

	// 既存行が CURRENT_TIMESTAMP でバックフィルされ、定数の初期値のままでないこと。
	var updatedAt string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT updated_at FROM self WHERE id = 1`).Scan(&updatedAt))
	require.NotEqual(t, "1970-01-01T00:00:00Z", updatedAt)
}
