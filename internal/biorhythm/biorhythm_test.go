package biorhythm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// atHour builds a time at the given local hour. The date is fixed and
// irrelevant because the sleep logic only inspects t.Hour().
func atHour(h int) time.Time {
	return time.Date(2026, time.June, 4, h, 30, 0, 0, time.Local)
}

func TestAsleepNormalWindow(t *testing.T) {
	// Window 1..7 is half-open: asleep for hours 1..6, awake otherwise.
	c := Clock{SleepStartHour: 1, SleepEndHour: 7}

	asleepHours := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true}
	for h := 0; h < 24; h++ {
		want := asleepHours[h]
		require.Equalf(t, want, c.Asleep(atHour(h)), "Asleep at hour %d", h)
		require.Equalf(t, !want, c.Awake(atHour(h)), "Awake at hour %d", h)
	}

	// Boundary checks: start is inclusive, end is exclusive.
	require.True(t, c.Asleep(atHour(1)))
	require.False(t, c.Asleep(atHour(7)))
	require.False(t, c.Asleep(atHour(0)))
}

func TestAsleepWrapAroundWindow(t *testing.T) {
	// Window 23..6 wraps midnight: asleep for hours 23,0,1,2,3,4,5.
	c := Clock{SleepStartHour: 23, SleepEndHour: 6}

	asleepHours := map[int]bool{23: true, 0: true, 1: true, 2: true, 3: true, 4: true, 5: true}
	for h := 0; h < 24; h++ {
		want := asleepHours[h]
		require.Equalf(t, want, c.Asleep(atHour(h)), "Asleep at hour %d", h)
		require.Equalf(t, !want, c.Awake(atHour(h)), "Awake at hour %d", h)
	}

	// Boundary checks across midnight.
	require.True(t, c.Asleep(atHour(23)))
	require.True(t, c.Asleep(atHour(0)))
	require.True(t, c.Asleep(atHour(5)))
	require.False(t, c.Asleep(atHour(6)))
	require.False(t, c.Asleep(atHour(22)))
}

func TestNeverAsleepWhenStartEqualsEnd(t *testing.T) {
	c := Clock{SleepStartHour: 3, SleepEndHour: 3}
	for h := 0; h < 24; h++ {
		require.Falsef(t, c.Asleep(atHour(h)), "Asleep at hour %d", h)
		require.Truef(t, c.Awake(atHour(h)), "Awake at hour %d", h)
	}
}

func TestEnergyAfterRecoversWhenAsleep(t *testing.T) {
	got := EnergyAfter(0.5, time.Hour, true)
	// Recovers at ~0.25/h, so one hour adds ~0.25.
	require.InDelta(t, 0.75, got, 1e-9)
	require.GreaterOrEqual(t, got, 0.5)
}

func TestEnergyAfterDrainsWhenAwake(t *testing.T) {
	got := EnergyAfter(0.5, time.Hour, false)
	// Drains at ~0.05/h, so one hour subtracts ~0.05.
	require.InDelta(t, 0.45, got, 1e-9)
	require.LessOrEqual(t, got, 0.5)
}

func TestEnergyAfterClampsAtOne(t *testing.T) {
	// A long sleep cannot push energy above 1.0.
	got := EnergyAfter(0.9, 10*time.Hour, true)
	require.Equal(t, 1.0, got)
}

func TestEnergyAfterClampsAtZero(t *testing.T) {
	// A long waking period cannot push energy below 0.0.
	got := EnergyAfter(0.1, 100*time.Hour, false)
	require.Equal(t, 0.0, got)
}

func TestEnergyAfterNoOpOnNonPositiveDt(t *testing.T) {
	require.Equal(t, 0.42, EnergyAfter(0.42, 0, true))
	require.Equal(t, 0.42, EnergyAfter(0.42, -time.Hour, false))

	// A no-op still clamps an out-of-range input back into [0,1].
	require.Equal(t, 1.0, EnergyAfter(1.5, 0, false))
	require.Equal(t, 0.0, EnergyAfter(-0.5, -time.Hour, true))
}

func TestEnergyAfterFractionalHours(t *testing.T) {
	// Half an hour asleep recovers half the per-hour amount.
	got := EnergyAfter(0.4, 30*time.Minute, true)
	require.InDelta(t, 0.4+0.25*0.5, got, 1e-9)
}

func TestClamp01(t *testing.T) {
	require.Equal(t, 0.0, Clamp01(-1.0))
	require.Equal(t, 0.0, Clamp01(0.0))
	require.Equal(t, 0.5, Clamp01(0.5))
	require.Equal(t, 1.0, Clamp01(1.0))
	require.Equal(t, 1.0, Clamp01(2.0))
}
