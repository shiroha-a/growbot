package learning

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRewardZeroEngagement verifies that no engagement yields exactly zero
// reward.
func TestRewardZeroEngagement(t *testing.T) {
	require.Equal(t, 0.0, Reward(0, 0, 0))
}

// TestRewardClampsNegativeInputs verifies that negative counts are treated as
// zero, so a fully-negative input is indistinguishable from no engagement.
func TestRewardClampsNegativeInputs(t *testing.T) {
	require.Equal(t, 0.0, Reward(-5, -3, -1))
	// 一部だけ負の場合も負の寄与は 0 に丸められる。
	require.Equal(t, Reward(0, 1, 0), Reward(-2, 1, -4))
}

// TestRewardBoundedInUnitInterval verifies that the reward always lies in [0,1)
// even for very large engagement.
func TestRewardBoundedInUnitInterval(t *testing.T) {
	cases := [][3]int{
		{0, 0, 0},
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
		{10, 10, 10},
		{1000, 1000, 1000},
	}
	for _, c := range cases {
		r := Reward(c[0], c[1], c[2])
		require.GreaterOrEqual(t, r, 0.0)
		require.Less(t, r, 1.0)
	}
}

// TestRewardMonotonicIncrease verifies that adding engagement never decreases
// the reward and strictly increases it from a baseline.
func TestRewardMonotonicIncrease(t *testing.T) {
	base := Reward(1, 1, 1)
	more := Reward(2, 2, 2)
	require.Greater(t, more, base)

	// 各次元の増加が単調に reward を増やすこと。
	require.Greater(t, Reward(2, 0, 0), Reward(1, 0, 0))
	require.Greater(t, Reward(0, 2, 0), Reward(0, 1, 0))
	require.Greater(t, Reward(0, 0, 2), Reward(0, 0, 1))
}

// TestRewardRepliesWeightedMost verifies the weighting order: a single reply
// (weight 3) outranks a single reaction (weight 2), which outranks a single
// renote (weight 1).
func TestRewardRepliesWeightedMost(t *testing.T) {
	reply := Reward(0, 1, 0)
	reaction := Reward(1, 0, 0)
	renote := Reward(0, 0, 1)

	require.Greater(t, reply, reaction)
	require.Greater(t, reaction, renote)
}
