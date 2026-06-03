package safety

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGuardAllowed(t *testing.T) {
	g := NewGuard([]string{"BadWord", "  ", ""}, 0.5)
	require.NotNil(t, g)

	// Empty text is always allowed.
	require.True(t, g.Allowed(""))

	// NG word match is case-insensitive.
	require.False(t, g.Allowed("this contains a badword here"))
	require.False(t, g.Allowed("THIS HAS BADWORD"))

	// A normal sentence passes.
	require.True(t, g.Allowed("今日はいい天気ですね"))

	// A symbol-heavy string is rejected.
	require.False(t, g.Allowed("！！！？？？★★★"))

	// A short symbol under four non-space runes is not flagged.
	require.True(t, g.Allowed("！"))
}

func TestGuardNoSymbolLimit(t *testing.T) {
	// maxSymbolRatio of 0 is normalised to 1.0, disabling the symbol check.
	g := NewGuard(nil, 0)
	require.NotNil(t, g)
	require.True(t, g.Allowed("！！！？？？★★★"))
}

func TestGuardNilReceiver(t *testing.T) {
	var g *Guard
	require.True(t, g.Allowed("！！！？？？★★★"))
	require.True(t, g.Allowed("anything goes"))
}

func TestRateLimiter(t *testing.T) {
	base := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	r := NewRateLimiter(2, time.Hour)
	require.NotNil(t, r)

	// Two events within the window are allowed, the third is rejected.
	require.True(t, r.Allow(base))
	require.True(t, r.Allow(base))
	require.False(t, r.Allow(base))

	// After advancing beyond the window the next event is allowed again.
	require.True(t, r.Allow(base.Add(2*time.Hour)))
}

func TestRateLimiterDisabled(t *testing.T) {
	base := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	r := NewRateLimiter(0, time.Hour)
	require.NotNil(t, r)

	for i := 0; i < 5; i++ {
		require.True(t, r.Allow(base))
	}
}

func TestRateLimiterNilReceiver(t *testing.T) {
	var r *RateLimiter
	require.True(t, r.Allow(time.Now()))
}
