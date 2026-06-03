package seed

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSentencesNonEmpty verifies that the seed corpus yields a meaningful
// baseline vocabulary: at least 40 entries, none of which are empty.
func TestSentencesNonEmpty(t *testing.T) {
	got := Sentences()
	require.GreaterOrEqual(t, len(got), 40, "seed corpus must contain at least 40 sentences")
	for i, s := range got {
		require.NotEmpty(t, s, "sentence at index %d must not be empty", i)
		require.Equal(t, s, strings.TrimSpace(s), "sentence at index %d must already be trimmed", i)
	}
}

// TestSentencesNoNewlines verifies that splitting was performed correctly and
// no entry contains an embedded newline character.
func TestSentencesNoNewlines(t *testing.T) {
	for i, s := range Sentences() {
		require.NotContains(t, s, "\n", "sentence at index %d must not contain a newline", i)
		require.NotContains(t, s, "\r", "sentence at index %d must not contain a carriage return", i)
	}
}

// TestSentencesNoDuplicates verifies that the corpus has no repeated entries so
// the Markov chains are not biased by accidental duplication.
func TestSentencesNoDuplicates(t *testing.T) {
	got := Sentences()
	seen := make(map[string]struct{}, len(got))
	for _, s := range got {
		_, dup := seen[s]
		require.False(t, dup, "duplicate sentence found: %q", s)
		seen[s] = struct{}{}
	}
}
