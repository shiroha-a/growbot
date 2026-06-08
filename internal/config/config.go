// Package config loads runtime configuration for growbot from environment
// variables (and an optional .env file).
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config holds all runtime configuration values for the bot.
//
// Values are populated from environment variables. An optional .env file in
// the working directory is loaded best-effort before parsing.
type Config struct {
	// MisskeyBaseURL is the base URL of the Misskey instance,
	// e.g. https://misskey.example.com.
	MisskeyBaseURL string `env:"MISSKEY_BASE_URL"`
	// MisskeyToken is the bot account API token.
	MisskeyToken string `env:"MISSKEY_TOKEN"`
	// DBPath is the path to the SQLite database file.
	DBPath string `env:"DB_PATH" envDefault:"./bot.db"`
	// LogLevel is the minimum log level (debug|info|warn|error).
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	// LogFormat is the log output format (text|json).
	LogFormat string `env:"LOG_FORMAT" envDefault:"text"`

	// MarkovOrder is the n-gram order of the Markov model (validated >= 2).
	MarkovOrder int `env:"MARKOV_ORDER" envDefault:"2"`
	// PostInterval is how often the bot autonomously posts a generated note.
	PostInterval time.Duration `env:"POST_INTERVAL" envDefault:"30m"`
	// MinSentenceTokens is the minimum token length of a generated sentence.
	MinSentenceTokens int `env:"MIN_SENTENCE_TOKENS" envDefault:"3"`
	// MaxSentenceTokens caps the length of a generated sentence.
	MaxSentenceTokens int `env:"MAX_SENTENCE_TOKENS" envDefault:"40"`
	// LearnMinTokens skips learning from notes shorter than this many tokens.
	LearnMinTokens int `env:"LEARN_MIN_TOKENS" envDefault:"2"`
	// LearnTimeline selects which Misskey timeline the bot learns from:
	// local | global | hybrid | home. On a near-single-user instance the local
	// timeline is almost empty, so "global" (federated public notes) gives the
	// bot far more material to learn from.
	LearnTimeline string `env:"LEARN_TIMELINE" envDefault:"local"`
	// LearnMinJPRatio skips learning from notes whose ratio of Japanese
	// characters is below this threshold, in [0,1]. It keeps the (Japanese)
	// model clean when learning from busy, multilingual timelines such as the
	// global timeline. 0 disables the filter.
	LearnMinJPRatio float64 `env:"LEARN_MIN_JP_RATIO" envDefault:"0.3"`
	// AutonomousPost toggles self-initiated posting.
	AutonomousPost bool `env:"AUTONOMOUS_POST" envDefault:"true"`

	// TickInterval is how often the internal life loop updates drives/mood/energy.
	TickInterval time.Duration `env:"TICK_INTERVAL" envDefault:"1m"`
	// SleepStartHour is the local hour at which the bot falls asleep (0..23).
	SleepStartHour int `env:"SLEEP_START_HOUR" envDefault:"1"`
	// SleepEndHour is the local hour at which the bot wakes up (0..23).
	SleepEndHour int `env:"SLEEP_END_HOUR" envDefault:"7"`
	// UrgeThreshold is the aggregate drive pressure required to act (0..1).
	UrgeThreshold float64 `env:"URGE_THRESHOLD" envDefault:"0.55"`

	// MemoryDecay multiplies the strength of chains not heard since the last
	// consolidation (forgetting), in (0,1].
	MemoryDecay float64 `env:"MEMORY_DECAY" envDefault:"0.9"`
	// MemoryReinforce is added to the strength of recently-heard chains (>= 0).
	MemoryReinforce float64 `env:"MEMORY_REINFORCE" envDefault:"0.1"`
	// MemoryPruneBelow deletes chains whose strength falls below it, in [0,1).
	MemoryPruneBelow float64 `env:"MEMORY_PRUNE_BELOW" envDefault:"0.05"`
	// ConsolidateMinInterval is the minimum time between sleep consolidations.
	ConsolidateMinInterval time.Duration `env:"CONSOLIDATE_MIN_INTERVAL" envDefault:"12h"`
	// DreamChance is the probability of posting a dream on waking, in [0,1].
	DreamChance float64 `env:"DREAM_CHANCE" envDefault:"0.3"`

	// MentionReply toggles replying to mentions and replies.
	MentionReply bool `env:"MENTION_REPLY" envDefault:"true"`
	// MentionReplyAlways makes the bot reply to mentions regardless of its sleep
	// state or energy level. The reply cooldown, NG-word/symbol filter, and the
	// hourly post rate limit still apply, and autonomous posting is unaffected.
	MentionReplyAlways bool `env:"MENTION_REPLY_ALWAYS" envDefault:"false"`
	// AffinityGain is how much affinity rises per interaction with an actor, in
	// [0,1]. A value of 0 disables affinity growth.
	AffinityGain float64 `env:"AFFINITY_GAIN" envDefault:"0.1"`

	// EngagementPollInterval is how often the bot polls its posts for engagement.
	EngagementPollInterval time.Duration `env:"ENGAGEMENT_POLL_INTERVAL" envDefault:"5m"`
	// ReactionDelay is how long to wait after a post before scoring its engagement.
	ReactionDelay time.Duration `env:"REACTION_DELAY" envDefault:"30m"`
	// TopicMinFreq is the minimum vocabulary frequency for a token to seed a topical post.
	TopicMinFreq int `env:"TOPIC_MIN_FREQ" envDefault:"3"`
	// CatchphraseChance is the probability of appending the bot's catchphrase, [0,1].
	CatchphraseChance float64 `env:"CATCHPHRASE_CHANCE" envDefault:"0.2"`
	// EngagementGiveup is how long after a post the bot abandons scoring it, so a
	// permanently-unfetchable note cannot starve the queue. Must exceed REACTION_DELAY.
	EngagementGiveup time.Duration `env:"ENGAGEMENT_GIVEUP" envDefault:"24h"`

	// NGWords are forbidden substrings; a post or learned note containing one is
	// suppressed. Comma-separated.
	NGWords []string `env:"NG_WORDS" envSeparator:","`
	// MaxSymbolRatio suppresses text whose symbol/punctuation ratio exceeds it, in
	// (0,1]. A value of 1 effectively disables the symbol gate.
	MaxSymbolRatio float64 `env:"MAX_SYMBOL_RATIO" envDefault:"0.5"`
	// MaxPostsPerHour is the hard cap on posts per rolling hour (0 disables the cap).
	MaxPostsPerHour int `env:"MAX_POSTS_PER_HOUR" envDefault:"12"`
	// FriendAffinity is the affinity threshold above which an actor counts as a
	// friend in the growth metrics, in [0,1].
	FriendAffinity float64 `env:"FRIEND_AFFINITY" envDefault:"0.3"`
}

// Load reads configuration from the environment.
//
// It first loads an optional .env file (best-effort; no error if absent),
// then parses environment variables into a Config applying defaults, and
// finally performs light validation. The returned Config is never nil on
// success.
func Load() (*Config, error) {
	// .env はローカル開発向けの補助なので、存在しなくてもエラーにしない。
	_ = godotenv.Load()

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}

	// NG_WORDS= のように空文字1要素になるケースを正規化し、空要素を取り除く。
	cfg.NGWords = nonEmptyStrings(cfg.NGWords)

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// nonEmptyStrings returns in with blank (whitespace-only) entries removed.
func nonEmptyStrings(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// validate performs light sanity checks on the configuration.
//
// 空の MisskeyBaseURL はスモークテスト用途で許容する。値がある場合のみ
// スキームを検証する。
func (c *Config) validate() error {
	if c.MisskeyBaseURL != "" &&
		!strings.HasPrefix(c.MisskeyBaseURL, "http://") &&
		!strings.HasPrefix(c.MisskeyBaseURL, "https://") {
		return fmt.Errorf("MISSKEY_BASE_URL must start with http:// or https://, got %q", c.MisskeyBaseURL)
	}
	// base URL を設定したのに token が空なのはほぼ設定ミスなので起動時に弾く。
	if c.MisskeyBaseURL != "" && c.MisskeyToken == "" {
		return fmt.Errorf("MISSKEY_TOKEN must be set when MISSKEY_BASE_URL is configured")
	}

	// PostInterval が非正だと time.NewTicker が panic するため、ここで弾く。
	if c.PostInterval <= 0 {
		return fmt.Errorf("POST_INTERVAL must be positive, got %s", c.PostInterval)
	}

	// N階数・生成長・学習閾値は正でなければならない(下流のNewModelは黙ってクランプ
	// するが、設定ミスは起動時に明示して気付けるようにする)。
	if c.MarkovOrder < 2 {
		return fmt.Errorf("MARKOV_ORDER must be >= 2, got %d", c.MarkovOrder)
	}
	if c.MinSentenceTokens < 1 {
		return fmt.Errorf("MIN_SENTENCE_TOKENS must be >= 1, got %d", c.MinSentenceTokens)
	}
	if c.MaxSentenceTokens < 1 {
		return fmt.Errorf("MAX_SENTENCE_TOKENS must be >= 1, got %d", c.MaxSentenceTokens)
	}
	if c.LearnMinTokens < 1 {
		return fmt.Errorf("LEARN_MIN_TOKENS must be >= 1, got %d", c.LearnMinTokens)
	}
	switch c.LearnTimeline {
	case "local", "global", "hybrid", "home":
	default:
		return fmt.Errorf("LEARN_TIMELINE must be one of local|global|hybrid|home, got %q", c.LearnTimeline)
	}
	if c.LearnMinJPRatio < 0 || c.LearnMinJPRatio > 1 {
		return fmt.Errorf("LEARN_MIN_JP_RATIO must be in [0,1], got %v", c.LearnMinJPRatio)
	}
	// 最小長が最大長を超えると自然な文末(EOS)が永遠に成立しないため検証する。
	if c.MinSentenceTokens > c.MaxSentenceTokens {
		return fmt.Errorf("MIN_SENTENCE_TOKENS (%d) must not exceed MAX_SENTENCE_TOKENS (%d)",
			c.MinSentenceTokens, c.MaxSentenceTokens)
	}

	// TickInterval が非正だと life loop の ticker が panic するため弾く。
	if c.TickInterval <= 0 {
		return fmt.Errorf("TICK_INTERVAL must be positive, got %s", c.TickInterval)
	}
	if c.SleepStartHour < 0 || c.SleepStartHour > 23 ||
		c.SleepEndHour < 0 || c.SleepEndHour > 23 {
		return fmt.Errorf("SLEEP_START_HOUR/SLEEP_END_HOUR must be in 0..23, got %d/%d",
			c.SleepStartHour, c.SleepEndHour)
	}
	if c.UrgeThreshold < 0 || c.UrgeThreshold > 1 {
		return fmt.Errorf("URGE_THRESHOLD must be in [0,1], got %v", c.UrgeThreshold)
	}

	if c.MemoryDecay <= 0 || c.MemoryDecay > 1 {
		return fmt.Errorf("MEMORY_DECAY must be in (0,1], got %v", c.MemoryDecay)
	}
	if c.MemoryReinforce < 0 {
		return fmt.Errorf("MEMORY_REINFORCE must be >= 0, got %v", c.MemoryReinforce)
	}
	if c.MemoryPruneBelow < 0 || c.MemoryPruneBelow >= 1 {
		return fmt.Errorf("MEMORY_PRUNE_BELOW must be in [0,1), got %v", c.MemoryPruneBelow)
	}
	if c.ConsolidateMinInterval <= 0 {
		return fmt.Errorf("CONSOLIDATE_MIN_INTERVAL must be positive, got %s", c.ConsolidateMinInterval)
	}
	if c.DreamChance < 0 || c.DreamChance > 1 {
		return fmt.Errorf("DREAM_CHANCE must be in [0,1], got %v", c.DreamChance)
	}
	if c.AffinityGain < 0 || c.AffinityGain > 1 {
		return fmt.Errorf("AFFINITY_GAIN must be in [0,1], got %v", c.AffinityGain)
	}
	if c.EngagementPollInterval <= 0 {
		return fmt.Errorf("ENGAGEMENT_POLL_INTERVAL must be positive, got %s", c.EngagementPollInterval)
	}
	if c.ReactionDelay <= 0 {
		return fmt.Errorf("REACTION_DELAY must be positive, got %s", c.ReactionDelay)
	}
	if c.TopicMinFreq < 1 {
		return fmt.Errorf("TOPIC_MIN_FREQ must be >= 1, got %d", c.TopicMinFreq)
	}
	if c.CatchphraseChance < 0 || c.CatchphraseChance > 1 {
		return fmt.Errorf("CATCHPHRASE_CHANCE must be in [0,1], got %v", c.CatchphraseChance)
	}
	if c.EngagementGiveup <= c.ReactionDelay {
		return fmt.Errorf("ENGAGEMENT_GIVEUP (%s) must be greater than REACTION_DELAY (%s)",
			c.EngagementGiveup, c.ReactionDelay)
	}
	// 0 は「最も厳格」のつもりが NewGuard 側では「無効化」になり意味が衝突するため弾く。
	if c.MaxSymbolRatio <= 0 || c.MaxSymbolRatio > 1 {
		return fmt.Errorf("MAX_SYMBOL_RATIO must be in (0,1], got %v", c.MaxSymbolRatio)
	}
	if c.MaxPostsPerHour < 0 {
		return fmt.Errorf("MAX_POSTS_PER_HOUR must be >= 0, got %d", c.MaxPostsPerHour)
	}
	if c.FriendAffinity < 0 || c.FriendAffinity > 1 {
		return fmt.Errorf("FRIEND_AFFINITY must be in [0,1], got %v", c.FriendAffinity)
	}
	return nil
}
