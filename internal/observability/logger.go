package observability

import (
	"log/slog"
	"os"
	"strings"
)

func NewLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:       lvl,
		ReplaceAttr: redactSecrets,
	}

	fi, _ := os.Stdout.Stat()
	isTTY := fi != nil && fi.Mode()&os.ModeCharDevice != 0
	useJSON := strings.ToLower(format) == "json" || !isTTY

	if useJSON {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func redactSecrets(_ []string, a slog.Attr) slog.Attr {
	switch strings.ToLower(a.Key) {
	case "token", "authorization", "cookie", "auth":
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}
