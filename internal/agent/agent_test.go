package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"growbot/internal/config"
	"growbot/internal/psyche"
)

// TestTimelineChannel maps LEARN_TIMELINE values to Misskey channel names.
func TestTimelineChannel(t *testing.T) {
	cases := map[string]string{
		"local":   "localTimeline",
		"global":  "globalTimeline",
		"hybrid":  "hybridTimeline",
		"home":    "homeTimeline",
		"unknown": "localTimeline", // validated upstream; an unknown value falls back to local
	}
	for in, want := range cases {
		require.Equal(t, want, timelineChannel(in), "timelineChannel(%q)", in)
	}
}

// TestJapaneseRatio checks the Japanese-character ratio used to gate learning.
func TestJapaneseRatio(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		min, max float64
	}{
		{"pure Japanese", "今日はいい天気ですね", 0.99, 1.0},
		{"katakana", "コンニチハ", 0.99, 1.0},
		{"iteration mark and punctuation", "人々、様々。", 0.99, 1.0},
		{"English only", "hello world", 0.0, 0.0},
		{"empty", "", 0.0, 0.0},
		{"mixed mostly Japanese", "今日はgood", 0.4, 0.7},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := japaneseRatio(tt.in)
			require.GreaterOrEqual(t, r, tt.min)
			require.LessOrEqual(t, r, tt.max)
		})
	}
}

// TestSanitizeText verifies mentions, URLs and custom-emoji shortcodes are
// stripped while plain Japanese (and non-mention "@") is preserved.
func TestSanitizeText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"mention at start", "@bob hello", "hello"},
		{"mention mid sentence", "ねえ@bob@example.com元気?", "ねえ元気?"},
		{"underscore-prefixed mention (MFM notifies)", "_@bob", "_"},
		{"underscore-prefixed mention with host", "_@bob@host", "_"},
		{"url without surrounding spaces", "見てhttps://example.com/path面白い", "見て面白い"},
		{"url with spaces", "リンク https://x.com です", "リンク です"},
		{"uppercase scheme URL", "HTTPS://evil.example/x", ""},
		{"ipv6 literal URL", "今http://[::1]:8080/xだ", "今だ"},
		{"idn host: scheme stripped (no linkify)", "見てhttps://例え.jp/パス", "見て例え.jp/パス"},
		{"custom emoji", "かわいい:blobcat:ね", "かわいいね"},
		{"all markup together", "@a https://x.com :foo: 本文", "本文"},
		{"all markup leaves nothing", "@bob https://x.com :foo:", ""},
		{"email is not a mention", "a@b.com", "a@b.com"},
		{"plain Japanese unchanged", "今日はいい天気", "今日はいい天気"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, sanitizeText(tt.in))
		})
	}
}

// selfWith builds a Self whose four drives are all set to urge (so Urge() == urge)
// and with the given energy.
func selfWith(urge, energy float64) *psyche.Self {
	return &psyche.Self{
		Drives: psyche.Drives{
			Recognition: urge,
			Curiosity:   urge,
			Expression:  urge,
			Boredom:     urge,
		},
		Energy: energy,
	}
}

func actCfg() *config.Config {
	return &config.Config{
		AutonomousPost: true,
		UrgeThreshold:  0.55,
		PostInterval:   30 * time.Minute,
	}
}

func TestDecideAct(t *testing.T) {
	cfg := actCfg()

	// すべての条件を満たすときだけ発話する。
	require.True(t, decideAct(cfg, selfWith(0.8, 1.0), time.Hour))

	// 自律投稿が無効なら発話しない。
	off := actCfg()
	off.AutonomousPost = false
	require.False(t, decideAct(off, selfWith(0.8, 1.0), time.Hour))

	// エネルギーが低すぎると発話しない。
	require.False(t, decideAct(cfg, selfWith(0.8, minEnergyToAct-0.01), time.Hour))

	// 衝動が閾値未満なら発話しない。
	require.False(t, decideAct(cfg, selfWith(0.2, 1.0), time.Hour))

	// 直前の投稿から十分時間が経っていなければ発話しない。
	require.False(t, decideAct(cfg, selfWith(0.8, 1.0), time.Minute))
}

func TestShouldReply(t *testing.T) {
	// 起きていて、十分なエネルギーがあり、クールダウンを過ぎていれば返信する。
	require.True(t, shouldReply(false, 1.0, time.Hour, false))
	// 睡眠中は返信しない。
	require.False(t, shouldReply(true, 1.0, time.Hour, false))
	// 低エネルギーでは返信しない。
	require.False(t, shouldReply(false, minEnergyToAct-0.01, time.Hour, false))
	// 直前に返信したばかりなら控える。
	require.False(t, shouldReply(false, 1.0, mentionMinInterval-time.Second, false))
}

func TestShouldReplyAlways(t *testing.T) {
	// MENTION_REPLY_ALWAYS時は睡眠中かつエネルギー0でも返信する。
	require.True(t, shouldReply(true, 0.0, time.Hour, true))
	// always時でも連投クールダウンは安全弁として効く。
	require.False(t, shouldReply(false, 1.0, mentionMinInterval-time.Second, true))
}

func TestAlreadySeen(t *testing.T) {
	a := &Agent{seenMentions: make(map[string]struct{})}
	require.False(t, a.alreadySeen("n1")) // 初出
	require.True(t, a.alreadySeen("n1"))  // 2回目は重複
	require.False(t, a.alreadySeen("n2")) // 別IDは初出
	require.False(t, a.alreadySeen(""))   // 空IDは常に未処理扱い
}
