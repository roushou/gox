package logging

import (
	"io"
	"log/slog"
	"os"
)

func New(level string) *slog.Logger {
	// MCP stdio transport uses stdout for protocol frames.
	// Logs must go to stderr to avoid corrupting JSON-RPC messages.
	return NewWithWriter(level, os.Stderr)
}

func NewWithWriter(level string, writer io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}
	handler := slog.NewJSONHandler(writer, opts)
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
