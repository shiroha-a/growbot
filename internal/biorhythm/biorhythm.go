// Package biorhythm models the bot's daily sleep/wake cycle and the slow
// dynamics of its energy level.
//
// A [Clock] describes when the bot is asleep in local wall-clock hours, and the
// energy helpers describe how energy recovers during sleep and drains while
// awake. All energy values are clamped to the inclusive range [0,1].
package biorhythm

import (
	"math"
	"time"
)

// Per-hour rates of energy change. Sleep recovers faster than waking drains,
// so a normal night of sleep more than offsets a day of activity.
const (
	// recoverPerHour is the energy regained per hour while asleep.
	recoverPerHour = 0.25
	// drainPerHour is the energy lost per hour while awake.
	drainPerHour = 0.05
)

// Clock describes the bot's sleep window in local wall-clock hours.
//
// SleepStartHour and SleepEndHour are hours in the range 0..23. The window is
// half-open: the bot is asleep for hours in [SleepStartHour, SleepEndHour) and
// wraps around midnight when SleepStartHour > SleepEndHour. When the two hours
// are equal the bot is never asleep.
type Clock struct {
	SleepStartHour int // local hour at which sleep begins (inclusive)
	SleepEndHour   int // local hour at which sleep ends (exclusive)
}

// Asleep reports whether the bot is asleep at time t, using t's local hour.
//
// The sleep window is half-open and may wrap around midnight. For example a
// window of 1..7 means asleep for hours 1,2,3,4,5,6; a window of 23..6 means
// asleep for hours 23,0,1,2,3,4,5. When SleepStartHour == SleepEndHour the bot
// is never asleep.
func (c Clock) Asleep(t time.Time) bool {
	// start==endは睡眠ゼロ時間とみなし、常に起床扱いにする。
	if c.SleepStartHour == c.SleepEndHour {
		return false
	}
	h := t.Hour()
	if c.SleepStartHour < c.SleepEndHour {
		// 日付をまたがない通常の窓。
		return h >= c.SleepStartHour && h < c.SleepEndHour
	}
	// 真夜中をまたぐ窓。開始時刻以降、または終了時刻より前なら睡眠中。
	return h >= c.SleepStartHour || h < c.SleepEndHour
}

// Awake reports whether the bot is awake at time t. It is the inverse of
// [Clock.Asleep].
func (c Clock) Awake(t time.Time) bool {
	return !c.Asleep(t)
}

// EnergyAfter returns the energy level after elapsing dt from the given energy,
// recovering toward 1.0 while asleep and draining toward 0.0 while awake.
//
// The change is proportional to dt.Hours() at the relevant per-hour rate. A
// non-positive dt is a no-op. The result is always clamped to [0,1].
func EnergyAfter(energy float64, dt time.Duration, asleep bool) float64 {
	if dt <= 0 {
		// 時間が経過していない、または逆行する場合は変化させない。
		return Clamp01(energy)
	}
	hours := dt.Hours()
	if asleep {
		energy += recoverPerHour * hours
	} else {
		energy -= drainPerHour * hours
	}
	return Clamp01(energy)
}

// Clamp01 clamps x to the inclusive range [0,1]. A NaN input collapses to 0 so
// the [0,1] invariant always holds for downstream callers.
func Clamp01(x float64) float64 {
	// NaN は比較が常に偽になり素通りするため先に潰す。
	if math.IsNaN(x) {
		return 0
	}
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
