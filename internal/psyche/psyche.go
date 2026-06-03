package psyche

import (
	"math/rand"
	"time"
)

// driveBaseRatePerHour is the baseline growth of an unmet drive per elapsed
// hour. 1時間あたり約0.1の速さで欠乏が溜まるため、約10時間放置で飽和に近づく。
const driveBaseRatePerHour = 0.1

// clamp constrains v to the inclusive range [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// NewTemperament draws a fresh, born-once temperament. Each trait is an
// independent draw from r in [0,1). When r is nil a time-seeded source is used
// so callers may omit an explicit rng (at the cost of reproducibility).
func NewTemperament(r *rand.Rand) Temperament {
	if r == nil {
		// 乱数源が無い場合は時刻シードで生成する。再現性は犠牲になるが誕生は進められる。
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return Temperament{
		Extraversion: r.Float64(),
		Neuroticism:  r.Float64(),
		Curiosity:    r.Float64(),
	}
}

// BaselineMood derives a modest resting mood from the fixed temperament.
//
// Extraverted bots rest slightly happier and more energetic, while neurotic
// bots rest slightly unhappier. The coefficients keep the baseline gentle so
// temperament biases mood without pinning it to the extremes.
func (t Temperament) BaselineMood() Mood {
	return Mood{
		Valence: clamp((t.Extraversion-t.Neuroticism)*0.3, -1, 1),
		Arousal: clamp((t.Extraversion-0.5)*0.4, -1, 1),
	}
}

// TowardBaseline moves each mood field a fraction rate toward baseline,
// modelling the inertia by which mood drifts back to its resting state over
// time. rate is clamped to [0,1]; the result is clamped to [-1,1].
//
// 慣性: 時間とともに基線へ戻る。
func (m Mood) TowardBaseline(baseline Mood, rate float64) Mood {
	rate = clamp(rate, 0, 1)
	return Mood{
		Valence: clamp(m.Valence+(baseline.Valence-m.Valence)*rate, -1, 1),
		Arousal: clamp(m.Arousal+(baseline.Arousal-m.Arousal)*rate, -1, 1),
	}
}

// Nudge adds the given deltas to mood and clamps the result to [-1,1]. It is
// used to react to events (a like, a reply, the passage of a dull hour).
func (m Mood) Nudge(dValence, dArousal float64) Mood {
	return Mood{
		Valence: clamp(m.Valence+dValence, -1, 1),
		Arousal: clamp(m.Arousal+dArousal, -1, 1),
	}
}

// Grow increases each drive deficit according to the elapsed duration dt.
//
// Curiosity grows faster for curious bots and Expression faster for extraverted
// bots, so a bot's temperament shapes which needs press on it soonest.
// Recognition and Boredom grow at the base rate. A non-positive dt is a no-op.
// Every field is clamped to [0,1].
func (d Drives) Grow(dt time.Duration, t Temperament) Drives {
	// 経過時間が無い(もしくは負)場合は何も増やさない。
	if dt <= 0 {
		return d
	}
	hours := dt.Hours()
	base := driveBaseRatePerHour * hours
	return Drives{
		Recognition: clamp(d.Recognition+base, 0, 1),
		Curiosity:   clamp(d.Curiosity+base*t.Curiosity, 0, 1),
		Expression:  clamp(d.Expression+base*t.Extraversion, 0, 1),
		Boredom:     clamp(d.Boredom+base, 0, 1),
	}
}

// Bump adds delta to the named drive and clamps the result to [0,1]. A negative
// delta satisfies the need; a positive delta intensifies it. An unknown
// DriveKind leaves the drives unchanged.
func (d Drives) Bump(k DriveKind, delta float64) Drives {
	switch k {
	case DriveRecognition:
		d.Recognition = clamp(d.Recognition+delta, 0, 1)
	case DriveCuriosity:
		d.Curiosity = clamp(d.Curiosity+delta, 0, 1)
	case DriveExpression:
		d.Expression = clamp(d.Expression+delta, 0, 1)
	case DriveBoredom:
		d.Boredom = clamp(d.Boredom+delta, 0, 1)
	}
	return d
}

// Get returns the current deficit of the named drive. An unknown DriveKind
// returns 0.
func (d Drives) Get(k DriveKind) float64 {
	switch k {
	case DriveRecognition:
		return d.Recognition
	case DriveCuriosity:
		return d.Curiosity
	case DriveExpression:
		return d.Expression
	case DriveBoredom:
		return d.Boredom
	default:
		return 0
	}
}

// Urge reports the aggregate motivational pressure as the mean of the four
// deficits, clamped to [0,1]. A higher value means the bot is more strongly
// driven to act.
func (d Drives) Urge() float64 {
	mean := (d.Recognition + d.Curiosity + d.Expression + d.Boredom) / 4
	return clamp(mean, 0, 1)
}
