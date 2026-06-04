// Package observability provides logging utilities for growbot.
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// parseLevel converts a textual log level into a slog.Level.
// Unknown values fall back to slog.LevelInfo.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewLogger creates a *slog.Logger writing to os.Stdout.
//
// The level argument is parsed case-insensitively (debug|info|warn|error);
// unknown values default to info. When format equals "json" a JSON handler
// is used, otherwise a text handler is used.
func NewLogger(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}

	var handler slog.Handler
	// formatの判定は小文字化して比較し、json以外は全てtextとして扱う
	if strings.ToLower(strings.TrimSpace(format)) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
