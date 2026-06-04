// Package assoc provides keyword association helpers built on top of the
// morphological analysis output. It extracts nouns from a token stream and
// selects seed words for downstream association/generation steps without
// depending on any external service or LLM.
package assoc

import (
	"math/rand"
	"time"

	"growbot/internal/morph"
)

// nounPOS is the major part-of-speech tag for nouns in the IPA dictionary.
const nounPOS = "名詞"

// Nouns returns the surface form of every noun token in tokens, preserving the
// original order, removing duplicates (keeping the first occurrence) and
// skipping empty surfaces. It never panics on nil or empty input.
func Nouns(tokens []morph.Token) []string {
	if len(tokens) == 0 {
		return nil
	}

	// 重複排除のための既出集合。初出順を保つためスライスへ順次追加する。
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if tok.POS != nounPOS {
			continue
		}
		if tok.Surface == "" {
			continue
		}
		if _, ok := seen[tok.Surface]; ok {
			continue
		}
		seen[tok.Surface] = struct{}{}
		out = append(out, tok.Surface)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// PickSeed selects one noun surface at random from tokens using r. When r is
// nil, a time-seeded source is used instead. It returns ("", false) when tokens
// contains no usable nouns. The result is deterministic for a given r.
func PickSeed(tokens []morph.Token, r *rand.Rand) (string, bool) {
	nouns := Nouns(tokens)
	if len(nouns) == 0 {
		return "", false
	}
	if r == nil {
		// rが未指定の場合は時刻シードのローカル乱数源を用いる。
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return nouns[r.Intn(len(nouns))], true
}
