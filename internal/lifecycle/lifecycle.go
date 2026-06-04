// Package lifecycle models the bot's developmental stages, which gate how
// complex its generated utterances are allowed to be.
//
// The bot is "born at a certain level": it starts at [StageChild], a young
// speaker rather than an infant, and advances through [StageAdolescent],
// [StageYoungAdult], and [StageMature] as it accumulates age and vocabulary.
// Each stage exposes [Params] bounding the number of tokens a generated
// utterance may contain, so older bots may speak in longer, richer sentences.
package lifecycle

import "time"

// Stage is a developmental stage of the bot. Stages are ordered from least to
// most mature, so they may be compared with the usual relational operators.
type Stage int

const (
	// StageChild is the initial stage. The bot is born here as a young
	// speaker; it never starts as an infant.
	StageChild Stage = iota
	// StageAdolescent is the second stage.
	StageAdolescent
	// StageYoungAdult is the third stage.
	StageYoungAdult
	// StageMature is the final stage.
	StageMature
)

// String returns a stable, lowercase identifier for the stage. Unknown stage
// values return "unknown".
func (s Stage) String() string {
	switch s {
	case StageChild:
		return "child"
	case StageAdolescent:
		return "adolescent"
	case StageYoungAdult:
		return "young_adult"
	case StageMature:
		return "mature"
	default:
		return "unknown"
	}
}

// Params bounds the number of tokens a generated utterance may contain at a
// given stage. Both bounds are inclusive.
type Params struct {
	MinTokens int // minimum number of tokens per utterance
	MaxTokens int // maximum number of tokens per utterance
}

// Params returns the token bounds associated with the stage. Unknown stage
// values fall back to the [StageChild] bounds, keeping output conservative.
func (s Stage) Params() Params {
	switch s {
	case StageAdolescent:
		return Params{MinTokens: 3, MaxTokens: 20}
	case StageYoungAdult:
		return Params{MinTokens: 4, MaxTokens: 30}
	case StageMature:
		return Params{MinTokens: 5, MaxTokens: 40}
	case StageChild:
		fallthrough
	default:
		return Params{MinTokens: 2, MaxTokens: 12}
	}
}

// Thresholds required to reach each stage. Both the age and vocabulary
// thresholds must be met before a stage is unlocked.
const (
	adolescentMinAge   = 3 * 24 * time.Hour
	adolescentMinVocab = 300

	youngAdultMinAge   = 14 * 24 * time.Hour
	youngAdultMinVocab = 1000

	matureMinAge   = 60 * 24 * time.Hour
	matureMinVocab = 3000
)

// Evaluate returns the highest [Stage] whose age and vocabulary thresholds are
// both satisfied. A negative age is treated as zero. When no higher stage's
// thresholds are met, [StageChild] is returned.
func Evaluate(age time.Duration, vocab int) Stage {
	if age < 0 {
		age = 0
	}
	switch {
	case age >= matureMinAge && vocab >= matureMinVocab:
		return StageMature
	case age >= youngAdultMinAge && vocab >= youngAdultMinVocab:
		return StageYoungAdult
	case age >= adolescentMinAge && vocab >= adolescentMinVocab:
		return StageAdolescent
	default:
		return StageChild
	}
}
