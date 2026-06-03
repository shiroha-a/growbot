// Package habit models an emergent verbal tic (口癖) that the bot appends to
// its utterances occasionally as it matures.
//
// A catchphrase is selected from a pool of candidates and then appended to a
// piece of text with some probability. Both operations accept an optional
// [*math/rand.Rand]: when one is supplied the behaviour is deterministic, and
// when it is nil a time-seeded generator is used instead.
package habit

import (
	"math/rand"
	"strings"
	"time"
)

// newRand returns r when it is non-nil, otherwise a fresh time-seeded
// generator. Centralising this keeps the nil-handling behaviour identical
// across the exported helpers.
func newRand(r *rand.Rand) *rand.Rand {
	if r != nil {
		return r
	}
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// PickCatchphrase chooses one non-empty candidate at random and returns it.
//
// Empty candidates are skipped, so a candidate is only ever returned when it
// has content. When candidates is nil/empty or contains only empty strings it
// returns ("", false). The selection is deterministic for a given r; when r is
// nil a time-seeded generator is used.
func PickCatchphrase(candidates []string, r *rand.Rand) (string, bool) {
	// 空文字を除いた候補だけを抽選対象にする。
	nonEmpty := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c != "" {
			nonEmpty = append(nonEmpty, c)
		}
	}
	if len(nonEmpty) == 0 {
		return "", false
	}
	rng := newRand(r)
	return nonEmpty[rng.Intn(len(nonEmpty))], true
}

// Apply appends catchphrase to text with probability p (clamped to [0,1]) and
// returns the result.
//
// The catchphrase is not appended, and text is returned unchanged, when text
// is empty, catchphrase is empty, or text already ends with catchphrase. The
// returned string always has text as a prefix and introduces no control
// characters. The decision is deterministic for a given r; when r is nil a
// time-seeded generator is used.
func Apply(text, catchphrase string, p float64, r *rand.Rand) string {
	// 二重付与や無意味な付与を避けるためのガード。これらは確率抽選より優先する。
	if text == "" || catchphrase == "" || strings.HasSuffix(text, catchphrase) {
		return text
	}
	// pを[0,1]に収めてから抽選する。
	if p <= 0 {
		return text
	}
	if p > 1 {
		p = 1
	}
	rng := newRand(r)
	if rng.Float64() < p {
		return text + catchphrase
	}
	return text
}
