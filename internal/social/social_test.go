package social

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"growbot/internal/store"

	"github.com/stretchr/testify/require"
)

// newTestStore opens a fresh migrated database in a temp dir and returns a
// social Store backed by it.
func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")
	db, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db), db
}

// TestObserveNewInteraction verifies that the first interaction creates a row
// with the interaction counter set, affinity equal to the gain, timestamps
// populated, and the username stored.
func TestObserveNewInteraction(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	rel, err := s.Observe(ctx, "actor-1", "alice", KindInteraction, 0.1)
	require.NoError(t, err)
	require.Equal(t, "actor-1", rel.ActorID)
	require.Equal(t, "alice", rel.Username)
	require.Equal(t, 1, rel.Interactions)
	require.Equal(t, 0, rel.ReactionsReceived)
	require.InDelta(t, 0.1, rel.Affinity, 1e-9)
	require.False(t, rel.FirstSeen.IsZero())
	require.False(t, rel.LastSeen.IsZero())
}

// TestObserveRepeatedInteraction verifies that re-observing the same actor
// increments the counter, grows affinity additively, and preserves first_seen.
func TestObserveRepeatedInteraction(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	first, err := s.Observe(ctx, "actor-1", "alice", KindInteraction, 0.1)
	require.NoError(t, err)

	second, err := s.Observe(ctx, "actor-1", "alice", KindInteraction, 0.1)
	require.NoError(t, err)
	require.Equal(t, 2, second.Interactions)
	require.Equal(t, 0, second.ReactionsReceived)
	require.InDelta(t, 0.2, second.Affinity, 1e-9)

	// first_seen は再観測でも不変であること。
	require.Equal(t, first.FirstSeen.UTC(), second.FirstSeen.UTC())
}

// TestObserveReactionBumpsReactions verifies that a reaction event increments
// reactions_received rather than interactions.
func TestObserveReactionBumpsReactions(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	rel, err := s.Observe(ctx, "actor-1", "bob", KindReaction, 0.05)
	require.NoError(t, err)
	require.Equal(t, 0, rel.Interactions)
	require.Equal(t, 1, rel.ReactionsReceived)
	require.InDelta(t, 0.05, rel.Affinity, 1e-9)

	// 続けて反応を観測しても interactions は増えない。
	rel, err = s.Observe(ctx, "actor-1", "bob", KindReaction, 0.05)
	require.NoError(t, err)
	require.Equal(t, 0, rel.Interactions)
	require.Equal(t, 2, rel.ReactionsReceived)
}

// TestAffinityClampsAtOne verifies that affinity saturates at 1.0 regardless of
// how many gains accumulate.
func TestAffinityClampsAtOne(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	var rel *Relationship
	var err error
	for i := 0; i < 50; i++ {
		rel, err = s.Observe(ctx, "actor-1", "alice", KindInteraction, 0.1)
		require.NoError(t, err)
	}
	require.InDelta(t, 1.0, rel.Affinity, 1e-9)
	require.LessOrEqual(t, rel.Affinity, 1.0)
	require.Equal(t, 50, rel.Interactions)

	aff, err := s.Affinity(ctx, "actor-1")
	require.NoError(t, err)
	require.InDelta(t, 1.0, aff, 1e-9)
}

// TestGetUnknownActor verifies the not-found contract for Get and Affinity.
func TestGetUnknownActor(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	rel, ok, err := s.Get(ctx, "nobody")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, rel)

	aff, err := s.Affinity(ctx, "nobody")
	require.NoError(t, err)
	require.Equal(t, 0.0, aff)
}

// TestObserveEmptyUsernameDoesNotOverwrite verifies that a later observation
// with an empty username keeps the previously stored username.
func TestObserveEmptyUsernameDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	_, err := s.Observe(ctx, "actor-1", "alice", KindInteraction, 0.1)
	require.NoError(t, err)

	rel, err := s.Observe(ctx, "actor-1", "", KindInteraction, 0.1)
	require.NoError(t, err)
	require.Equal(t, "alice", rel.Username)
}

// TestObserveValidation verifies the guard conditions on Observe.
func TestObserveValidation(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	_, err := s.Observe(ctx, "", "alice", KindInteraction, 0.1)
	require.Error(t, err)

	var nilStore *Store
	_, err = nilStore.Observe(ctx, "actor-1", "alice", KindInteraction, 0.1)
	require.Error(t, err)
}
