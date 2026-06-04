// Package psyche models the bot's internal psychological state: a born-once
// fixed temperament, a fluctuating mood, a set of motivational drives, and an
// energy level. All values are plain floats kept within fixed ranges so the
// rest of the codebase can reason about them without depending on any LLM.
package psyche

import "time"

// Temperament is the born-once fixed character. All fields in [0,1].
type Temperament struct {
	Extraversion float64 // 外向性
	Neuroticism  float64 // 神経症傾向
	Curiosity    float64 // 好奇心
}

// Mood is the fluctuating affective state. Fields in [-1,1].
type Mood struct {
	Valence float64 // 不満(-1)〜嬉しい(+1)
	Arousal float64 // 気だるい(-1)〜元気(+1)
}

// DriveKind enumerates the bot's internal needs.
type DriveKind int

const (
	DriveRecognition DriveKind = iota // 承認欲求
	DriveCuriosity                    // 好奇心
	DriveExpression                   // 表現欲
	DriveBoredom                      // 退屈
)

// Drives holds the current deficit (unmet need) of each drive, in [0,1].
type Drives struct {
	Recognition float64
	Curiosity   float64
	Expression  float64
	Boredom     float64
}

// Self is the persisted psychological state of the bot.
type Self struct {
	BornAt      time.Time
	Temperament Temperament
	Mood        Mood
	Drives      Drives
	Energy      float64 // 0..1
}
