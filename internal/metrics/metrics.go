// Package metrics records periodic growth snapshots of the bot.
//
// Each snapshot captures coarse-grained growth indicators (vocabulary size,
// Markov chain count, friend count, growth stage, and average reward) so the
// bot's development can be observed over time without inspecting the underlying
// learning tables directly.
package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Snapshot is a single point-in-time measurement of the bot's growth.
//
// RecordedAt is populated by the database on Record and read back by Latest;
// callers do not need to set it when recording.
type Snapshot struct {
	Vocab      int
	Chains     int
	Friends    int
	Stage      string
	AvgReward  float64
	RecordedAt time.Time
}

// Store persists and retrieves growth snapshots backed by the metrics table.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store backed by the given database handle.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Record inserts snap as a new metrics row.
//
// The recorded_at column is left to its CURRENT_TIMESTAMP default, so
// snap.RecordedAt is ignored on insert. It returns an error if the store or its
// database handle is nil, or if the insert fails.
func (s *Store) Record(ctx context.Context, snap Snapshot) error {
	if s == nil || s.db == nil {
		return errors.New("metrics: nil store")
	}

	const query = `INSERT INTO metrics(vocab, chains, friends, stage, avg_reward)
		VALUES(?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, query,
		snap.Vocab, snap.Chains, snap.Friends, snap.Stage, snap.AvgReward,
	); err != nil {
		return fmt.Errorf("metrics: record: %w", err)
	}
	return nil
}

// Latest returns the most recently recorded snapshot.
//
// The boolean is false (with a zero Snapshot and nil error) when no snapshots
// have been recorded yet. It returns an error if the store or its database
// handle is nil, or if the query fails for any reason other than no rows.
func (s *Store) Latest(ctx context.Context) (Snapshot, bool, error) {
	if s == nil || s.db == nil {
		return Snapshot{}, false, errors.New("metrics: nil store")
	}

	// 同一秒に複数記録されても rowid 降順で真に最新の行を選ぶ。
	const query = `SELECT recorded_at, vocab, chains, friends, stage, avg_reward
		FROM metrics
		ORDER BY recorded_at DESC, rowid DESC
		LIMIT 1`

	var snap Snapshot
	err := s.db.QueryRowContext(ctx, query).Scan(
		&snap.RecordedAt,
		&snap.Vocab,
		&snap.Chains,
		&snap.Friends,
		&snap.Stage,
		&snap.AvgReward,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("metrics: latest: %w", err)
	}
	return snap, true, nil
}
