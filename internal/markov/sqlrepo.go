package markov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SQLRepo is a SQLite-backed implementation of Repo. It persists vocabulary in
// the tokens table and n-gram transitions in the chains table, both created by
// the 0002_markov.sql migration.
type SQLRepo struct {
	db *sql.DB
}

// NewSQLRepo wraps an open *sql.DB as a Repo. The database is expected to have
// already had its migrations applied (e.g. via store.Open).
func NewSQLRepo(db *sql.DB) *SQLRepo {
	return &SQLRepo{db: db}
}

// Compile-time assertion that SQLRepo satisfies the Repo interface.
var _ Repo = (*SQLRepo)(nil)

// InferOrder reports the n-gram order the stored chains were built at.
//
// A prev_key holds Order-1 context surfaces joined by sep, i.e. Order-2
// separators, so Order = separators + 2. The boolean is false when no chains
// exist yet (a fresh database), in which case the caller should fall back to
// the configured order. This lets the daemon stay consistent with its own data
// even if MARKOV_ORDER is later changed, instead of silently generating empty
// output because the start context no longer matches any stored prev_key.
//
// It assumes the database was built at a single order and that surfaces never
// contain sep. The whole markov package already relies on that invariant when
// joining context surfaces into a prev_key (sep is the non-printable \x1f unit
// separator, which does not occur in morphological surfaces).
func (r *SQLRepo) InferOrder(ctx context.Context) (int, bool, error) {
	var prevKey string
	err := r.db.QueryRowContext(ctx, `SELECT prev_key FROM chains LIMIT 1;`).Scan(&prevKey)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("markov: infer order: %w", err)
	}
	return strings.Count(prevKey, sep) + 2, true, nil
}

// ApplyLearn applies a batch of token and chain increments atomically.
//
// Every bump is written inside a single transaction using prepared statements,
// so a single learn observation either lands in full or not at all. Tokens are
// upserted (freq incremented, pos refreshed, last_used touched) and chains are
// upserted (count incremented, last_used touched). Any error rolls the whole
// transaction back. The supplied context governs every statement.
func (r *SQLRepo) ApplyLearn(ctx context.Context, tokens []TokenBump, chains []ChainBump) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("markov: nil repo")
	}
	// 学習対象が空ならトランザクションを開かずに早期リターンする。
	if len(tokens) == 0 && len(chains) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("markov: begin tx: %w", err)
	}
	// commit に到達しなかった場合に確実にロールバックする。
	// commit 済みなら Rollback は sql.ErrTxDone を返すだけで無害。
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if len(tokens) > 0 {
		tokenStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO tokens (surface, pos, freq, last_used)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(surface) DO UPDATE SET
				freq = freq + 1,
				pos = CASE WHEN excluded.pos <> '' THEN excluded.pos ELSE tokens.pos END,
				last_used = CURRENT_TIMESTAMP;
		`)
		if err != nil {
			return fmt.Errorf("markov: prepare token upsert: %w", err)
		}
		defer tokenStmt.Close()

		for _, tb := range tokens {
			if _, err := tokenStmt.ExecContext(ctx, tb.Surface, tb.POS); err != nil {
				return fmt.Errorf("markov: upsert token %q: %w", tb.Surface, err)
			}
		}
	}

	if len(chains) > 0 {
		chainStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO chains (prev_key, next_surface, count, last_used)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(prev_key, next_surface) DO UPDATE SET
				count = count + 1,
				last_used = CURRENT_TIMESTAMP;
		`)
		if err != nil {
			return fmt.Errorf("markov: prepare chain upsert: %w", err)
		}
		defer chainStmt.Close()

		for _, cb := range chains {
			if _, err := chainStmt.ExecContext(ctx, cb.PrevKey, cb.Next); err != nil {
				return fmt.Errorf("markov: upsert chain %q->%q: %w", cb.PrevKey, cb.Next, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("markov: commit: %w", err)
	}
	committed = true
	return nil
}

// Nexts returns every candidate next token observed after prevKey along with
// its accumulated count, ordered by surface. An unknown prevKey yields an empty
// slice and no error.
//
// 候補は next_surface 昇順で安定的に返す。生成側の重み付きサンプリングは
// 候補の並び順に依存するため、同一シードで再現可能な生成を保証するには
// この順序の安定性が必要になる。
func (r *SQLRepo) Nexts(ctx context.Context, prevKey string) ([]Transition, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("markov: nil repo")
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT next_surface, count, strength,
		        CASE WHEN reward_n > 0 THEN reward_sum / reward_n ELSE 0 END AS reward
		 FROM chains WHERE prev_key = ? ORDER BY next_surface;`, prevKey)
	if err != nil {
		return nil, fmt.Errorf("markov: query nexts: %w", err)
	}
	defer rows.Close()

	var out []Transition
	for rows.Next() {
		var t Transition
		if err := rows.Scan(&t.Surface, &t.Count, &t.Strength, &t.Reward); err != nil {
			return nil, fmt.Errorf("markov: scan next: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("markov: iterate nexts: %w", err)
	}
	return out, nil
}

// Stats reports the current vocabulary and chain table sizes.
func (r *SQLRepo) Stats(ctx context.Context) (Stats, error) {
	if r == nil || r.db == nil {
		return Stats{}, fmt.Errorf("markov: nil repo")
	}

	var s Stats
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM tokens;`).Scan(&s.Vocab); err != nil {
		return Stats{}, fmt.Errorf("markov: count tokens: %w", err)
	}
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM chains;`).Scan(&s.Chains); err != nil {
		return Stats{}, fmt.Errorf("markov: count chains: %w", err)
	}
	return s, nil
}

// RandomContext returns a uniformly random prev_key from the chains table, used
// to seed dream-like generation from a half-remembered fragment. The boolean is
// false when no chains exist yet.
func (r *SQLRepo) RandomContext(ctx context.Context) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, fmt.Errorf("markov: nil repo")
	}
	var prevKey string
	err := r.db.QueryRowContext(ctx,
		`SELECT prev_key FROM chains ORDER BY RANDOM() LIMIT 1;`).Scan(&prevKey)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("markov: random context: %w", err)
	}
	return prevKey, true, nil
}

// CreditChains adds reward to each transition's reward_sum/reward_n, so phrasings
// from well-received posts gain sampling weight. It runs in one transaction.
func (r *SQLRepo) CreditChains(ctx context.Context, chains []ChainBump, reward float64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("markov: nil repo")
	}
	if len(chains) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("markov: begin credit tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx,
		`UPDATE chains SET reward_sum = reward_sum + ?, reward_n = reward_n + 1
		 WHERE prev_key = ? AND next_surface = ?;`)
	if err != nil {
		return fmt.Errorf("markov: prepare credit: %w", err)
	}
	defer stmt.Close()

	for _, cb := range chains {
		if _, err := stmt.ExecContext(ctx, reward, cb.PrevKey, cb.Next); err != nil {
			return fmt.Errorf("markov: credit chain %q->%q: %w", cb.PrevKey, cb.Next, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("markov: commit credit: %w", err)
	}
	committed = true
	return nil
}

// RandomFrequentToken returns a random token of the given POS whose freq is at
// least minFreq, used to seed topical posts. The boolean is false when none
// qualify.
func (r *SQLRepo) RandomFrequentToken(ctx context.Context, pos string, minFreq int) (string, bool, error) {
	if r == nil || r.db == nil {
		return "", false, fmt.Errorf("markov: nil repo")
	}
	var surface string
	err := r.db.QueryRowContext(ctx,
		`SELECT surface FROM tokens WHERE pos = ? AND freq >= ? ORDER BY RANDOM() LIMIT 1;`,
		pos, minFreq).Scan(&surface)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("markov: random frequent token: %w", err)
	}
	return surface, true, nil
}
