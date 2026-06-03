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
	require.True(t, shouldReply(false, 1.0, time.Hour))
	// 睡眠中は返信しない。
	require.False(t, shouldReply(true, 1.0, time.Hour))
	// 低エネルギーでは返信しない。
	require.False(t, shouldReply(false, minEnergyToAct-0.01, time.Hour))
	// 直前に返信したばかりなら控える。
	require.False(t, shouldReply(false, 1.0, mentionMinInterval-time.Second))
}

func TestAlreadySeen(t *testing.T) {
	a := &Agent{seenMentions: make(map[string]struct{})}
	require.False(t, a.alreadySeen("n1")) // 初出
	require.True(t, a.alreadySeen("n1"))  // 2回目は重複
	require.False(t, a.alreadySeen("n2")) // 別IDは初出
	require.False(t, a.alreadySeen(""))   // 空IDは常に未処理扱い
}
