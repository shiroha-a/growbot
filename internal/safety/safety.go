// Package safety provides content gating and post rate limiting for the bot.
//
// The Guard rejects text that contains configured NG words or that is
// dominated by punctuation/symbol runes. The RateLimiter enforces a sliding
// window cap on how many posts may be emitted within a given duration. Both
// types are defensive: nil receivers behave as "no restriction".
package safety

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// Guard performs content gating against a configured NG word list and an
// optional symbol-ratio ceiling.
type Guard struct {
	// ng holds the lowercased, trimmed NG words to match as substrings.
	ng []string
	// maxSymbolRatio is the maximum allowed ratio of punctuation/symbol runes
	// to non-space runes. A value of 1.0 disables the symbol-ratio check.
	maxSymbolRatio float64
}

// NewGuard builds a Guard from the given NG words and maximum symbol ratio.
//
// NG words are stored lowercased and trimmed, with empty entries dropped so
// that matching is case-insensitive. If maxSymbolRatio is <= 0 or > 1 it is
// normalised to 1.0, which disables the symbol-ratio limit.
func NewGuard(ngWords []string, maxSymbolRatio float64) *Guard {
	ng := make([]string, 0, len(ngWords))
	for _, w := range ngWords {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		ng = append(ng, w)
	}

	if maxSymbolRatio <= 0 || maxSymbolRatio > 1 {
		maxSymbolRatio = 1.0
	}

	return &Guard{ng: ng, maxSymbolRatio: maxSymbolRatio}
}

// Allowed reports whether text passes content gating.
//
// Empty text is always allowed. Text is rejected if its lowercased form
// contains any configured NG word as a substring (case-insensitive), or if its
// symbol ratio exceeds maxSymbolRatio while the text has at least four
// non-space runes. The symbol ratio is the count of unicode.IsPunct or
// unicode.IsSymbol runes divided by the count of non-space runes. A nil
// receiver imposes no restriction and returns true.
func (g *Guard) Allowed(text string) bool {
	if g == nil {
		return true
	}
	if text == "" {
		return true
	}

	lower := strings.ToLower(text)
	for _, w := range g.ng {
		if strings.Contains(lower, w) {
			return false
		}
	}

	// 記号比率の判定は短い装飾(例: "！"単体)を弾かないよう、
	// 非空白文字が4文字以上ある場合に限って行う。
	var nonSpace, symbols int
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		nonSpace++
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			symbols++
		}
	}

	if nonSpace >= 4 {
		ratio := float64(symbols) / float64(nonSpace)
		if ratio > g.maxSymbolRatio {
			return false
		}
	}

	return true
}

// RateLimiter enforces a sliding-window cap of max events per window.
type RateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	events []time.Time
}

// NewRateLimiter returns a RateLimiter allowing at most max events per window.
//
// If max <= 0 the limiter is effectively disabled and Allow always returns true
// without recording events. window must be positive; a non-positive window
// prunes every event each call and so also disables the cap.
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{max: max, window: window}
}

// Allow reports whether an event occurring at now may proceed under the limit.
//
// When the limiter is disabled (max <= 0) or the receiver is nil, Allow returns
// true without recording. Otherwise events older than now-window are pruned; if
// fewer than max events remain, now is recorded and Allow returns true,
// otherwise it returns false. Allow is safe for concurrent use.
func (r *RateLimiter) Allow(now time.Time) bool {
	if r == nil {
		return true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.max <= 0 {
		return true
	}

	cutoff := now.Add(-r.window)
	kept := r.events[:0]
	for _, t := range r.events {
		// cutoff以前(同時刻含む)のイベントはウィンドウ外として破棄する。
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	r.events = kept

	if len(r.events) < r.max {
		r.events = append(r.events, now)
		return true
	}

	return false
}
