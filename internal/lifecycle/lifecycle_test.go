package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const day = 24 * time.Hour

func TestEvaluateNewborn(t *testing.T) {
	// A newborn starts as a child speaker, never an infant.
	got := Evaluate(0, 0)
	require.Equal(t, StageChild, got)
	require.Equal(t, 12, got.Params().MaxTokens)
}

func TestEvaluateRequiresBothThresholds(t *testing.T) {
	tests := []struct {
		name  string
		age   time.Duration
		vocab int
		want  Stage
	}{
		{name: "age and vocab met for adolescent", age: 5 * day, vocab: 500, want: StageAdolescent},
		{name: "age met but vocab low stays child", age: 5 * day, vocab: 100, want: StageChild},
		{name: "vocab met but age low stays child", age: 1 * day, vocab: 500, want: StageChild},
		{name: "young adult both met", age: 20 * day, vocab: 1500, want: StageYoungAdult},
		{name: "young adult vocab short falls to adolescent", age: 20 * day, vocab: 500, want: StageAdolescent},
		{name: "mature both met", age: 100 * day, vocab: 5000, want: StageMature},
		{name: "mature age met but vocab below adolescent stays child", age: 100 * day, vocab: 200, want: StageChild},
		{name: "mature age met vocab adolescent level", age: 100 * day, vocab: 1500, want: StageYoungAdult},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Evaluate(tt.age, tt.vocab))
		})
	}
}

func TestEvaluateBoundaryThresholds(t *testing.T) {
	// Exact threshold values must unlock their stage (inclusive comparisons).
	require.Equal(t, StageAdolescent, Evaluate(3*day, 300))
	require.Equal(t, StageYoungAdult, Evaluate(14*day, 1000))
	require.Equal(t, StageMature, Evaluate(60*day, 3000))

	// One unit below either threshold must not unlock the stage.
	require.Equal(t, StageChild, Evaluate(3*day-time.Nanosecond, 300))
	require.Equal(t, StageChild, Evaluate(3*day, 299))
}

func TestEvaluateNegativeAge(t *testing.T) {
	// Negative age is treated as zero, so vocabulary alone cannot advance.
	require.Equal(t, StageChild, Evaluate(-100*day, 10000))
}

func TestStageParams(t *testing.T) {
	tests := []struct {
		stage Stage
		want  Params
	}{
		{StageChild, Params{MinTokens: 2, MaxTokens: 12}},
		{StageAdolescent, Params{MinTokens: 3, MaxTokens: 20}},
		{StageYoungAdult, Params{MinTokens: 4, MaxTokens: 30}},
		{StageMature, Params{MinTokens: 5, MaxTokens: 40}},
	}
	for _, tt := range tests {
		t.Run(tt.stage.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.stage.Params())
		})
	}
}

func TestStageString(t *testing.T) {
	require.Equal(t, "child", StageChild.String())
	require.Equal(t, "adolescent", StageAdolescent.String())
	require.Equal(t, "young_adult", StageYoungAdult.String())
	require.Equal(t, "mature", StageMature.String())
	require.Equal(t, "unknown", Stage(99).String())
}

func TestStageOrdering(t *testing.T) {
	// Stages must be monotonically ordered so they can be compared directly.
	require.True(t, StageChild < StageAdolescent)
	require.True(t, StageAdolescent < StageYoungAdult)
	require.True(t, StageYoungAdult < StageMature)
}
