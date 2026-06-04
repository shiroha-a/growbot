// Package memory implements sleep-time memory consolidation for growbot.
//
// Consolidation reinforces recently-used Markov chains, decays disused ones
// following a forgetting curve, and prunes chains whose memory trace has faded
// below a threshold. It operates directly on the chains table maintained by the
// learning pipeline.
package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Params controls a consolidation pass.
type Params struct {
	Decay      float64 // strength multiplier for chains NOT used since `since`, in (0,1]
	Reinforce  float64 // strength added to chains used since `since`, >= 0
	Cap        float64 // max strength after reinforcement (e.g. 1.0)
	PruneBelow float64 // delete chains whose strength < this (e.g. 0.05)
}

// Report summarizes a consolidation pass. Reinforced and Decayed count the
// chains matched by the recency partition (last_used >= / < since), not strictly
// the chains whose strength numerically moved; Pruned counts deleted rows.
type Report struct {
	Reinforced int64
	Decayed    int64
	Pruned     int64
}

// MetaLastConsolidation is the meta key holding the timestamp of the last
// consolidation pass.
const MetaLastConsolidation = "last_consolidation"

// LastConsolidation returns the time of the previous consolidation and whether
// one has happened. A missing value yields the zero time and false.
func LastConsolidation(ctx context.Context, db *sql.DB) (time.Time, bool, error) {
	if db == nil {
		return time.Time{}, false, fmt.Errorf("memory: last consolidation: nil db")
	}
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, MetaLastConsolidation).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("memory: read last consolidation: %w", err)
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("memory: parse last consolidation %q: %w", v, err)
	}
	return t, true, nil
}

// Consolidate applies one forgetting/consolidation pass over the chains table
// and records the consolidation time, all in a single transaction.
//
// Chains whose last_used is at or after `since` are reinforced (strength toward
// Cap); chains older than `since` (or with an unparseable timestamp) decay
// (strength * Decay); then any chain whose strength fell below PruneBelow is
// deleted. Finally `now` is written to MetaLastConsolidation so the next pass's
// `since` and this pass's strength changes commit atomically. On the first pass
// (since == zero time) every chain counts as recently used, so the pass decays
// and prunes nothing and uniformly reinforces.
func Consolidate(ctx context.Context, db *sql.DB, since, now time.Time, p Params) (Report, error) {
	var report Report

	if db == nil {
		return report, fmt.Errorf("memory: consolidate: nil db")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, fmt.Errorf("memory: begin tx: %w", err)
	}
	// committed が false のままなら異常終了とみなしロールバックする。
	// コミット後の Rollback は no-op となり無害。
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// last_used は CURRENT_TIMESTAMP で書かれるが、modernc.org/sqlite ではこれが
	// RFC3339("...T...Z")形式になる(標準SQLiteのスペース区切りとは異なる)。保存形式に
	// 依存しないよう、比較は両辺を datetime() で正規化して行う。since は RFC3339 で渡す。
	sinceStr := since.UTC().Format(time.RFC3339)

	// Reinforce: since 以降に使われた鎖は強度を Cap まで引き上げる。
	res, err := tx.ExecContext(ctx,
		`UPDATE chains SET strength = min(?, strength + ?) WHERE datetime(last_used) >= datetime(?)`,
		p.Cap, p.Reinforce, sinceStr,
	)
	if err != nil {
		return report, fmt.Errorf("memory: reinforce: %w", err)
	}
	if report.Reinforced, err = res.RowsAffected(); err != nil {
		return report, fmt.Errorf("memory: reinforce rows: %w", err)
	}

	// Decay: since より前にしか使われていない鎖は忘却曲線として強度を減衰させる。
	// last_used が解釈不能(datetime() が NULL)な行も不死化させず減衰側に倒す。
	res, err = tx.ExecContext(ctx,
		`UPDATE chains SET strength = strength * ? WHERE datetime(last_used) IS NULL OR datetime(last_used) < datetime(?)`,
		p.Decay, sinceStr,
	)
	if err != nil {
		return report, fmt.Errorf("memory: decay: %w", err)
	}
	if report.Decayed, err = res.RowsAffected(); err != nil {
		return report, fmt.Errorf("memory: decay rows: %w", err)
	}

	// Prune: 減衰後の強度が閾値を下回った鎖を削除する。decay の直後に実行する
	// ことで、今回の減衰で閾値を割った鎖も同じパスで刈り取れる。
	res, err = tx.ExecContext(ctx,
		`DELETE FROM chains WHERE strength < ?`,
		p.PruneBelow,
	)
	if err != nil {
		return report, fmt.Errorf("memory: prune: %w", err)
	}
	if report.Pruned, err = res.RowsAffected(); err != nil {
		return report, fmt.Errorf("memory: prune rows: %w", err)
	}

	// 整理時刻を同一トランザクションで記録し、強度変更と原子的にコミットする。
	// (別の書き込みにすると、コミット後・記録前のクラッシュで同一区間を二重減衰しうる。)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		MetaLastConsolidation, now.UTC().Format(time.RFC3339),
	); err != nil {
		return report, fmt.Errorf("memory: record consolidation time: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("memory: commit: %w", err)
	}
	committed = true

	return report, nil
}
