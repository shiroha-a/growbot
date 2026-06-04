package learning

import (
	"context"
	"database/sql"
	"math/rand"
	"path/filepath"
	"testing"

	"growbot/internal/store"

	"github.com/stretchr/testify/require"
)

// newTestDB opens a fresh migrated database in a temp dir.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")
	db, err := store.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestBanditSelectEmptyArms verifies that selecting with no arms errors.
func TestBanditSelectEmptyArms(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	b := NewBandit(db, rand.New(rand.NewSource(1)))

	_, err := b.Select(ctx, nil)
	require.Error(t, err)

	_, err = b.Select(ctx, []string{})
	require.Error(t, err)
}

// TestBanditNilDB verifies the nil-db guard on both methods.
func TestBanditNilDB(t *testing.T) {
	ctx := context.Background()
	b := NewBandit(nil, rand.New(rand.NewSource(1)))

	_, err := b.Select(ctx, []string{"a"})
	require.Error(t, err)

	err = b.Update(ctx, "a", 0.5)
	require.Error(t, err)
}

// TestBanditConvergence verifies that after rewarding "good" highly and "bad"
// poorly, Thompson sampling selects "good" the large majority of the time.
func TestBanditConvergence(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	b := NewBandit(db, rand.New(rand.NewSource(42)))

	for i := 0; i < 300; i++ {
		require.NoError(t, b.Update(ctx, "good", 0.8))
		require.NoError(t, b.Update(ctx, "bad", 0.1))
	}

	good := 0
	for i := 0; i < 200; i++ {
		arm, err := b.Select(ctx, []string{"good", "bad"})
		require.NoError(t, err)
		if arm == "good" {
			good++
		}
	}
	require.Greater(t, good, 150)
}

// TestBanditUpdateClampsReward verifies that out-of-range rewards are clamped
// before being folded into the posterior, keeping alpha/beta growth bounded.
func TestBanditUpdateClampsReward(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	b := NewBandit(db, rand.New(rand.NewSource(7)))

	require.NoError(t, b.Update(ctx, "arm", 5.0))
	require.NoError(t, b.Update(ctx, "arm", -5.0))

	var alpha, beta float64
	var pulls int
	err := db.QueryRowContext(ctx,
		`SELECT alpha, beta, pulls FROM bandit_arms WHERE arm = ?;`, "arm",
	).Scan(&alpha, &beta, &pulls)
	require.NoError(t, err)

	// 報酬 5.0 -> 1.0、-5.0 -> 0.0 にクランプされる。
	// 事前分布 Beta(1,1) から alpha は +1(=1+0)、beta は +1(=0+1) になる。
	require.InDelta(t, 2.0, alpha, 1e-9)
	require.InDelta(t, 2.0, beta, 1e-9)
	require.Equal(t, 2, pulls)
}

// TestBanditSelectCreatesArmRows verifies that selecting unknown arms lazily
// creates their rows with the uniform prior.
func TestBanditSelectCreatesArmRows(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	b := NewBandit(db, rand.New(rand.NewSource(3)))

	_, err := b.Select(ctx, []string{"x", "y"})
	require.NoError(t, err)

	for _, arm := range []string{"x", "y"} {
		var alpha, beta float64
		err := db.QueryRowContext(ctx,
			`SELECT alpha, beta FROM bandit_arms WHERE arm = ?;`, arm,
		).Scan(&alpha, &beta)
		require.NoError(t, err)
		require.InDelta(t, 1.0, alpha, 1e-9)
		require.InDelta(t, 1.0, beta, 1e-9)
	}
}
