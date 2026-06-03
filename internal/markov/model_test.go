package markov

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"growbot/internal/morph"

	"github.com/stretchr/testify/require"
)

type rewardAcc struct {
	sum float64
	n   int
}

// fakeRepo is an in-memory implementation of Repo for tests. It accumulates
// transition counts keyed by prev_key and tracks the distinct vocabulary.
type fakeRepo struct {
	chains map[string]map[string]int       // prevKey -> next surface -> count
	reward map[string]map[string]rewardAcc // prevKey -> next surface -> reward acc
	vocab  map[string]struct{}             // distinct surface set
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		chains: make(map[string]map[string]int),
		reward: make(map[string]map[string]rewardAcc),
		vocab:  make(map[string]struct{}),
	}
}

func (f *fakeRepo) ApplyLearn(_ context.Context, tokens []TokenBump, chains []ChainBump) error {
	for _, t := range tokens {
		f.vocab[t.Surface] = struct{}{}
	}
	for _, c := range chains {
		row, ok := f.chains[c.PrevKey]
		if !ok {
			row = make(map[string]int)
			f.chains[c.PrevKey] = row
		}
		row[c.Next]++
	}
	return nil
}

func (f *fakeRepo) Nexts(_ context.Context, prevKey string) ([]Transition, error) {
	row, ok := f.chains[prevKey]
	if !ok {
		return nil, nil
	}
	out := make([]Transition, 0, len(row))
	for surface, count := range row {
		// 学習直後の strength は 1.0 相当(本物のSQLRepoのDEFAULT)。
		tr := Transition{Surface: surface, Count: count, Strength: 1.0}
		if rr, ok := f.reward[prevKey]; ok {
			if acc, ok := rr[surface]; ok && acc.n > 0 {
				tr.Reward = acc.sum / float64(acc.n)
			}
		}
		out = append(out, tr)
	}
	// Repo契約どおり候補を surface 昇順で安定化する(本物のSQLRepoと同様)。
	// マップ反復順に依存すると同一シードでも生成が揺れてテストがflakyになる。
	sort.Slice(out, func(i, j int) bool { return out[i].Surface < out[j].Surface })
	return out, nil
}

// RandomContext returns a deterministic key (the smallest) for test
// reproducibility; the real SQLRepo uses RANDOM().
func (f *fakeRepo) RandomContext(_ context.Context) (string, bool, error) {
	if len(f.chains) == 0 {
		return "", false, nil
	}
	keys := make([]string, 0, len(f.chains))
	for k := range f.chains {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys[0], true, nil
}

func (f *fakeRepo) Stats(_ context.Context) (Stats, error) {
	return Stats{Vocab: len(f.vocab), Chains: len(f.chains)}, nil
}

func (f *fakeRepo) CreditChains(_ context.Context, chains []ChainBump, reward float64) error {
	for _, c := range chains {
		row, ok := f.reward[c.PrevKey]
		if !ok {
			row = make(map[string]rewardAcc)
			f.reward[c.PrevKey] = row
		}
		acc := row[c.Next]
		acc.sum += reward
		acc.n++
		row[c.Next] = acc
	}
	return nil
}

func (f *fakeRepo) RandomFrequentToken(_ context.Context, _ string, _ int) (string, bool, error) {
	return "", false, nil
}

// tokens is a small helper to build a morph.Token slice from surfaces.
func tokens(surfaces ...string) []morph.Token {
	out := make([]morph.Token, 0, len(surfaces))
	for _, s := range surfaces {
		out = append(out, morph.Token{Surface: s, POS: "名詞", BaseForm: s})
	}
	return out
}

func TestLearnProducesExpectedChainBumps(t *testing.T) {
	repo := newFakeRepo()
	m := NewModel(repo, Options{Order: 2}, rand.New(rand.NewSource(1)))

	err := m.Learn(context.Background(), tokens("A", "B"))
	require.NoError(t, err)

	// order==2 では BOS->A, A->B, B->EOS の3遷移が生成される。
	require.Equal(t, 1, repo.chains[BOS]["A"])
	require.Equal(t, 1, repo.chains["A"]["B"])
	require.Equal(t, 1, repo.chains["B"][EOS])

	// 余計な遷移が無いことを確認する。
	require.Len(t, repo.chains, 3)
	require.Len(t, repo.chains[BOS], 1)
	require.Len(t, repo.chains["A"], 1)
	require.Len(t, repo.chains["B"], 1)
}

func TestLearnEmptyIsNoop(t *testing.T) {
	repo := newFakeRepo()
	m := NewModel(repo, Options{Order: 2}, rand.New(rand.NewSource(1)))

	require.NoError(t, m.Learn(context.Background(), nil))
	require.Empty(t, repo.chains)
	require.Empty(t, repo.vocab)
}

func TestGenerateDeterministicAndBounded(t *testing.T) {
	repo := newFakeRepo()
	// SEEDされた乱数で生成を決定的にする。
	m := NewModel(repo, Options{Order: 2, MinTokens: 1, MaxTokens: 40}, rand.New(rand.NewSource(1)))

	ctx := context.Background()
	trained := []string{"私", "は", "猫", "が", "好き", "です"}
	// 学習対象の語彙集合(終端判定用)。
	allowed := map[string]struct{}{}
	for _, s := range trained {
		allowed[s] = struct{}{}
	}

	require.NoError(t, m.Learn(ctx, tokens("私", "は", "猫", "が", "好き")))
	require.NoError(t, m.Learn(ctx, tokens("猫", "が", "好き", "です")))
	require.NoError(t, m.Learn(ctx, tokens("私", "は", "猫")))

	out, err := m.Generate(ctx)
	require.NoError(t, err)

	// 非空であり、学習した語の連結のみで構成されることを確認する。
	require.NotEmpty(t, out)

	// 連結結果を貪欲に分解し、学習語彙のみで構成されることを検証する。
	rest := out
	count := 0
	for len(rest) > 0 {
		matched := false
		for s := range allowed {
			if strings.HasPrefix(rest, s) {
				rest = rest[len(s):]
				matched = true
				count++
				break
			}
		}
		require.True(t, matched, "generated output contains an untrained fragment: %q (remaining %q)", out, rest)
	}

	// maxTokensを超えていないこと(終端していること)を確認する。
	require.LessOrEqual(t, count, 40)

	// セパレータやBOS/EOS等の制御文字が混入していないことを確認する。
	require.False(t, strings.ContainsAny(out, BOS+EOS+sep))
}

func TestGenerateRepeatableWithSameSeed(t *testing.T) {
	build := func() (*Model, context.Context) {
		repo := newFakeRepo()
		m := NewModel(repo, Options{Order: 2}, rand.New(rand.NewSource(7)))
		ctx := context.Background()
		_ = m.Learn(ctx, tokens("あ", "い", "う"))
		_ = m.Learn(ctx, tokens("あ", "う", "え"))
		return m, ctx
	}

	m1, ctx1 := build()
	m2, ctx2 := build()

	out1, err1 := m1.Generate(ctx1)
	out2, err2 := m2.Generate(ctx2)
	require.NoError(t, err1)
	require.NoError(t, err2)
	// 同一シードでは生成結果が一致する(決定的)。
	require.Equal(t, out1, out2)
}

func TestStatsDelegates(t *testing.T) {
	repo := newFakeRepo()
	m := NewModel(repo, Options{Order: 2}, rand.New(rand.NewSource(1)))
	ctx := context.Background()

	require.NoError(t, m.Learn(ctx, tokens("X", "Y")))

	s, err := m.Stats(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, s.Vocab)
	require.Equal(t, 3, s.Chains)
}

func TestOptionDefaults(t *testing.T) {
	repo := newFakeRepo()
	m := NewModel(repo, Options{Order: 0, MinTokens: 0, MaxTokens: 0}, nil)
	require.Equal(t, 2, m.order)
	require.Equal(t, 1, m.minTokens)
	require.Equal(t, 40, m.maxTokens)
	require.NotNil(t, m.rng)
}

// TestSampleWeightsByStrength verifies that a low-strength (forgotten)
// transition is chosen far less often than a high-strength one of equal count.
func TestSampleWeightsByStrength(t *testing.T) {
	m := NewModel(newFakeRepo(), Options{Order: 2}, rand.New(rand.NewSource(1)))
	nexts := []Transition{
		{Surface: "強", Count: 10, Strength: 1.0},
		{Surface: "弱", Count: 10, Strength: 0.1},
	}
	counts := map[string]int{}
	for i := 0; i < 2000; i++ {
		counts[m.sample(nexts)]++
	}
	// 重み 10 対 1 なので強が弱を大きく上回る。
	require.Greater(t, counts["強"], counts["弱"]*3)
	require.Positive(t, counts["弱"]) // ごく稀には選ばれる
}

// TestDreamProducesNoControlChars verifies that dream output never leaks the
// BOS/EOS/separator control bytes.
func TestDreamProducesNoControlChars(t *testing.T) {
	repo := newFakeRepo()
	m := NewModel(repo, Options{Order: 2, MinTokens: 1, MaxTokens: 20}, rand.New(rand.NewSource(3)))
	ctx := context.Background()
	require.NoError(t, m.Learn(ctx, tokens("空", "が", "青い")))
	require.NoError(t, m.Learn(ctx, tokens("海", "が", "広い")))

	out, err := m.Dream(ctx)
	require.NoError(t, err)
	require.False(t, strings.ContainsAny(out, BOS+EOS+sep))
}

// TestDreamEmptyModel verifies Dream is a safe no-op-ish on an unlearned model.
func TestDreamEmptyModel(t *testing.T) {
	m := NewModel(newFakeRepo(), Options{Order: 2}, rand.New(rand.NewSource(1)))
	out, err := m.Dream(context.Background())
	require.NoError(t, err)
	require.Equal(t, "", out)
}

// TestGenerateFromSeed verifies that seeded generation begins with the seed, an
// unknown seed returns itself, and an empty seed falls back to Generate.
func TestGenerateFromSeed(t *testing.T) {
	repo := newFakeRepo()
	m := NewModel(repo, Options{Order: 2, MinTokens: 1, MaxTokens: 20}, rand.New(rand.NewSource(2)))
	ctx := context.Background()
	require.NoError(t, m.Learn(ctx, tokens("猫", "が", "鳴く")))

	out, err := m.GenerateFromSeed(ctx, "猫")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "猫"))
	require.False(t, strings.ContainsAny(out, BOS+EOS+sep))

	// 未知の seed は seed 単体で返る。
	out2, err := m.GenerateFromSeed(ctx, "存在しない")
	require.NoError(t, err)
	require.Equal(t, "存在しない", out2)

	// 空 seed は通常生成にフォールバック(非空)。
	out3, err := m.GenerateFromSeed(ctx, "")
	require.NoError(t, err)
	require.NotEmpty(t, out3)
}

// TestSampleWeightsByReward verifies that a higher-reward transition is chosen
// more often than an equal-count, equal-strength one with no reward.
func TestSampleWeightsByReward(t *testing.T) {
	m := NewModel(newFakeRepo(), Options{Order: 2}, rand.New(rand.NewSource(1)))
	nexts := []Transition{
		{Surface: "good", Count: 10, Strength: 1.0, Reward: 1.0},
		{Surface: "meh", Count: 10, Strength: 1.0, Reward: 0.0},
	}
	counts := map[string]int{}
	for i := 0; i < 4000; i++ {
		counts[m.sample(nexts)]++
	}
	// good の重みは meh の2倍(1+1.0 対 1)なので明確に多く選ばれる。
	require.Greater(t, counts["good"], counts["meh"])
}

// TestCreditRaisesReward verifies that crediting a sentence's chains raises the
// reward Nexts reports for those transitions.
func TestCreditRaisesReward(t *testing.T) {
	repo := newFakeRepo()
	m := NewModel(repo, Options{Order: 2}, rand.New(rand.NewSource(1)))
	ctx := context.Background()
	require.NoError(t, m.Learn(ctx, tokens("猫", "が", "鳴く")))

	before, err := repo.Nexts(ctx, "猫")
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.InDelta(t, 0.0, before[0].Reward, 1e-9)

	require.NoError(t, m.Credit(ctx, tokens("猫", "が", "鳴く"), 0.8))
	after, err := repo.Nexts(ctx, "猫")
	require.NoError(t, err)
	require.InDelta(t, 0.8, after[0].Reward, 1e-9)
}
