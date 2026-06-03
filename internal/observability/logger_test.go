package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLevelUnknownFallsBackToInfo(t *testing.T) {
	// 未知のレベル文字列はinfoにフォールバックする
	require.Equal(t, slog.LevelInfo, parseLevel("unknown"))
	require.Equal(t, slog.LevelInfo, parseLevel(""))
}

func TestParseLevelKnownValues(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":  slog.LevelDebug,
		"DEBUG":  slog.LevelDebug,
		"info":   slog.LevelInfo,
		"Info":   slog.LevelInfo,
		"warn":   slog.LevelWarn,
		"WARN":   slog.LevelWarn,
		"error":  slog.LevelError,
		"Error":  slog.LevelError,
		" warn ": slog.LevelWarn,
	}
	for in, want := range cases {
		require.Equalf(t, want, parseLevel(in), "input %q", in)
	}
}

func TestNewLoggerNotNil(t *testing.T) {
	require.NotNil(t, NewLogger("info", "text"))
	require.NotNil(t, NewLogger("info", "json"))
}

func TestNewLoggerJSONHandler(t *testing.T) {
	// jsonフォーマット指定時はJSONHandlerが使われることを確認する
	logger := NewLogger("info", "json")
	_, ok := logger.Handler().(*slog.JSONHandler)
	require.True(t, ok)
}

func TestNewLoggerTextHandler(t *testing.T) {
	// json以外のフォーマット指定時はTextHandlerが使われることを確認する
	logger := NewLogger("info", "text")
	_, ok := logger.Handler().(*slog.TextHandler)
	require.True(t, ok)

	// 空文字などjson以外のフォーマットもtextにフォールバックする
	logger = NewLogger("info", "yaml")
	_, ok = logger.Handler().(*slog.TextHandler)
	require.True(t, ok)
}

func TestNewLoggerRespectsLevel(t *testing.T) {
	// levelの設定が実際にハンドラへ反映されることを確認する
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: parseLevel("warn")}
	logger := slog.New(slog.NewTextHandler(&buf, opts))

	logger.Info("should be filtered")
	require.Empty(t, buf.String())

	logger.Warn("should appear")
	require.Contains(t, buf.String(), "should appear")

	// NewLoggerが生成するハンドラもEnabledでレベルを尊重する
	warnLogger := NewLogger("warn", "text")
	require.False(t, warnLogger.Handler().Enabled(context.Background(), slog.LevelInfo))
	require.True(t, warnLogger.Handler().Enabled(context.Background(), slog.LevelWarn))
}

func TestNewLoggerEmptyLevelDefaultsInfo(t *testing.T) {
	// 空のレベルはinfoとして扱われ、debugは無効になる
	logger := NewLogger("", "text")
	require.False(t, logger.Handler().Enabled(context.Background(), slog.LevelDebug))
	require.True(t, logger.Handler().Enabled(context.Background(), slog.LevelInfo))
}

func TestNewLoggerOutputContainsMessage(t *testing.T) {
	// 出力にメッセージが含まれることをスモークテストする(os.Stdoutではなくハンドラ生成の健全性確認)
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: parseLevel("info")}))
	logger.Info("hello", slog.String("k", "v"))
	out := buf.String()
	require.True(t, strings.Contains(out, "hello"))
	require.True(t, strings.Contains(out, "\"k\":\"v\""))
}
