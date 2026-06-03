// Package seed provides the initial Japanese seed corpus used to give the
// bot a baseline vocabulary so it is not mute at birth.
package seed

import (
	_ "embed"
	"strings"
)

// seedJA holds the raw, newline-separated seed corpus embedded at build time.
//
//go:embed seed_ja.txt
var seedJA string

// Sentences returns the seed corpus as a slice of trimmed, non-empty
// sentences in their original file order. Blank lines are dropped.
func Sentences() []string {
	lines := strings.Split(seedJA, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		// 各行の前後の空白を除去し、空行は語彙に含めない
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
