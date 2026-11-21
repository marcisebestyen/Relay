// Package logging builds the structured JSON logger used across Relay.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ParseLevel converts debug|info|warn|error (case-insensitive) to a slog level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q: want debug, info, warn or error", s)
	}
}

// NewLogger returns a JSON logger writing to w at the given level.
func NewLogger(level slog.Level, w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
