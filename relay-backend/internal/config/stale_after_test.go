package config_test

import (
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
)

func TestWorkerStaleAfterDefaultsTo30s(t *testing.T) {
	cfg, err := config.Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Worker.StaleAfter != 30*time.Second {
		t.Fatalf("StaleAfter = %s, want 30s", cfg.Worker.StaleAfter)
	}
}

func TestWorkerStaleAfterReadsOverride(t *testing.T) {
	cfg, err := config.Load(func(key string) (string, bool) {
		if key == "RELAY_WORKER_STALE_AFTER" {
			return "45s", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Worker.StaleAfter != 45*time.Second {
		t.Fatalf("StaleAfter = %s, want 45s", cfg.Worker.StaleAfter)
	}
}

func TestValidateRejectsNonPositiveWorkerStaleAfter(t *testing.T) {
	cfg, err := config.Load(func(key string) (string, bool) {
		switch key {
		case "RELAY_DATABASE_URL":
			return "postgres://x", true
		case "RELAY_WORKER_STALE_AFTER":
			return "0s", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(config.RoleWorker); err == nil {
		t.Fatal("Validate: want error for RELAY_WORKER_STALE_AFTER=0s")
	}
}
