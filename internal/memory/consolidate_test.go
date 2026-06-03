package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"growbot/internal/store"

	"github.com/stretchr/testify/require"
)

// openDB opens a real migrated SQLite database in a temp dir so the chains
// table created by migration 0002 exists.
func openDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "m.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, ctx
}

// insertChain inserts a single chain row with explicit strength and last_used
// so consolidation behavior is deterministic.
func insertChain(t *testing.T, ctx context.Context, db *sql.DB, prevKey, next string, strength float64, lastUsed string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO chains (prev_key, next_surface, count, strength, last_used) VALUES (?, ?, ?, ?, ?)`,
		prevKey, next, 1, strength, lastUsed,
	)
	require.NoError(t, err)
}

// strengthOf returns the stored strength for a chain, or fails if it is absent.
func strengthOf(t *testing.T, ctx context.Context, db *sql.DB, prevKey, next string) float64 {
	t.Helper()
	var s float64
	err := db.QueryRowContext(ctx,
		`SELECT strength FROM chains WHERE prev_key = ? AND next_surface = ?`,
		prevKey, next,
	).Scan(&s)
	require.NoError(t, err)
	return s
}

// chainExists reports whether a chain row is present.
func chainExists(t *testing.T, ctx context.Context, db *sql.DB, prevKey, next string) bool {
	t.Helper()
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chains WHERE prev_key = ? AND next_surface = ?`,
		prevKey, next,
	).Scan(&n)
	require.NoError(t, err)
	return n > 0
}

// TestConsolidate verifies that one pass reinforces recently-used chains,
// decays disused ones, and prunes chains whose strength fell below the
// threshold.
func TestConsolidate(t *testing.T) {
	db, ctx := openDB(t)

	// last_used は実運用では CURRENT_TIMESTAMP により RFC3339 形式で保存されるため、
	// テストも同形式を用いて現実の保存形式を反映させる。
	// recent: since 以降に使用、強化対象。
	insertChain(t, ctx, db, "a", "recent", 0.5, "2026-01-03T00:00:00Z")
	// old: since より前、減衰しても生き残る (0.5*0.9 = 0.45)。
	insertChain(t, ctx, db, "a", "old", 0.5, "2026-01-01T00:00:00Z")
	// faded: since より前、減衰で閾値を割り削除される (0.05*0.9 = 0.045 < 0.05)。
	insertChain(t, ctx, db, "a", "faded", 0.05, "2026-01-01T00:00:00Z")

	since := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	report, err := Consolidate(ctx, db, since, now, Params{
		Decay:      0.9,
		Reinforce:  0.1,
		Cap:        1.0,
		PruneBelow: 0.05,
	})
	require.NoError(t, err)

	// recent 1件を強化、old/faded の2件を減衰、faded 1件を削除。
	require.Equal(t, int64(1), report.Reinforced)
	require.Equal(t, int64(2), report.Decayed)
	require.Equal(t, int64(1), report.Pruned)

	// recent: min(1.0, 0.5+0.1) = 0.6。
	require.InDelta(t, 0.6, strengthOf(t, ctx, db, "a", "recent"), 1e-9)
	// old: 0.5*0.9 = 0.45 で生存。
	require.InDelta(t, 0.45, strengthOf(t, ctx, db, "a", "old"), 1e-9)
	// faded: 削除済み。
	require.False(t, chainExists(t, ctx, db, "a", "faded"))

	// 整理時刻が同一トランザクションで meta に記録されている。
	lc, ok, err := LastConsolidation(ctx, db)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, lc.Equal(now), "expected %v, got %v", now, lc)
}

// TestConsolidateNilDB verifies that a nil database handle yields an error
// rather than a panic.
func TestConsolidateNilDB(t *testing.T) {
	_, err := Consolidate(context.Background(), nil, time.Now(), time.Now(), Params{})
	require.Error(t, err)
}
