package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/observability/logging"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"DEBUG", slog.LevelDebug},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := logging.ParseLevel(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLevelRejectsInvalid(t *testing.T) {
	for _, in := range []string{"", "trace", "verbose"} {
		if _, err := logging.ParseLevel(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestNewLoggerEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(slog.LevelInfo, &buf)

	logger.Info("hello", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, buf.String())
	}
	for _, key := range []string{"time", "level", "msg"} {
		if _, ok := rec[key]; !ok {
			t.Errorf("missing key %q in %v", key, rec)
		}
	}
	if rec["msg"] != "hello" || rec["k"] != "v" {
		t.Errorf("unexpected record: %v", rec)
	}
}

func TestNewLoggerHonorsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(slog.LevelWarn, &buf)

	logger.Info("skipped")

	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}
