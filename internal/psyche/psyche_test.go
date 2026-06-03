package psyche

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// inUnitMood asserts both mood fields stay within [-1,1].
func inUnitMood(t *testing.T, m Mood) {
	t.Helper()
	require.GreaterOrEqual(t, m.Valence, -1.0)
	require.LessOrEqual(t, m.Valence, 1.0)
	require.GreaterOrEqual(t, m.Arousal, -1.0)
	require.LessOrEqual(t, m.Arousal, 1.0)
}

// TestNewTemperamentDeterministic verifies that a seeded rng reproduces the
// same temperament and that all traits fall in [0,1).
func TestNewTemperamentDeterministic(t *testing.T) {
	a := NewTemperament(rand.New(rand.NewSource(42)))
	b := NewTemperament(rand.New(rand.NewSource(42)))
	require.Equal(t, a, b)

	for _, v := range []float64{a.Extraversion, a.Neuroticism, a.Curiosity} {
		require.GreaterOrEqual(t, v, 0.0)
		require.Less(t, v, 1.0)
	}

	// A different seed should (overwhelmingly likely) differ.
	c := NewTemperament(rand.New(rand.NewSource(7)))
	require.NotEqual(t, a, c)
}

// TestNewTemperamentNilRand verifies the nil-rng path still produces a valid
// temperament without panicking.
func TestNewTemperamentNilRand(t *testing.T) {
	got := NewTemperament(nil)
	for _, v := range []float64{got.Extraversion, got.Neuroticism, got.Curiosity} {
		require.GreaterOrEqual(t, v, 0.0)
		require.Less(t, v, 1.0)
	}
}

// TestBaselineMoodInRange checks the derived baseline mood stays in [-1,1]
// across the full temperament corner space.
func TestBaselineMoodInRange(t *testing.T) {
	for _, e := range []float64{0, 0.5, 1} {
		for _, n := range []float64{0, 0.5, 1} {
			tm := Temperament{Extraversion: e, Neuroticism: n, Curiosity: 0.5}
			inUnitMood(t, tm.BaselineMood())
		}
	}
}

// TestTowardBaselineClampsRateAndResult verifies rate clamping and convergence.
func TestTowardBaselineClampsRateAndResult(t *testing.T) {
	base := Mood{Valence: 0.5, Arousal: -0.5}
	m := Mood{Valence: -1, Arousal: 1}

	// rate=0 keeps the mood unchanged.
	require.Equal(t, m, m.TowardBaseline(base, 0))

	// rate=1 (and any over-1 rate, which clamps to 1) lands exactly on baseline.
	require.Equal(t, base, m.TowardBaseline(base, 1))
	require.Equal(t, base, m.TowardBaseline(base, 5))

	// Negative rate clamps to 0, i.e. no movement.
	require.Equal(t, m, m.TowardBaseline(base, -3))

	// A partial step moves toward baseline and stays in range.
	half := m.TowardBaseline(base, 0.5)
	inUnitMood(t, half)
	require.InDelta(t, -0.25, half.Valence, 1e-9)
	require.InDelta(t, 0.25, half.Arousal, 1e-9)
}

// TestNudgeClamps verifies Nudge adds deltas and clamps to [-1,1].
func TestNudgeClamps(t *testing.T) {
	m := Mood{Valence: 0.9, Arousal: -0.9}

	up := m.Nudge(0.5, -0.5)
	require.Equal(t, 1.0, up.Valence)
	require.Equal(t, -1.0, up.Arousal)

	mid := Mood{}.Nudge(0.3, -0.2)
	require.InDelta(t, 0.3, mid.Valence, 1e-9)
	require.InDelta(t, -0.2, mid.Arousal, 1e-9)
}

// TestDrivesGrowIncreasesAndClamps verifies drives grow over an hour, that
// temperament scales Curiosity/Expression, and that growth saturates at 1.0.
func TestDrivesGrowIncreasesAndClamps(t *testing.T) {
	tm := Temperament{Extraversion: 1, Neuroticism: 0.5, Curiosity: 1}

	grown := Drives{}.Grow(time.Hour, tm)
	require.Greater(t, grown.Recognition, 0.0)
	require.Greater(t, grown.Curiosity, 0.0)
	require.Greater(t, grown.Expression, 0.0)
	require.Greater(t, grown.Boredom, 0.0)
	// 1時間・base 0.1・最大係数(1.0)なので各欠乏は約0.1。
	require.InDelta(t, driveBaseRatePerHour, grown.Recognition, 1e-9)

	// Lower temperament traits slow Curiosity and Expression growth.
	mild := Temperament{Extraversion: 0.2, Curiosity: 0.2}
	g2 := Drives{}.Grow(time.Hour, mild)
	require.Less(t, g2.Curiosity, g2.Recognition)
	require.Less(t, g2.Expression, g2.Recognition)

	// A long elapsed time clamps every deficit at 1.0.
	sat := Drives{}.Grow(1000*time.Hour, tm)
	require.Equal(t, 1.0, sat.Recognition)
	require.Equal(t, 1.0, sat.Curiosity)
	require.Equal(t, 1.0, sat.Expression)
	require.Equal(t, 1.0, sat.Boredom)

	// Non-positive dt is a no-op.
	start := Drives{Recognition: 0.3}
	require.Equal(t, start, start.Grow(0, tm))
	require.Equal(t, start, start.Grow(-time.Hour, tm))
}

// TestDrivesBumpSatisfiesAndClamps verifies Bump adjusts the named drive and
// clamps to [0,1], and that an unknown kind is inert.
func TestDrivesBumpSatisfiesAndClamps(t *testing.T) {
	d := Drives{Recognition: 0.4, Curiosity: 0.4, Expression: 0.4, Boredom: 0.4}

	// Positive delta intensifies, clamped at 1.0.
	require.Equal(t, 1.0, d.Bump(DriveRecognition, 5).Recognition)

	// Negative delta satisfies, clamped at 0.0.
	require.Equal(t, 0.0, d.Bump(DriveBoredom, -5).Boredom)

	// Partial satisfy.
	require.InDelta(t, 0.1, d.Bump(DriveCuriosity, -0.3).Curiosity, 1e-9)

	// Only the named drive changes.
	got := d.Bump(DriveExpression, 0.2)
	require.InDelta(t, 0.6, got.Expression, 1e-9)
	require.Equal(t, d.Recognition, got.Recognition)

	// Unknown kind leaves drives untouched.
	require.Equal(t, d, d.Bump(DriveKind(99), 0.5))
}

// TestDrivesGetReadsNamedDrive verifies Get returns the matching field and 0
// for unknown kinds.
func TestDrivesGetReadsNamedDrive(t *testing.T) {
	d := Drives{Recognition: 0.1, Curiosity: 0.2, Expression: 0.3, Boredom: 0.4}
	require.Equal(t, 0.1, d.Get(DriveRecognition))
	require.Equal(t, 0.2, d.Get(DriveCuriosity))
	require.Equal(t, 0.3, d.Get(DriveExpression))
	require.Equal(t, 0.4, d.Get(DriveBoredom))
	require.Equal(t, 0.0, d.Get(DriveKind(99)))
}

// TestUrgeIsMean verifies Urge is the clamped mean of the four deficits.
func TestUrgeIsMean(t *testing.T) {
	d := Drives{Recognition: 0.2, Curiosity: 0.4, Expression: 0.6, Boredom: 0.8}
	require.InDelta(t, 0.5, d.Urge(), 1e-9)

	require.Equal(t, 0.0, Drives{}.Urge())

	full := Drives{Recognition: 1, Curiosity: 1, Expression: 1, Boredom: 1}
	require.Equal(t, 1.0, full.Urge())
	require.False(t, math.IsNaN(full.Urge()))
}
