package psyche

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
)

// Store is a SQLite-backed persistence layer for the single self row. The
// database is expected to have already had its migrations applied (e.g. via
// store.Open), including 0003_psyche.sql.
type Store struct {
	db *sql.DB
}

// NewStore wraps an open *sql.DB as a Store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// selfColumns lists the psyche columns in a fixed order shared by the read and
// write paths so SELECT/INSERT/UPDATE stay in agreement.
const selfColumns = `born_at, extraversion, neuroticism, curiosity,
	mood_valence, mood_arousal, energy,
	drive_recognition, drive_curiosity, drive_expression, drive_boredom`

// LoadOrBirth returns the persisted self, creating ("birthing") it on first use.
//
// When no self row exists yet, a fresh Self is generated: a random Temperament
// drawn from r, a Mood at that temperament's baseline, zero Drives, and full
// Energy. It is inserted as id=1 (born_at defaulting to the current time) and
// read back so the returned BornAt reflects the stored value. When a row
// already exists it is loaded verbatim, making birth idempotent.
func (s *Store) LoadOrBirth(ctx context.Context, r *rand.Rand) (*Self, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("psyche: nil store")
	}

	self, err := s.load(ctx)
	if err == nil {
		return self, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// self 行が無いので「誕生」させる。性格は乱数で一度だけ確定し、気分はその基線、
	// 欲求は空、エネルギーは満タンで開始する。
	t := NewTemperament(r)
	newborn := &Self{
		Temperament: t,
		Mood:        t.BaselineMood(),
		Energy:      1.0,
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO self (
			id, extraversion, neuroticism, curiosity,
			mood_valence, mood_arousal, energy,
			drive_recognition, drive_curiosity, drive_expression, drive_boredom
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newborn.Temperament.Extraversion,
		newborn.Temperament.Neuroticism,
		newborn.Temperament.Curiosity,
		newborn.Mood.Valence,
		newborn.Mood.Arousal,
		newborn.Energy,
		newborn.Drives.Recognition,
		newborn.Drives.Curiosity,
		newborn.Drives.Expression,
		newborn.Drives.Boredom,
	); err != nil {
		return nil, fmt.Errorf("psyche: birth insert: %w", err)
	}

	// born_at はDB側のDEFAULT(CURRENT_TIMESTAMP)で確定するため読み戻す。
	born, err := s.load(ctx)
	if err != nil {
		return nil, fmt.Errorf("psyche: birth read back: %w", err)
	}
	return born, nil
}

// load reads the single self row. It returns sql.ErrNoRows when the row does
// not exist so callers can distinguish "not yet born" from real errors.
func (s *Store) load(ctx context.Context) (*Self, error) {
	var self Self
	err := s.db.QueryRowContext(ctx,
		`SELECT `+selfColumns+` FROM self WHERE id = 1`).Scan(
		&self.BornAt,
		&self.Temperament.Extraversion,
		&self.Temperament.Neuroticism,
		&self.Temperament.Curiosity,
		&self.Mood.Valence,
		&self.Mood.Arousal,
		&self.Energy,
		&self.Drives.Recognition,
		&self.Drives.Curiosity,
		&self.Drives.Expression,
		&self.Drives.Boredom,
	)
	if err != nil {
		// ErrNoRows はそのまま返し、誕生処理側で判定させる。
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("psyche: load self: %w", err)
	}
	return &self, nil
}

// Save persists the mutable psychological state back to the self row (id=1),
// touching updated_at. Temperament is also written even though it is fixed at
// birth, keeping the row a complete snapshot. The supplied context governs the
// statement.
func (s *Store) Save(ctx context.Context, self *Self) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("psyche: nil store")
	}
	if self == nil {
		return fmt.Errorf("psyche: nil self")
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE self SET
			extraversion = ?, neuroticism = ?, curiosity = ?,
			mood_valence = ?, mood_arousal = ?, energy = ?,
			drive_recognition = ?, drive_curiosity = ?,
			drive_expression = ?, drive_boredom = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1`,
		self.Temperament.Extraversion,
		self.Temperament.Neuroticism,
		self.Temperament.Curiosity,
		self.Mood.Valence,
		self.Mood.Arousal,
		self.Energy,
		self.Drives.Recognition,
		self.Drives.Curiosity,
		self.Drives.Expression,
		self.Drives.Boredom,
	)
	if err != nil {
		return fmt.Errorf("psyche: save self: %w", err)
	}

	// 行が存在しない(まだ誕生していない)状態でのSaveは呼び出し側の誤りなので明示する。
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("psyche: save rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("psyche: save self: no self row (not yet born)")
	}
	return nil
}
