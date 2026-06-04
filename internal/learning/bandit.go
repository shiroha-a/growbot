package learning

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Bandit is a Thompson-sampling multi-armed bandit over named arms.
//
// Each arm is modelled by a Beta(alpha,beta) posterior persisted in the
// bandit_arms table. Selecting an arm samples a value from each arm's posterior
// and returns the arm with the largest sample, so arms that have earned more
// reward are explored more often while uncertain arms still get a chance.
type Bandit struct {
	db  *sql.DB
	rng *rand.Rand
	mu  sync.Mutex
}

// NewBandit returns a Bandit backed by db using rng as its source of
// randomness. When rng is nil a time-seeded generator is created so the bandit
// is usable out of the box.
func NewBandit(db *sql.DB, rng *rand.Rand) *Bandit {
	if rng == nil {
		// rng 未指定時は時刻シードで決定論性を諦める代わりに即利用可能にする。
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &Bandit{db: db, rng: rng}
}

// Select chooses one of arms by Thompson sampling and returns it.
//
// Every supplied arm is first ensured to have a row in bandit_arms (created with
// the uniform Beta(1,1) prior when absent). A Beta sample is drawn from each
// arm's posterior and the arm with the maximum sample is returned. It errors
// when arms is empty or the backing db is nil.
func (b *Bandit) Select(ctx context.Context, arms []string) (string, error) {
	if b == nil || b.db == nil {
		return "", errors.New("learning: nil bandit")
	}
	if len(arms) == 0 {
		return "", errors.New("learning: no arms to select")
	}

	best := ""
	bestTheta := math.Inf(-1)
	for _, arm := range arms {
		if _, err := b.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO bandit_arms(arm) VALUES(?);`, arm,
		); err != nil {
			return "", fmt.Errorf("learning: ensure arm %q: %w", arm, err)
		}

		var alpha, beta float64
		if err := b.db.QueryRowContext(ctx,
			`SELECT alpha, beta FROM bandit_arms WHERE arm = ?;`, arm,
		).Scan(&alpha, &beta); err != nil {
			return "", fmt.Errorf("learning: read arm %q: %w", arm, err)
		}

		theta := b.sampleBeta(alpha, beta)
		if theta > bestTheta {
			bestTheta = theta
			best = arm
		}
	}
	return best, nil
}

// Update folds reward (clamped to [0,1]) into arm's posterior.
//
// The arm row is created when absent, then alpha grows by reward and beta by
// (1-reward), shifting the posterior toward success or failure proportionally.
// pulls and reward_sum accumulate for bookkeeping.
func (b *Bandit) Update(ctx context.Context, arm string, reward float64) error {
	if b == nil || b.db == nil {
		return errors.New("learning: nil bandit")
	}

	if reward < 0 {
		reward = 0
	}
	if reward > 1 {
		reward = 1
	}

	if _, err := b.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO bandit_arms(arm) VALUES(?);`, arm,
	); err != nil {
		return fmt.Errorf("learning: ensure arm %q: %w", arm, err)
	}

	if _, err := b.db.ExecContext(ctx,
		`UPDATE bandit_arms
		 SET alpha = alpha + ?,
		     beta = beta + (1 - ?),
		     pulls = pulls + 1,
		     reward_sum = reward_sum + ?
		 WHERE arm = ?;`,
		reward, reward, reward, arm,
	); err != nil {
		return fmt.Errorf("learning: update arm %q: %w", arm, err)
	}
	return nil
}

// sampleBeta draws a sample from Beta(alpha,beta) using the ratio of two Gamma
// samples. The bandit always keeps alpha,beta >= 1, so the Marsaglia-Tsang
// generator (valid for shape >= 1) applies directly. A degenerate denominator
// falls back to 0.5 so Select never produces NaN.
func (b *Bandit) sampleBeta(alpha, beta float64) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	ga := gammaSample(alpha, b.rng)
	gb := gammaSample(beta, b.rng)
	if ga+gb <= 0 {
		return 0.5
	}
	return ga / (ga + gb)
}

// gammaSample draws a sample from Gamma(shape,1) for shape >= 1 using the
// Marsaglia-Tsang method.
func gammaSample(shape float64, r *rand.Rand) float64 {
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)
	for {
		x := r.NormFloat64()
		v := 1.0 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := r.Float64()
		if u < 1.0-0.0331*x*x*x*x {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return d * v
		}
	}
}
