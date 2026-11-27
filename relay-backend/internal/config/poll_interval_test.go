package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
)

func TestPollIntervalDefaultsToOneSecond(t *testing.T) {
	cfg, err := config.Load(mapLookup(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Worker.PollInterval != time.Second {
		t.Errorf("poll interval = %s, want 1s", cfg.Worker.PollInterval)
	}
}

func TestPollIntervalOverride(t *testing.T) {
	cfg, err := config.Load(mapLookup(map[string]string{"RELAY_WORKER_POLL_INTERVAL": "250ms"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Worker.PollInterval != 250*time.Millisecond {
		t.Errorf("poll interval = %s, want 250ms", cfg.Worker.PollInterval)
	}
}

func TestValidateRejectsNonPositivePollInterval(t *testing.T) {
	cfg, err := config.Load(mapLookup(map[string]string{
		"RELAY_DATABASE_URL":         "postgres://x",
		"RELAY_WORKER_POLL_INTERVAL": "0s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = cfg.Validate(config.RoleWorker)
	if err == nil || !strings.Contains(err.Error(), "RELAY_WORKER_POLL_INTERVAL must be > 0") {
		t.Fatalf("want poll interval error, got %v", err)
	}
}
