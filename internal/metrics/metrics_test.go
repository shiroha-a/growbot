package metrics

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"growbot/internal/store"

	"github.com/stretchr/testify/require"
)

// openTestDB opens a fresh migrated database in a temp directory.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")
	db, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestLatestEmpty verifies that Latest reports no snapshot on an empty table.
func TestLatestEmpty(t *testing.T) {
	ctx := context.Background()
	s := NewStore(openTestDB(t))

	got, ok, err := s.Latest(ctx)
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, got)
}

// TestRecordAndLatest verifies that a recorded snapshot is returned by Latest
// with its fields preserved and a database-populated RecordedAt.
func TestRecordAndLatest(t *testing.T) {
	ctx := context.Background()
	s := NewStore(openTestDB(t))

	snap := Snapshot{
		Vocab:     172,
		Chains:    346,
		Friends:   3,
		Stage:     "adolescent",
		AvgReward: 0.42,
	}
	require.NoError(t, s.Record(ctx, snap))

	got, ok, err := s.Latest(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, got.RecordedAt.IsZero())
	require.Equal(t, snap.Vocab, got.Vocab)
	require.Equal(t, snap.Chains, got.Chains)
	require.Equal(t, snap.Friends, got.Friends)
	require.Equal(t, snap.Stage, got.Stage)
	require.InDelta(t, snap.AvgReward, got.AvgReward, 1e-9)
}

// TestLatestReturnsNewest verifies that, after a second Record, Latest returns
// the more recently inserted snapshot.
func TestLatestReturnsNewest(t *testing.T) {
	ctx := context.Background()
	s := NewStore(openTestDB(t))

	require.NoError(t, s.Record(ctx, Snapshot{
		Vocab:     172,
		Chains:    346,
		Friends:   3,
		Stage:     "adolescent",
		AvgReward: 0.42,
	}))

	newer := Snapshot{
		Vocab:     200,
		Chains:    420,
		Friends:   5,
		Stage:     "adult",
		AvgReward: 0.61,
	}
	require.NoError(t, s.Record(ctx, newer))

	got, ok, err := s.Latest(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, newer.Vocab, got.Vocab)
	require.Equal(t, newer.Chains, got.Chains)
	require.Equal(t, newer.Friends, got.Friends)
	require.Equal(t, newer.Stage, got.Stage)
	require.InDelta(t, newer.AvgReward, got.AvgReward, 1e-9)
}

// TestLatestSameSecondTiebreak records many snapshots within one wall-clock
// second (identical recorded_at) and verifies Latest returns the last inserted,
// proving the rowid tiebreak rather than relying on timestamp granularity.
func TestLatestSameSecondTiebreak(t *testing.T) {
	ctx := context.Background()
	s := NewStore(openTestDB(t))

	const n = 30
	for i := 0; i < n; i++ {
		require.NoError(t, s.Record(ctx, Snapshot{Vocab: i, Stage: "child"}))
	}

	got, ok, err := s.Latest(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, n-1, got.Vocab)
}

// TestNilStore verifies that nil-receiver methods fail safely instead of
// panicking.
func TestNilStore(t *testing.T) {
	ctx := context.Background()
	var s *Store

	require.Error(t, s.Record(ctx, Snapshot{}))

	_, ok, err := s.Latest(ctx)
	require.Error(t, err)
	require.False(t, ok)
}
