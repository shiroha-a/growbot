package voice

import (
	"math/rand"
	"strings"
	"testing"

	"growbot/internal/psyche"

	"github.com/stretchr/testify/require"
)

// TestDecorateEmptyReturnsEmpty verifies that empty input is returned unchanged
// regardless of mood or rng.
func TestDecorateEmptyReturnsEmpty(t *testing.T) {
	require.Equal(t, "", Decorate("", psyche.Mood{Valence: 1, Arousal: 1}, rand.New(rand.NewSource(1))))
	require.Equal(t, "", Decorate("", psyche.Mood{}, nil))
}

// TestDecorateDeterministic verifies that the same seed and inputs always yield
// the same output.
func TestDecorateDeterministic(t *testing.T) {
	const text = "こんにちは"
	moods := []psyche.Mood{
		{Valence: 0.8, Arousal: 0.8},
		{Valence: 0.5, Arousal: -0.5},
		{Valence: -0.8, Arousal: 0.0},
		{Valence: 0.0, Arousal: 0.0},
		{Valence: 0.0, Arousal: 0.8},
	}
	for _, m := range moods {
		first := Decorate(text, m, rand.New(rand.NewSource(42)))
		second := Decorate(text, m, rand.New(rand.NewSource(42)))
		require.Equal(t, first, second, "Decorate must be deterministic for a fixed seed (mood %+v)", m)
	}
}

// TestDecorateAlwaysPrefix verifies that, across many seeds and moods, the
// output never corrupts the input: text is always a prefix and the result is
// never shorter than the input.
func TestDecorateAlwaysPrefix(t *testing.T) {
	const text = "今日もいい天気"
	moods := []psyche.Mood{
		{Valence: 0.9, Arousal: 0.9},
		{Valence: 0.6, Arousal: -0.6},
		{Valence: -0.9, Arousal: 0.4},
		{Valence: -0.4, Arousal: -0.4},
		{Valence: 0.1, Arousal: 0.1},
		{Valence: 0.0, Arousal: 0.9},
	}
	for seed := int64(0); seed < 200; seed++ {
		for _, m := range moods {
			got := Decorate(text, m, rand.New(rand.NewSource(seed)))
			require.True(t, strings.HasPrefix(got, text),
				"output %q must keep input %q as a prefix (seed %d, mood %+v)", got, text, seed, m)
			require.GreaterOrEqual(t, len(got), len(text),
				"output must never be shorter than the input (seed %d, mood %+v)", seed, m)
			require.NotContains(t, got, "\n", "output must not contain control characters")
			require.NotContains(t, got, "\r", "output must not contain control characters")
		}
	}
}

// TestDecoratePositiveAppendsCheerfulMarker verifies that a strongly positive,
// calm mood appends one of the expected cheerful markers for at least one
// fixed seed, and that whenever a suffix is added it is drawn from that set.
func TestDecoratePositiveAppendsCheerfulMarker(t *testing.T) {
	const text = "やった"
	m := psyche.Mood{Valence: 0.9, Arousal: -0.4}

	appended := false
	for seed := int64(0); seed < 50; seed++ {
		got := Decorate(text, m, rand.New(rand.NewSource(seed)))
		require.True(t, strings.HasPrefix(got, text), "input must remain a prefix")
		require.GreaterOrEqual(t, len(got), len(text), "output must be at least as long as input")

		suffix := strings.TrimPrefix(got, text)
		if suffix == "" {
			continue
		}
		appended = true
		require.Contains(t, cheerfulMarkers, suffix,
			"any appended suffix for a positive calm mood must be a cheerful marker (seed %d)", seed)
	}
	require.True(t, appended, "at least one seed must produce a cheerful marker")
}

// TestDecorateExcitedAppendsExcitedMarker verifies that a high-arousal positive
// mood, when it appends, draws from the excited marker set.
func TestDecorateExcitedAppendsExcitedMarker(t *testing.T) {
	const text = "すごい"
	m := psyche.Mood{Valence: 0.9, Arousal: 0.9}

	appended := false
	for seed := int64(0); seed < 50; seed++ {
		got := Decorate(text, m, rand.New(rand.NewSource(seed)))
		suffix := strings.TrimPrefix(got, text)
		if suffix == "" {
			continue
		}
		appended = true
		require.Contains(t, excitedMarkers, suffix,
			"any appended suffix for an excited mood must be an excited marker (seed %d)", seed)
	}
	require.True(t, appended, "at least one seed must produce an excited marker")
}

// TestDecorateNegativeStaysTerse verifies that a strongly negative mood only
// ever appends the downbeat marker or nothing at all.
func TestDecorateNegativeStaysTerse(t *testing.T) {
	const text = "つかれた"
	m := psyche.Mood{Valence: -0.9, Arousal: -0.2}

	for seed := int64(0); seed < 50; seed++ {
		got := Decorate(text, m, rand.New(rand.NewSource(seed)))
		suffix := strings.TrimPrefix(got, text)
		if suffix == "" {
			continue
		}
		require.Contains(t, downbeatMarkers, suffix,
			"a negative mood must only append a downbeat marker (seed %d)", seed)
	}
}

// TestDecorateNeutralCalmUnchanged verifies that a neutral, low-arousal mood
// returns the text unchanged for every seed.
func TestDecorateNeutralCalmUnchanged(t *testing.T) {
	const text = "ふつう"
	m := psyche.Mood{Valence: 0.0, Arousal: -0.5}
	for seed := int64(0); seed < 50; seed++ {
		got := Decorate(text, m, rand.New(rand.NewSource(seed)))
		require.Equal(t, text, got, "neutral calm mood must not decorate (seed %d)", seed)
	}
}
