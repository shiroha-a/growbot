package markov

import "context"

// Sentence-boundary sentinels and the delimiter used to join context
// surfaces into a chain prev_key.
const (
	BOS = "\x02" // beginning-of-sentence marker
	EOS = "\x03" // end-of-sentence marker
	sep = "\x1f" // delimiter joining context surfaces into a prev_key
)

// TokenBump is a single vocabulary increment.
type TokenBump struct {
	Surface string
	POS     string
}

// ChainBump is a single n-gram transition increment.
type ChainBump struct {
	PrevKey string
	Next    string
}

// Transition is a candidate next token with its observed count, current memory
// strength, and average engagement reward. The effective sampling weight is
// Count*Strength*(1+influence*Reward), so a forgotten (low-strength) transition
// is chosen less often and one that appeared in well-received posts is chosen
// more.
type Transition struct {
	Surface  string
	Count    int
	Strength float64
	Reward   float64 // average reward in [0,1] of posts that used this transition
}

// Stats summarizes the learned model size.
type Stats struct {
	Vocab  int
	Chains int
}

// Repo is the persistence backend for the Markov model.
//
// Nexts must return its candidates in a stable order (sorted by Surface).
// 生成側のサンプリングは候補の並び順に依存するため、決定的な生成のために
// すべての実装はこの順序保証を守る必要がある。
type Repo interface {
	ApplyLearn(ctx context.Context, tokens []TokenBump, chains []ChainBump) error
	Nexts(ctx context.Context, prevKey string) ([]Transition, error)
	Stats(ctx context.Context) (Stats, error)
	// RandomContext returns a random known prev_key, used to seed dream-like
	// generation. The boolean is false when no chains exist yet.
	RandomContext(ctx context.Context) (string, bool, error)
	// CreditChains adds an engagement reward to each given transition (its
	// reward_sum and reward_n), so generation can later favor phrasings that
	// were well received.
	CreditChains(ctx context.Context, chains []ChainBump, reward float64) error
	// RandomFrequentToken returns a random token of the given POS whose frequency
	// is at least minFreq, used to seed topical posts. False when none qualify.
	RandomFrequentToken(ctx context.Context, pos string, minFreq int) (string, bool, error)
}
