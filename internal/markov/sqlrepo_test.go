package markov

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"growbot/internal/store"

	"github.com/stretchr/testify/require"
)

// openRepo opens a real migrated SQLite database in a temp dir and wraps it as
// an SQLRepo for use in tests.
func openRepo(t *testing.T) (*SQLRepo, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLRepo(db), ctx
}

// TestInferOrder verifies the n-gram order is recovered from a stored prev_key's
// separator count, and that a fresh database reports no order.
func TestInferOrder(t *testing.T) {
	t.Run("fresh database reports no order", func(t *testing.T) {
		repo, ctx := openRepo(t)
		_, ok, err := repo.InferOrder(ctx)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("order 2 prev_key has no separator", func(t *testing.T) {
		repo, ctx := openRepo(t)
		require.NoError(t, repo.ApplyLearn(ctx, nil, []ChainBump{{PrevKey: "猫", Next: "歩く"}}))
		order, ok, err := repo.InferOrder(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, 2, order)
	})

	t.Run("order 3 prev_key has one separator", func(t *testing.T) {
		repo, ctx := openRepo(t)
		key := "猫" + sep + "が"
		require.NoError(t, repo.ApplyLearn(ctx, nil, []ChainBump{{PrevKey: key, Next: "歩く"}}))
		order, ok, err := repo.InferOrder(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, 3, order)
	})
}

// TestApplyLearnNextsAndStats verifies that ApplyLearn persists tokens and
// chains, that Nexts returns the observed transitions with their counts, and
// that Stats reflects the table sizes.
func TestApplyLearnNextsAndStats(t *testing.T) {
	repo, ctx := openRepo(t)

	tokens := []TokenBump{
		{Surface: "猫", POS: "名詞"},
		{Surface: "歩く", POS: "動詞"},
	}
	chains := []ChainBump{
		{PrevKey: BOS, Next: "猫"},
		{PrevKey: "猫", Next: "歩く"},
		{PrevKey: "歩く", Next: EOS},
	}

	require.NoError(t, repo.ApplyLearn(ctx, tokens, chains))

	// "猫" の次は "歩く" が1回観測されているはず。
	nexts, err := repo.Nexts(ctx, "猫")
	require.NoError(t, err)
	require.Len(t, nexts, 1)
	require.Equal(t, "歩く", nexts[0].Surface)
	require.Equal(t, 1, nexts[0].Count)

	stats, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, stats.Vocab)
	require.Equal(t, 3, stats.Chains)
}

// TestApplyLearnIncrementsCount verifies that re-learning the same ChainBump
// (and TokenBump) increments the stored count rather than inserting a duplicate.
func TestApplyLearnIncrementsCount(t *testing.T) {
	repo, ctx := openRepo(t)

	tokens := []TokenBump{{Surface: "猫", POS: "名詞"}}
	chains := []ChainBump{{PrevKey: "猫", Next: "歩く"}}

	require.NoError(t, repo.ApplyLearn(ctx, tokens, chains))
	require.NoError(t, repo.ApplyLearn(ctx, tokens, chains))

	nexts, err := repo.Nexts(ctx, "猫")
	require.NoError(t, err)
	require.Len(t, nexts, 1)
	require.Equal(t, "歩く", nexts[0].Surface)
	// 同一遷移を2回学習したので count は2になる。
	require.Equal(t, 2, nexts[0].Count)

	// 重複しても語彙・チェインの行数は増えない(UPSERTのため)。
	stats, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Vocab)
	require.Equal(t, 1, stats.Chains)
}

// TestNextsMultipleCandidates verifies that all candidates for a prev_key are
// returned with their individual counts.
func TestNextsMultipleCandidates(t *testing.T) {
	repo, ctx := openRepo(t)

	chains := []ChainBump{
		{PrevKey: "猫", Next: "歩く"},
		{PrevKey: "猫", Next: "走る"},
		{PrevKey: "猫", Next: "走る"},
	}
	require.NoError(t, repo.ApplyLearn(ctx, nil, chains))

	nexts, err := repo.Nexts(ctx, "猫")
	require.NoError(t, err)
	require.Len(t, nexts, 2)

	// 行順序は保証されないため、surface でソートしてから検証する。
	sort.Slice(nexts, func(i, j int) bool { return nexts[i].Surface < nexts[j].Surface })
	got := map[string]int{}
	for _, n := range nexts {
		got[n.Surface] = n.Count
	}
	require.Equal(t, 1, got["歩く"])
	require.Equal(t, 2, got["走る"])
}

// TestNextsUnknownKey verifies that an unknown prev_key yields an empty result
// and no error.
func TestNextsUnknownKey(t *testing.T) {
	repo, ctx := openRepo(t)

	nexts, err := repo.Nexts(ctx, "存在しない")
	require.NoError(t, err)
	require.Empty(t, nexts)
}

// TestApplyLearnEmptyNoop verifies that an empty learn batch is a no-op and
// leaves the model empty.
func TestApplyLearnEmptyNoop(t *testing.T) {
	repo, ctx := openRepo(t)

	require.NoError(t, repo.ApplyLearn(ctx, nil, nil))

	stats, err := repo.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, stats.Vocab)
	require.Equal(t, 0, stats.Chains)
}

// TestNextsReturnsDefaultStrength verifies a freshly-learned chain reports the
// default strength of 1.0.
func TestNextsReturnsDefaultStrength(t *testing.T) {
	repo, ctx := openRepo(t)
	require.NoError(t, repo.ApplyLearn(ctx, nil, []ChainBump{{PrevKey: "猫", Next: "歩く"}}))

	nexts, err := repo.Nexts(ctx, "猫")
	require.NoError(t, err)
	require.Len(t, nexts, 1)
	require.InDelta(t, 1.0, nexts[0].Strength, 1e-9)
}

// TestRandomContext verifies absence on an empty model and a known key once a
// chain exists.
func TestRandomContext(t *testing.T) {
	repo, ctx := openRepo(t)

	_, ok, err := repo.RandomContext(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, repo.ApplyLearn(ctx, nil, []ChainBump{{PrevKey: "猫", Next: "歩く"}}))
	key, ok, err := repo.RandomContext(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "猫", key)
}

// TestCreditChains verifies CreditChains raises the average reward Nexts reports.
func TestCreditChains(t *testing.T) {
	repo, ctx := openRepo(t)
	require.NoError(t, repo.ApplyLearn(ctx, nil, []ChainBump{{PrevKey: "猫", Next: "歩く"}}))

	require.NoError(t, repo.CreditChains(ctx, []ChainBump{{PrevKey: "猫", Next: "歩く"}}, 0.5))
	require.NoError(t, repo.CreditChains(ctx, []ChainBump{{PrevKey: "猫", Next: "歩く"}}, 1.0))

	nexts, err := repo.Nexts(ctx, "猫")
	require.NoError(t, err)
	require.Len(t, nexts, 1)
	// reward_sum=1.5, reward_n=2 -> 平均 0.75。
	require.InDelta(t, 0.75, nexts[0].Reward, 1e-9)
}

// TestRandomFrequentToken verifies POS + frequency filtering.
func TestRandomFrequentToken(t *testing.T) {
	repo, ctx := openRepo(t)
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.ApplyLearn(ctx, []TokenBump{{Surface: "猫", POS: "名詞"}}, nil))
	}
	require.NoError(t, repo.ApplyLearn(ctx, []TokenBump{{Surface: "が", POS: "助詞"}}, nil))

	got, ok, err := repo.RandomFrequentToken(ctx, "名詞", 2)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "猫", got)

	// freq閾値を満たす名詞が無ければ false。
	_, ok, err = repo.RandomFrequentToken(ctx, "名詞", 10)
	require.NoError(t, err)
	require.False(t, ok)
}
