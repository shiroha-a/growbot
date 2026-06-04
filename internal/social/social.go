// Package social implements the relationships layer for growbot.
//
// It records, per actor (a remote Misskey user), how often the bot has
// interacted with them and an affinity score in the range [0,1] that grows as
// the relationship deepens. The bot uses these relationships to bias whom it
// engages with, mimicking how a living creature forms closer bonds with the
// peers it meets most.
package social

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Relationship is the persisted social state for a single actor.
type Relationship struct {
	// ActorID is the stable remote identifier for the actor.
	ActorID string
	// Username is the most recently observed non-empty display handle.
	Username string
	// Affinity is the closeness score in [0,1]; it only ever grows here.
	Affinity float64
	// Interactions counts mentions/replies received from the actor.
	Interactions int
	// ReactionsReceived counts reactions the actor gave to the bot.
	ReactionsReceived int
	// FirstSeen is when the actor was first observed.
	FirstSeen time.Time
	// LastSeen is when the actor was most recently observed.
	LastSeen time.Time
}

// Kind classifies the social event being observed.
type Kind int

const (
	// KindInteraction is a mention or reply from the actor toward the bot.
	KindInteraction Kind = iota
	// KindReaction is the actor reacting to one of the bot's posts.
	KindReaction
)

// Store provides persistence for social relationships.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store backed by db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// clampUnit constrains v to the closed interval [0,1].
func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Observe records a social event from actorID and returns the updated
// relationship.
//
// The first time an actor is seen the row is created with the relevant counter
// set to 1 and affinity set to clamp(affinityGain,0,1). On subsequent events
// the relevant counter is incremented by one, affinity grows by affinityGain
// (capped at 1.0), and the stored username is replaced only when a non-empty
// username is supplied. last_seen is always advanced; first_seen is preserved.
//
// kind selects which counter is incremented: KindInteraction bumps
// interactions, KindReaction bumps reactions_received. It returns an error when
// the store/db is nil or actorID is empty.
func (s *Store) Observe(ctx context.Context, actorID, username string, kind Kind, affinityGain float64) (*Relationship, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("social: nil store")
	}
	if actorID == "" {
		return nil, errors.New("social: empty actorID")
	}

	gain := clampUnit(affinityGain)

	// 初回 INSERT 時に立てるカウンタを kind に応じて選び、もう一方は 0 とする。
	var insertInteractions, insertReactions int
	switch kind {
	case KindReaction:
		insertReactions = 1
	default:
		insertInteractions = 1
	}

	// ON CONFLICT 側ではどちらのカウンタを +1 するかを kind で切り替える。
	// SQL を分岐させず、対象外のカウンタは現状値を据え置く形にして単純化する。
	bumpInteractions := 0
	bumpReactions := 0
	if kind == KindReaction {
		bumpReactions = 1
	} else {
		bumpInteractions = 1
	}

	const upsert = `
INSERT INTO relationships (
  actor_id, username, affinity, interactions, reactions_received, first_seen, last_seen
) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(actor_id) DO UPDATE SET
  interactions       = relationships.interactions + ?,
  reactions_received = relationships.reactions_received + ?,
  affinity           = max(0.0, min(1.0, relationships.affinity + ?)),
  username           = CASE WHEN excluded.username <> '' THEN excluded.username ELSE relationships.username END,
  last_seen          = CURRENT_TIMESTAMP;`

	if _, err := s.db.ExecContext(ctx, upsert,
		actorID, username, gain, insertInteractions, insertReactions,
		bumpInteractions, bumpReactions, gain,
	); err != nil {
		return nil, fmt.Errorf("social: observe %q: %w", actorID, err)
	}

	rel, ok, err := s.Get(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !ok {
		// 直前に upsert した行が読めないのは整合性異常。
		return nil, fmt.Errorf("social: observe %q: row missing after upsert", actorID)
	}
	return rel, nil
}

// Get returns the relationship for actorID. The boolean is false (with a nil
// relationship and nil error) when no such actor exists.
func (s *Store) Get(ctx context.Context, actorID string) (*Relationship, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, errors.New("social: nil store")
	}
	if actorID == "" {
		return nil, false, errors.New("social: empty actorID")
	}

	const query = `
SELECT actor_id, username, affinity, interactions, reactions_received, first_seen, last_seen
FROM relationships
WHERE actor_id = ?;`

	var rel Relationship
	err := s.db.QueryRowContext(ctx, query, actorID).Scan(
		&rel.ActorID,
		&rel.Username,
		&rel.Affinity,
		&rel.Interactions,
		&rel.ReactionsReceived,
		&rel.FirstSeen,
		&rel.LastSeen,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("social: get %q: %w", actorID, err)
	}
	return &rel, true, nil
}

// Affinity returns the affinity for actorID, or 0 when the actor is unknown.
func (s *Store) Affinity(ctx context.Context, actorID string) (float64, error) {
	rel, ok, err := s.Get(ctx, actorID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return rel.Affinity, nil
}

// Count returns the number of relationships whose affinity is at least
// minAffinity, used to gauge how many actors the bot feels close to.
func (s *Store) Count(ctx context.Context, minAffinity float64) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("social: nil store")
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM relationships WHERE affinity >= ?;`, minAffinity).Scan(&n); err != nil {
		return 0, fmt.Errorf("social: count: %w", err)
	}
	return n, nil
}
