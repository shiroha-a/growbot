package learning

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Post is a recorded post awaiting (or having undergone) engagement observation.
type Post struct {
	// NoteID is the remote Misskey note identifier and primary key.
	NoteID string
	// Kind classifies the post (e.g. a note kind or generation category).
	Kind string
	// Arm is the bandit arm that produced the post, used to attribute reward.
	Arm string
	// Text is the rendered post body.
	Text string
	// CreatedAt is when the post was recorded.
	CreatedAt time.Time
}

// Ledger persists the posts the bot has made so their later engagement can be
// observed and turned into bandit reward.
type Ledger struct {
	db *sql.DB
}

// NewLedger returns a Ledger backed by db.
func NewLedger(db *sql.DB) *Ledger {
	return &Ledger{db: db}
}

// Record inserts a post keyed by noteID. It is a no-op when the note is already
// recorded (INSERT OR IGNORE), so re-recording is safe. An empty noteID is an
// error since it is the primary key.
func (l *Ledger) Record(ctx context.Context, noteID, kind, arm, text string) error {
	if l == nil || l.db == nil {
		return errors.New("learning: nil ledger")
	}
	if noteID == "" {
		return errors.New("learning: empty noteID")
	}

	if _, err := l.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO posts(note_id, kind, arm, text) VALUES(?, ?, ?, ?);`,
		noteID, kind, arm, text,
	); err != nil {
		return fmt.Errorf("learning: record post %q: %w", noteID, err)
	}
	return nil
}

// Unobserved returns up to limit posts not yet observed and created in the
// half-open window [after, before): old enough to have accrued engagement (before
// `before`) but newer than the give-up horizon `after`, oldest first. Posts older
// than `after` are abandoned, so a permanently-unfetchable note cannot starve the
// queue. A non-positive limit returns no rows.
//
// Time comparison applies datetime() to both sides because the driver stores
// CURRENT_TIMESTAMP as RFC3339 text; the bounds are passed in UTC RFC3339.
func (l *Ledger) Unobserved(ctx context.Context, before, after time.Time, limit int) ([]Post, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("learning: nil ledger")
	}
	if limit <= 0 {
		return nil, nil
	}

	const query = `
SELECT note_id, kind, arm, text, created_at
FROM posts
WHERE observed = 0
  AND datetime(created_at) < datetime(?)
  AND datetime(created_at) >= datetime(?)
ORDER BY created_at
LIMIT ?;`

	rows, err := l.db.QueryContext(ctx, query,
		before.UTC().Format(time.RFC3339), after.UTC().Format(time.RFC3339), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("learning: query unobserved: %w", err)
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.NoteID, &p.Kind, &p.Arm, &p.Text, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("learning: scan unobserved: %w", err)
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("learning: iterate unobserved: %w", err)
	}
	return posts, nil
}

// MarkObserved records the observed engagement and computed reward for noteID
// and flags the post as observed so it is no longer returned by Unobserved.
func (l *Ledger) MarkObserved(ctx context.Context, noteID string, reactions, replies, renotes int, reward float64) error {
	if l == nil || l.db == nil {
		return errors.New("learning: nil ledger")
	}
	if noteID == "" {
		return errors.New("learning: empty noteID")
	}

	// observed=0 を条件にすることで二重採点(エンゲージメント上書き)を防ぐ。
	if _, err := l.db.ExecContext(ctx,
		`UPDATE posts
		 SET reactions = ?, replies = ?, renotes = ?, reward = ?,
		     observed = 1, observed_at = CURRENT_TIMESTAMP
		 WHERE note_id = ? AND observed = 0;`,
		reactions, replies, renotes, reward, noteID,
	); err != nil {
		return fmt.Errorf("learning: mark observed %q: %w", noteID, err)
	}
	return nil
}

// AvgReward returns the mean reward over observed posts, or 0 when there are
// none, used as a growth metric.
func (l *Ledger) AvgReward(ctx context.Context) (float64, error) {
	if l == nil || l.db == nil {
		return 0, errors.New("learning: nil ledger")
	}
	var avg sql.NullFloat64
	if err := l.db.QueryRowContext(ctx,
		`SELECT AVG(reward) FROM posts WHERE observed = 1;`).Scan(&avg); err != nil {
		return 0, fmt.Errorf("learning: avg reward: %w", err)
	}
	if !avg.Valid {
		return 0, nil
	}
	return avg.Float64, nil
}
