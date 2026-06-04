// Package markov implements a fully-local, LLM-free n-gram (Markov chain)
// language model. It learns vocabulary and transition counts from tokenized
// Japanese text and generates new sentences by weighted sampling. Persistence
// is delegated to a Repo so the model itself stays storage-agnostic.
package markov

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"time"

	"growbot/internal/morph"
)

// Options configures the behaviour of a Model.
type Options struct {
	// Order is the n-gram order (n). The transition context is Order-1 tokens.
	// Values below 2 are clamped to 2.
	Order int
	// MinTokens is the minimum number of surface tokens a generated sentence
	// should contain before an end-of-sentence marker is honoured. Values below
	// 1 are clamped to 1.
	MinTokens int
	// MaxTokens is the hard upper bound on generated surface tokens, guarding
	// against runaway loops. Non-positive values are clamped to 40.
	MaxTokens int
}

// Model is an n-gram language model backed by a Repo. Its rng is guarded by mu,
// so Generate is safe to call concurrently; but the length bounds (SetBounds)
// and generation must share one goroutine, as the bounds are not synchronized.
type Model struct {
	repo      Repo
	order     int
	minTokens int
	maxTokens int
	rng       *rand.Rand
	// mu guards rng so concurrent Generate calls do not race on the shared
	// random source. 乱数生成器はスレッドセーフではないため排他制御する。
	mu sync.Mutex
}

// NewModel constructs a Model from a Repo, Options and an optional rng.
// When rng is nil a time-seeded source is used. Options are normalized so
// that invalid values fall back to sane defaults.
func NewModel(repo Repo, opts Options, rng *rand.Rand) *Model {
	order := opts.Order
	if order < 2 {
		order = 2
	}
	minTokens := opts.MinTokens
	if minTokens < 1 {
		minTokens = 1
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 40
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &Model{
		repo:      repo,
		order:     order,
		minTokens: minTokens,
		maxTokens: maxTokens,
		rng:       rng,
	}
}

// Learn ingests a tokenized sentence, producing vocabulary and transition
// increments which are persisted via the Repo. An empty input is a no-op.
func (m *Model) Learn(ctx context.Context, tokens []morph.Token) error {
	if len(tokens) == 0 {
		return nil
	}

	tokenBumps := make([]TokenBump, 0, len(tokens))
	surfaces := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tokenBumps = append(tokenBumps, TokenBump{Surface: tok.Surface, POS: tok.POS})
		surfaces = append(surfaces, tok.Surface)
	}

	return m.repo.ApplyLearn(ctx, tokenBumps, m.chainBumps(surfaces))
}

// chainBumps builds the n-gram transitions for a sentence's surfaces, padding
// with Order-1 leading BOS and a trailing EOS so BOS->first ... last->EOS are
// all captured.
func (m *Model) chainBumps(surfaces []string) []ChainBump {
	// 文頭にOrder-1個のBOSを、文末にEOSを付与したパディング列を作る。
	padded := make([]string, 0, (m.order-1)+len(surfaces)+1)
	for i := 0; i < m.order-1; i++ {
		padded = append(padded, BOS)
	}
	padded = append(padded, surfaces...)
	padded = append(padded, EOS)

	chainBumps := make([]ChainBump, 0, len(surfaces)+1)
	for i := 0; i <= len(surfaces); i++ {
		prevKey := strings.Join(padded[i:i+m.order-1], sep)
		next := padded[i+m.order-1]
		chainBumps = append(chainBumps, ChainBump{PrevKey: prevKey, Next: next})
	}
	return chainBumps
}

// Credit assigns an engagement reward to the transitions a posted sentence used,
// so phrasings from well-received posts gain sampling weight. Empty input is a
// no-op.
func (m *Model) Credit(ctx context.Context, tokens []morph.Token, reward float64) error {
	if len(tokens) == 0 {
		return nil
	}
	// 報酬は [0,1] に収め、保存される平均報酬がドキュメントの不変条件を満たすようにする。
	if reward < 0 {
		reward = 0
	} else if reward > 1 {
		reward = 1
	}
	surfaces := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		surfaces = append(surfaces, tok.Surface)
	}
	return m.repo.CreditChains(ctx, m.chainBumps(surfaces), reward)
}

// FrequentToken returns a random token of the given POS whose frequency is at
// least minFreq (e.g. a noun to seed a topical post, or a particle to seed a
// verbal tic), or ("", false) when none qualify.
func (m *Model) FrequentToken(ctx context.Context, pos string, minFreq int) (string, bool, error) {
	return m.repo.RandomFrequentToken(ctx, pos, minFreq)
}

// SetBounds updates the generation length bounds, e.g. as the bot matures. It is
// intended to be called from the same goroutine that generates.
func (m *Model) SetBounds(minTokens, maxTokens int) {
	if minTokens < 1 {
		minTokens = 1
	}
	if maxTokens < minTokens {
		maxTokens = minTokens
	}
	m.minTokens = minTokens
	m.maxTokens = maxTokens
}

// Generate produces a new sentence by repeatedly sampling the next token from
// the learned transition distribution, starting from the sentence beginning. It
// returns the concatenation of the sampled surfaces (Japanese has no inter-word
// spacing). Emptiness is not an error: the caller receives whatever was
// produced.
func (m *Model) Generate(ctx context.Context) (string, error) {
	return m.generate(ctx, m.startWindow())
}

// startWindow returns the initial context window for the start of a sentence
// (all BOS markers).
func (m *Model) startWindow() []string {
	window := make([]string, m.order-1)
	for i := range window {
		window[i] = BOS
	}
	return window
}

// generate walks the Markov chain from the given initial context window. The
// window slice is mutated in place, so callers must pass a fresh copy.
func (m *Model) generate(ctx context.Context, window []string) (string, error) {
	out := make([]string, 0, m.maxTokens)
	for i := 0; i < m.maxTokens; i++ {
		// コンテキストのキャンセルを各反復で尊重し、途中までの文を返す。
		if err := ctx.Err(); err != nil {
			return strings.Join(out, ""), err
		}
		prevKey := strings.Join(window, sep)
		nexts, err := m.repo.Nexts(ctx, prevKey)
		if err != nil {
			return "", err
		}
		if len(nexts) == 0 {
			break
		}

		surface := m.sample(nexts)
		if surface == EOS {
			// 十分な長さに達していれば文を確定する。
			if len(out) >= m.minTokens {
				break
			}
			// まだ短い場合はEOS以外の候補から再サンプルして文を継続する。
			if hasNonEOS(nexts) {
				surface = m.sampleNonEOS(nexts)
			} else {
				break
			}
		}

		out = append(out, surface)
		// ウィンドウをスライドさせる(先頭を捨て末尾に追加)。Order==2では1要素。
		if len(window) > 0 {
			copy(window, window[1:])
			window[len(window)-1] = surface
		}
	}

	return strings.Join(out, ""), nil
}

// Dream produces a surreal recombination of memories: it stitches together a
// few short fragments, each seeded from a random remembered context rather than
// the sentence start, so the result reads like a half-remembered collage. This
// is the kind of thing the bot "says" on waking. An empty model yields "".
//
// Unlike Generate, Dream's seed contexts come from the repository's
// RandomContext (database-side randomness), so its output is intentionally not
// reproducible from the model rng.
func (m *Model) Dream(ctx context.Context) (string, error) {
	const fragments = 3
	var b strings.Builder
	for i := 0; i < fragments; i++ {
		if err := ctx.Err(); err != nil {
			return b.String(), err
		}
		window, err := m.dreamWindow(ctx)
		if err != nil {
			return "", err
		}
		frag, err := m.generate(ctx, window)
		if err != nil {
			return "", err
		}
		b.WriteString(frag)
	}
	return b.String(), nil
}

// dreamWindow builds an initial context window from a random remembered context,
// falling back to the sentence start when the model has no chains yet. The
// returned window is always order-1 long and safe to mutate.
func (m *Model) dreamWindow(ctx context.Context) ([]string, error) {
	prevKey, ok, err := m.repo.RandomContext(ctx)
	if err != nil {
		return nil, err
	}
	window := m.startWindow()
	if !ok {
		return window, nil
	}
	// prev_key は order-1 個の表層をsepで連結したもの。末尾合わせで埋め、
	// 想定外の長さでも境界を越えないよう安全に補正する。
	parts := strings.Split(prevKey, sep)
	if n := len(parts); n > len(window) {
		parts = parts[n-len(window):]
	}
	copy(window[len(window)-len(parts):], parts)
	return window, nil
}

// GenerateFromSeed generates a sentence that begins with the given seed surface
// and continues from what the model has learned to follow it, so a reply can be
// steered toward a keyword from an incoming message. When the seed has no known
// continuation the seed is returned on its own; an empty seed falls back to
// Generate.
//
// Steering is effective at order 2 (the default), where the seed is the full
// context. At higher orders the seed is BOS-padded and rarely matches a learned
// mid-sentence context, so it usually degrades to returning the seed alone.
func (m *Model) GenerateFromSeed(ctx context.Context, seed string) (string, error) {
	if seed == "" {
		return m.Generate(ctx)
	}
	// ウィンドウ末尾を seed にして、その直後から続きを生成する。
	window := m.startWindow()
	window[len(window)-1] = seed
	cont, err := m.generate(ctx, window)
	if err != nil {
		return "", err
	}
	return seed + cont, nil
}

// Stats reports the learned model size by delegating to the Repo.
func (m *Model) Stats(ctx context.Context) (Stats, error) {
	return m.repo.Stats(ctx)
}

// rewardInfluence scales how strongly engagement reward boosts a transition's
// sampling weight; a transition with average reward 1.0 is weighted (1+influence)x.
const rewardInfluence = 1.0

// chainWeight returns the effective sampling weight of a transition: its observed
// count scaled by memory strength and engagement reward, so a forgotten chain is
// chosen less often and one from well-received posts is chosen more. A
// non-positive count is weightless.
func chainWeight(t Transition) float64 {
	if t.Count <= 0 {
		return 0
	}
	s := t.Strength
	if s < 0 {
		s = 0
	}
	reward := t.Reward
	if reward < 0 {
		reward = 0
	} else if reward > 1 {
		reward = 1
	}
	return float64(t.Count) * s * (1 + rewardInfluence*reward)
}

// sample performs weighted random selection over the candidate transitions,
// proportional to chainWeight (count*strength*(1+influence*reward)). It is
// deterministic given the rng. When the total weight is non-positive it falls
// back to a uniform pick so it never returns an empty surface for a non-empty
// candidate list.
func (m *Model) sample(nexts []Transition) string {
	if len(nexts) == 0 {
		return ""
	}

	total := 0.0
	for _, t := range nexts {
		total += chainWeight(t)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if total <= 0 {
		// 重みが全て0(未学習相当)なら一様に選ぶ。
		return nexts[m.rng.Intn(len(nexts))].Surface
	}

	r := m.rng.Float64() * total
	for _, t := range nexts {
		w := chainWeight(t)
		if w <= 0 {
			continue
		}
		r -= w
		if r < 0 {
			return t.Surface
		}
	}
	// 浮動小数の丸めで稀に到達しうるため、保険として最後の候補を返す。
	return nexts[len(nexts)-1].Surface
}

// sampleNonEOS performs the same weighted selection as sample but restricts the
// candidate set to non-EOS surfaces. The caller must ensure at least one such
// candidate exists.
func (m *Model) sampleNonEOS(nexts []Transition) string {
	filtered := make([]Transition, 0, len(nexts))
	for _, t := range nexts {
		if t.Surface != EOS {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return EOS
	}
	return m.sample(filtered)
}

// hasNonEOS reports whether the candidate list contains any non-EOS surface.
func hasNonEOS(nexts []Transition) bool {
	for _, t := range nexts {
		if t.Surface != EOS {
			return true
		}
	}
	return false
}
