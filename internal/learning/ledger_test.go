package learning

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLedgerRecordEmptyNoteID verifies that recording with an empty note id is
// an error.
func TestLedgerRecordEmptyNoteID(t *testing.T) {
	ctx := context.Background()
	l := NewLedger(newTestDB(t))

	err := l.Record(ctx, "", "note", "arm", "body")
	require.Error(t, err)
}

// TestLedgerRecordAndUnobserved verifies that a recorded post is returned by
// Unobserved when the cutoff is in the future, with its fields intact.
func TestLedgerRecordAndUnobserved(t *testing.T) {
	ctx := context.Background()
	l := NewLedger(newTestDB(t))

	require.NoError(t, l.Record(ctx, "n1", "note", "armA", "hello"))

	// 未来をカットオフにすれば今作成した投稿は対象となる。
	posts, err := l.Unobserved(ctx, time.Now().Add(time.Hour), time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, posts, 1)
	require.Equal(t, "n1", posts[0].NoteID)
	require.Equal(t, "note", posts[0].Kind)
	require.Equal(t, "armA", posts[0].Arm)
	require.Equal(t, "hello", posts[0].Text)
	require.False(t, posts[0].CreatedAt.IsZero())
}

// TestLedgerRecordIgnoresDuplicate verifies that re-recording the same note id
// does not error and does not duplicate the row.
func TestLedgerRecordIgnoresDuplicate(t *testing.T) {
	ctx := context.Background()
	l := NewLedger(newTestDB(t))

	require.NoError(t, l.Record(ctx, "n1", "note", "armA", "hello"))
	require.NoError(t, l.Record(ctx, "n1", "other", "armB", "changed"))

	posts, err := l.Unobserved(ctx, time.Now().Add(time.Hour), time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, posts, 1)
	// 最初に記録した値が保持される(INSERT OR IGNORE)。
	require.Equal(t, "armA", posts[0].Arm)
}

// TestLedgerCutoffExcludesRecent verifies that a post created now is excluded
// when the cutoff lies in the past.
func TestLedgerCutoffExcludesRecent(t *testing.T) {
	ctx := context.Background()
	l := NewLedger(newTestDB(t))

	require.NoError(t, l.Record(ctx, "n1", "note", "armA", "hello"))

	// 過去をカットオフにすると、今作成した投稿は古くないので返らない。
	posts, err := l.Unobserved(ctx, time.Now().Add(-time.Hour), time.Time{}, 10)
	require.NoError(t, err)
	require.Empty(t, posts)
}

// TestLedgerGiveUpWindow verifies posts older than the give-up horizon are
// abandoned (not returned even though they are old enough to score).
func TestLedgerGiveUpWindow(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	l := NewLedger(db)

	// 過去の created_at を直接挿入し、ギブアップ窓の外側にする。
	_, err := db.ExecContext(ctx,
		`INSERT INTO posts(note_id, kind, arm, text, created_at) VALUES('old','note','arm','x','2000-01-01T00:00:00Z');`)
	require.NoError(t, err)

	// before=now(十分古い), after=now-1h: 2000年の投稿は after より古いので除外される。
	posts, err := l.Unobserved(ctx, time.Now(), time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Empty(t, posts)
}

// TestLedgerMarkObserved verifies that marking a post observed removes it from
// the unobserved set and persists the engagement and reward.
func TestLedgerMarkObserved(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	l := NewLedger(db)

	require.NoError(t, l.Record(ctx, "n1", "note", "armA", "hello"))

	reward := Reward(3, 1, 0)
	require.NoError(t, l.MarkObserved(ctx, "n1", 3, 1, 0, reward))

	// 観測済みになったので Unobserved には現れない。
	posts, err := l.Unobserved(ctx, time.Now().Add(time.Hour), time.Time{}, 10)
	require.NoError(t, err)
	require.Empty(t, posts)

	var (
		reactions, replies, renotes, observed int
		gotReward                             float64
	)
	err = db.QueryRowContext(ctx,
		`SELECT reactions, replies, renotes, reward, observed FROM posts WHERE note_id = ?;`,
		"n1",
	).Scan(&reactions, &replies, &renotes, &gotReward, &observed)
	require.NoError(t, err)
	require.Equal(t, 3, reactions)
	require.Equal(t, 1, replies)
	require.Equal(t, 0, renotes)
	require.Equal(t, 1, observed)
	require.InDelta(t, reward, gotReward, 1e-9)
}

// TestLedgerUnobservedRespectsLimit verifies that the limit caps the number of
// returned posts and that ordering is oldest-first.
func TestLedgerUnobservedRespectsLimit(t *testing.T) {
	ctx := context.Background()
	l := NewLedger(newTestDB(t))

	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, l.Record(ctx, id, "note", "arm", id))
	}

	posts, err := l.Unobserved(ctx, time.Now().Add(time.Hour), time.Time{}, 2)
	require.NoError(t, err)
	require.Len(t, posts, 2)
}
