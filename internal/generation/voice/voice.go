// Package voice decorates a generated sentence so that the bot's surface text
// reflects its current mood. Decoration is intentionally light: it only ever
// appends a short suffix and never mutates or corrupts the original text.
package voice

import (
	"math/rand"
	"time"

	"growbot/internal/psyche"
)

// 気分ごとの装飾候補。投稿文面の演出としてのみ用いるため、日本語の感嘆符や
// 単一の絵文字を文字列データとして許可している(コード/コメント中の絵文字とは別)。
var (
	// cheerfulMarkers are appended when the mood is clearly positive.
	cheerfulMarkers = []string{"！", "〜", "♪", "😊"}
	// excitedMarkers lean toward exclamation for high-arousal positive moods.
	excitedMarkers = []string{"！", "！！", "✨"}
	// downbeatMarkers are appended when the mood is clearly negative.
	downbeatMarkers = []string{"…"}
)

// Decorate optionally appends a short, mood-dependent suffix to text.
//
// The result always has text as a prefix; the function never inserts control
// characters and never alters the original content. If text is empty it is
// returned unchanged. If r is nil a time-seeded source is used. Given the same
// non-nil r and inputs, the output is deterministic.
func Decorate(text string, m psyche.Mood, r *rand.Rand) string {
	if text == "" {
		return text
	}
	if r == nil {
		// 乱数源が無い場合のみ時刻シードでフォールバックする
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	switch {
	case m.Valence > 0.3:
		// 嬉しい気分。高覚醒なら感嘆寄り、それ以外は陽気なマーカーを選ぶ。
		if m.Arousal > 0.3 {
			return maybeAppend(text, excitedMarkers, r)
		}
		return maybeAppend(text, cheerfulMarkers, r)
	case m.Valence < -0.3:
		// 不満な気分。控えめなマーカーを付けるか、何も付けずに素っ気なくする。
		return maybeAppend(text, downbeatMarkers, r)
	default:
		// 中立。高覚醒のときだけ稀に感嘆を付け、基本はそのまま返す。
		if m.Arousal > 0.3 {
			return maybeAppend(text, excitedMarkers, r)
		}
		return text
	}
}

// maybeAppend appends one element of markers to text with roughly even odds,
// otherwise returns text unchanged. It is deterministic for a given r and never
// appends when markers is empty.
func maybeAppend(text string, markers []string, r *rand.Rand) string {
	if len(markers) == 0 {
		return text
	}
	// 半分の確率で無装飾のまま返し、常に装飾されないようにする
	if r.Intn(2) == 0 {
		return text
	}
	return text + markers[r.Intn(len(markers))]
}
