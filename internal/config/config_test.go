package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// envKeys lists all environment variables read by Load. It must stay in sync
// with the Config struct so clearEnv fully isolates each test from a developer
// or CI environment that exports any of these.
var envKeys = []string{
	"MISSKEY_BASE_URL",
	"MISSKEY_TOKEN",
	"DB_PATH",
	"LOG_LEVEL",
	"LOG_FORMAT",
	"MARKOV_ORDER",
	"POST_INTERVAL",
	"MIN_SENTENCE_TOKENS",
	"MAX_SENTENCE_TOKENS",
	"LEARN_MIN_TOKENS",
	"LEARN_TIMELINE",
	"LEARN_MIN_JP_RATIO",
	"AUTONOMOUS_POST",
	"TICK_INTERVAL",
	"SLEEP_START_HOUR",
	"SLEEP_END_HOUR",
	"URGE_THRESHOLD",
	"MEMORY_DECAY",
	"MEMORY_REINFORCE",
	"MEMORY_PRUNE_BELOW",
	"CONSOLIDATE_MIN_INTERVAL",
	"DREAM_CHANCE",
	"MENTION_REPLY",
	"AFFINITY_GAIN",
	"ENGAGEMENT_POLL_INTERVAL",
	"REACTION_DELAY",
	"TOPIC_MIN_FREQ",
	"CATCHPHRASE_CHANCE",
	"ENGAGEMENT_GIVEUP",
	"NG_WORDS",
	"MAX_SYMBOL_RATIO",
	"MAX_POSTS_PER_HOUR",
	"FRIEND_AFFINITY",
}

// clearEnv unsets every known config variable and registers cleanup to
// restore the prior values after the test. With env/v11 v11.4.1, envDefault
// is applied both when a variable is unset and when it is set to an explicit
// empty string; tests fully unset the keys to make the intent explicit. The
// empty-string behavior is pinned separately by TestLoadEmptyStringUsesDefaults.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range envKeys {
		prev, had := os.LookupEnv(k)
		require.NoError(t, os.Unsetenv(k))
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prev)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}

// TestLoadDefaults verifies that envDefault values are applied when the
// variables are unset.
func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "", cfg.MisskeyBaseURL)
	require.Equal(t, "", cfg.MisskeyToken)
	require.Equal(t, "./bot.db", cfg.DBPath)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, "text", cfg.LogFormat)
}

// TestLoadFromEnv verifies that all values are read from the environment.
func TestLoadFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("MISSKEY_BASE_URL", "https://misskey.example.com")
	t.Setenv("MISSKEY_TOKEN", "secret-token")
	t.Setenv("DB_PATH", "/var/lib/growbot/bot.db")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "https://misskey.example.com", cfg.MisskeyBaseURL)
	require.Equal(t, "secret-token", cfg.MisskeyToken)
	require.Equal(t, "/var/lib/growbot/bot.db", cfg.DBPath)
	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, "json", cfg.LogFormat)
}

// TestLoadAllowsEmptyBaseURL verifies that an empty base URL is accepted for
// smoke runs.
func TestLoadAllowsEmptyBaseURL(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "", cfg.MisskeyBaseURL)
}

// TestLoadRejectsInvalidBaseURLScheme verifies that a non-HTTP scheme is
// rejected.
func TestLoadRejectsInvalidBaseURLScheme(t *testing.T) {
	clearEnv(t)
	t.Setenv("MISSKEY_BASE_URL", "ftp://misskey.example.com")

	cfg, err := Load()
	require.Error(t, err)
	require.Nil(t, cfg)
}

// TestLoadAcceptsLearnTimeline verifies a valid LEARN_TIMELINE value is parsed.
func TestLoadAcceptsLearnTimeline(t *testing.T) {
	clearEnv(t)
	t.Setenv("LEARN_TIMELINE", "global")

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "global", cfg.LearnTimeline)
}

// TestLoadDefaultsLearnTimeline verifies LEARN_TIMELINE defaults to local.
func TestLoadDefaultsLearnTimeline(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "local", cfg.LearnTimeline)
}

// TestLoadRejectsInvalidLearnTimeline verifies an unknown timeline is rejected.
func TestLoadRejectsInvalidLearnTimeline(t *testing.T) {
	clearEnv(t)
	t.Setenv("LEARN_TIMELINE", "firehose")

	cfg, err := Load()
	require.Error(t, err)
	require.Nil(t, cfg)
}

// TestLoadDefaultsLearnMinJPRatio verifies the Japanese-ratio threshold default.
func TestLoadDefaultsLearnMinJPRatio(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	require.NoError(t, err)
	require.InDelta(t, 0.3, cfg.LearnMinJPRatio, 1e-9)
}

// TestLoadRejectsOutOfRangeLearnMinJPRatio verifies the [0,1] bound is enforced.
func TestLoadRejectsOutOfRangeLearnMinJPRatio(t *testing.T) {
	clearEnv(t)
	t.Setenv("LEARN_MIN_JP_RATIO", "1.5")

	cfg, err := Load()
	require.Error(t, err)
	require.Nil(t, cfg)
}

// TestLoadAcceptsHTTPBaseURL verifies that a plain http base URL is accepted.
func TestLoadAcceptsHTTPBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("MISSKEY_BASE_URL", "http://localhost:3000")
	t.Setenv("MISSKEY_TOKEN", "tok")

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "http://localhost:3000", cfg.MisskeyBaseURL)
}

// TestLoadEmptyStringUsesDefaults pins env/v11 v11.4.1 behavior: a variable set
// to an explicit empty string still falls back to its envDefault.
func TestLoadEmptyStringUsesDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_PATH", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_FORMAT", "")

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "./bot.db", cfg.DBPath)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, "text", cfg.LogFormat)
}

// TestLoadValidation exercises each validate() branch via the environment.
func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{name: "defaults ok", env: nil, wantErr: false},
		{name: "post interval zero", env: map[string]string{"POST_INTERVAL": "0s"}, wantErr: true},
		{name: "tick interval negative", env: map[string]string{"TICK_INTERVAL": "-1s"}, wantErr: true},
		{name: "sleep start out of range", env: map[string]string{"SLEEP_START_HOUR": "24"}, wantErr: true},
		{name: "sleep end out of range", env: map[string]string{"SLEEP_END_HOUR": "-1"}, wantErr: true},
		{name: "urge too high", env: map[string]string{"URGE_THRESHOLD": "1.5"}, wantErr: true},
		{name: "urge negative", env: map[string]string{"URGE_THRESHOLD": "-0.1"}, wantErr: true},
		{name: "markov order too low", env: map[string]string{"MARKOV_ORDER": "1"}, wantErr: true},
		{name: "min sentence zero", env: map[string]string{"MIN_SENTENCE_TOKENS": "0"}, wantErr: true},
		{name: "max sentence zero", env: map[string]string{"MAX_SENTENCE_TOKENS": "0"}, wantErr: true},
		{name: "learn min zero", env: map[string]string{"LEARN_MIN_TOKENS": "0"}, wantErr: true},
		{name: "min exceeds max", env: map[string]string{"MIN_SENTENCE_TOKENS": "10", "MAX_SENTENCE_TOKENS": "3"}, wantErr: true},
		{name: "boundary hours and urge ok", env: map[string]string{"SLEEP_START_HOUR": "0", "SLEEP_END_HOUR": "23", "URGE_THRESHOLD": "1"}, wantErr: false},
		{name: "urge zero ok", env: map[string]string{"URGE_THRESHOLD": "0"}, wantErr: false},
		{name: "memory decay zero", env: map[string]string{"MEMORY_DECAY": "0"}, wantErr: true},
		{name: "memory decay above one", env: map[string]string{"MEMORY_DECAY": "1.1"}, wantErr: true},
		{name: "memory reinforce negative", env: map[string]string{"MEMORY_REINFORCE": "-0.1"}, wantErr: true},
		{name: "prune below one rejected", env: map[string]string{"MEMORY_PRUNE_BELOW": "1"}, wantErr: true},
		{name: "consolidate interval zero", env: map[string]string{"CONSOLIDATE_MIN_INTERVAL": "0s"}, wantErr: true},
		{name: "dream chance too high", env: map[string]string{"DREAM_CHANCE": "1.5"}, wantErr: true},
		{name: "memory params boundary ok", env: map[string]string{"MEMORY_DECAY": "1", "MEMORY_PRUNE_BELOW": "0", "DREAM_CHANCE": "0"}, wantErr: false},
		{name: "affinity gain too high", env: map[string]string{"AFFINITY_GAIN": "1.5"}, wantErr: true},
		{name: "affinity gain boundary ok", env: map[string]string{"AFFINITY_GAIN": "1"}, wantErr: false},
		{name: "engagement poll zero", env: map[string]string{"ENGAGEMENT_POLL_INTERVAL": "0s"}, wantErr: true},
		{name: "reaction delay negative", env: map[string]string{"REACTION_DELAY": "-1m"}, wantErr: true},
		{name: "topic min freq zero", env: map[string]string{"TOPIC_MIN_FREQ": "0"}, wantErr: true},
		{name: "catchphrase chance too high", env: map[string]string{"CATCHPHRASE_CHANCE": "2"}, wantErr: true},
		{name: "giveup not greater than reaction delay", env: map[string]string{"ENGAGEMENT_GIVEUP": "10m"}, wantErr: true},
		{name: "symbol ratio above one", env: map[string]string{"MAX_SYMBOL_RATIO": "1.5"}, wantErr: true},
		{name: "symbol ratio zero rejected", env: map[string]string{"MAX_SYMBOL_RATIO": "0"}, wantErr: true},
		{name: "symbol ratio one ok", env: map[string]string{"MAX_SYMBOL_RATIO": "1"}, wantErr: false},
		{name: "max posts negative", env: map[string]string{"MAX_POSTS_PER_HOUR": "-1"}, wantErr: true},
		{name: "max posts zero ok (disabled)", env: map[string]string{"MAX_POSTS_PER_HOUR": "0"}, wantErr: false},
		{name: "friend affinity above one", env: map[string]string{"FRIEND_AFFINITY": "1.5"}, wantErr: true},
		{name: "base url without token", env: map[string]string{"MISSKEY_BASE_URL": "https://m.example.com"}, wantErr: true},
		{name: "base url with token ok", env: map[string]string{"MISSKEY_BASE_URL": "https://m.example.com", "MISSKEY_TOKEN": "t"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, cfg)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			}
		})
	}
}
