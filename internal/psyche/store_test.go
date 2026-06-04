package psyche

import (
	"context"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"growbot/internal/store"
)

// openTestStore opens a temp-dir SQLite DB (with migrations applied) and wraps
// it in a psyche Store.
func openTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")

	db, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return NewStore(db), ctx
}

// TestLoadOrBirthCreatesSelf verifies that the first call births a self row
// with full energy and a sensible temperament/mood.
func TestLoadOrBirthCreatesSelf(t *testing.T) {
	s, ctx := openTestStore(t)

	self, err := s.LoadOrBirth(ctx, rand.New(rand.NewSource(1)))
	require.NoError(t, err)
	require.NotNil(t, self)

	require.Equal(t, 1.0, self.Energy)
	require.False(t, self.BornAt.IsZero())

	// Temperament traits are valid probabilities.
	for _, v := range []float64{
		self.Temperament.Extraversion,
		self.Temperament.Neuroticism,
		self.Temperament.Curiosity,
	} {
		require.GreaterOrEqual(t, v, 0.0)
		require.Less(t, v, 1.0)
	}

	// Mood matches the temperament baseline and is in range.
	inUnitMood(t, self.Mood)
	require.Equal(t, self.Temperament.BaselineMood(), self.Mood)

	// Drives start empty.
	require.Equal(t, Drives{}, self.Drives)
}

// TestLoadOrBirthIdempotent verifies a second LoadOrBirth returns the same
// identity (born_at + temperament), i.e. birth happens at most once.
func TestLoadOrBirthIdempotent(t *testing.T) {
	s, ctx := openTestStore(t)

	first, err := s.LoadOrBirth(ctx, rand.New(rand.NewSource(1)))
	require.NoError(t, err)

	// A different rng on the second call must not re-roll the temperament,
	// because the existing row is loaded rather than re-birthed.
	second, err := s.LoadOrBirth(ctx, rand.New(rand.NewSource(999)))
	require.NoError(t, err)

	require.Equal(t, first.BornAt, second.BornAt)
	require.Equal(t, first.Temperament, second.Temperament)
}

// TestSaveRoundTrips verifies that mutated mood/drives/energy persist across a
// reload.
func TestSaveRoundTrips(t *testing.T) {
	s, ctx := openTestStore(t)

	self, err := s.LoadOrBirth(ctx, rand.New(rand.NewSource(1)))
	require.NoError(t, err)

	self.Mood = Mood{Valence: 0.42, Arousal: -0.17}
	self.Drives = Drives{Recognition: 0.1, Curiosity: 0.2, Expression: 0.3, Boredom: 0.4}
	self.Energy = 0.55

	require.NoError(t, s.Save(ctx, self))

	reloaded, err := s.LoadOrBirth(ctx, rand.New(rand.NewSource(2)))
	require.NoError(t, err)

	require.InDelta(t, 0.42, reloaded.Mood.Valence, 1e-9)
	require.InDelta(t, -0.17, reloaded.Mood.Arousal, 1e-9)
	require.InDelta(t, 0.1, reloaded.Drives.Recognition, 1e-9)
	require.InDelta(t, 0.2, reloaded.Drives.Curiosity, 1e-9)
	require.InDelta(t, 0.3, reloaded.Drives.Expression, 1e-9)
	require.InDelta(t, 0.4, reloaded.Drives.Boredom, 1e-9)
	require.InDelta(t, 0.55, reloaded.Energy, 1e-9)

	// Identity is untouched by Save.
	require.Equal(t, self.BornAt, reloaded.BornAt)
	require.Equal(t, self.Temperament, reloaded.Temperament)
}

// TestNilGuards verifies that nil receivers/db/self are handled without panic.
func TestNilGuards(t *testing.T) {
	ctx := context.Background()

	var nilStore *Store
	_, err := nilStore.LoadOrBirth(ctx, nil)
	require.Error(t, err)
	require.Error(t, nilStore.Save(ctx, &Self{}))

	emptyStore := &Store{}
	_, err = emptyStore.LoadOrBirth(ctx, nil)
	require.Error(t, err)
	require.Error(t, emptyStore.Save(ctx, &Self{}))

	// Non-nil store but nil self.
	s, ctx := openTestStore(t)
	require.Error(t, s.Save(ctx, nil))
}
