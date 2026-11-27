package config_test

import (
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
)

func validEnv() map[string]string {
	return map[string]string{"RELAY_DATABASE_URL": "postgres://u:secret@h/db"}
}

func load(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	cfg, err := config.Load(mapLookup(env))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func TestValidateAcceptsDefaultsWithDatabaseURL(t *testing.T) {
	cfg := load(t, validEnv())
	for _, r := range []config.Role{config.RoleCombined, config.RoleAPI, config.RoleWorker} {
		if err := cfg.Validate(r); err != nil {
			t.Errorf("role %s: %v", r, err)
		}
	}
}

func TestValidateMissingDatabaseURL(t *testing.T) {
	err := load(t, nil).Validate(config.RoleAPI)
	if err == nil || !strings.Contains(err.Error(), "RELAY_DATABASE_URL is required") {
		t.Fatalf("want clear missing-URL error, got %v", err)
	}
}

func TestValidateReportsAllErrorsTogether(t *testing.T) {
	env := validEnv()
	env["RELAY_DATABASE_URL"] = ""
	env["RELAY_WORKER_CONCURRENCY"] = "0"
	env["RELAY_HEARTBEAT_INTERVAL"] = "30s"
	env["RELAY_LEASE_DURATION"] = "30s"
	env["RELAY_LOG_LEVEL"] = "loud"
	err := load(t, env).Validate(config.RoleCombined)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"RELAY_DATABASE_URL", "RELAY_WORKER_CONCURRENCY", "RELAY_HEARTBEAT_INTERVAL", "RELAY_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %s: %v", want, err)
		}
	}
}

func TestValidateRules(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		role config.Role
		want string
	}{
		{"concurrency below 1", map[string]string{"RELAY_WORKER_CONCURRENCY": "0"}, config.RoleWorker, "RELAY_WORKER_CONCURRENCY"},
		{"heartbeat equals lease", map[string]string{"RELAY_HEARTBEAT_INTERVAL": "30s", "RELAY_LEASE_DURATION": "30s"}, config.RoleWorker, "RELAY_HEARTBEAT_INTERVAL"},
		{"heartbeat above lease", map[string]string{"RELAY_HEARTBEAT_INTERVAL": "1m"}, config.RoleCombined, "RELAY_HEARTBEAT_INTERVAL"},
		{"negative heartbeat", map[string]string{"RELAY_HEARTBEAT_INTERVAL": "-1s"}, config.RoleWorker, "RELAY_HEARTBEAT_INTERVAL"},
		{"zero lease", map[string]string{"RELAY_LEASE_DURATION": "0"}, config.RoleWorker, "RELAY_LEASE_DURATION"},
		{"max attempts below 1", map[string]string{"RELAY_DEFAULT_MAX_ATTEMPTS": "0"}, config.RoleAPI, "RELAY_DEFAULT_MAX_ATTEMPTS"},
		{"zero retry base", map[string]string{"RELAY_RETRY_BASE_DELAY": "0"}, config.RoleWorker, "RELAY_RETRY_BASE_DELAY"},
		{"retry max below base", map[string]string{"RELAY_RETRY_BASE_DELAY": "1m", "RELAY_RETRY_MAX_DELAY": "1s"}, config.RoleWorker, "RELAY_RETRY_MAX_DELAY"},
		{"negative default timeout", map[string]string{"RELAY_DEFAULT_TIMEOUT": "-1s"}, config.RoleAPI, "RELAY_DEFAULT_TIMEOUT"},
		{"zero cancel grace", map[string]string{"RELAY_CANCEL_GRACE_PERIOD": "0"}, config.RoleWorker, "RELAY_CANCEL_GRACE_PERIOD"},
		{"zero shutdown timeout", map[string]string{"RELAY_SHUTDOWN_TIMEOUT": "0"}, config.RoleAPI, "RELAY_SHUTDOWN_TIMEOUT"},
		{"zero payload limit", map[string]string{"RELAY_MAX_PAYLOAD_BYTES": "0"}, config.RoleAPI, "RELAY_MAX_PAYLOAD_BYTES"},
		{"zero result limit", map[string]string{"RELAY_MAX_RESULT_BYTES": "0"}, config.RoleWorker, "RELAY_MAX_RESULT_BYTES"},
		{"bad log level", map[string]string{"RELAY_LOG_LEVEL": "loud"}, config.RoleAPI, "RELAY_LOG_LEVEL"},
		{"queues only commas", map[string]string{"RELAY_WORKER_QUEUES": " , ,"}, config.RoleWorker, "RELAY_WORKER_QUEUES"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			for k, v := range tt.env {
				env[k] = v
			}
			err := load(t, env).Validate(tt.role)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error naming %s, got %v", tt.want, err)
			}
		})
	}
}

func TestAPIRoleSkipsWorkerSettings(t *testing.T) {
	env := validEnv()
	env["RELAY_WORKER_CONCURRENCY"] = "0"
	env["RELAY_HEARTBEAT_INTERVAL"] = "1h"
	env["RELAY_WORKER_QUEUES"] = ","
	if err := load(t, env).Validate(config.RoleAPI); err != nil {
		t.Errorf("api role must ignore worker settings: %v", err)
	}
}

func TestWorkerRoleSkipsAPISettings(t *testing.T) {
	env := validEnv()
	env["RELAY_MAX_PAYLOAD_BYTES"] = "0"
	env["RELAY_HTTP_ADDR"] = ""
	if err := load(t, env).Validate(config.RoleWorker); err != nil {
		t.Errorf("worker role must ignore api settings: %v", err)
	}
}

func TestValidateRejectsUnknownRole(t *testing.T) {
	if err := load(t, validEnv()).Validate(config.Role("bogus")); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRole(t *testing.T) {
	for _, s := range []string{"combined", "api", "worker"} {
		if r, err := config.ParseRole(s); err != nil || string(r) != s {
			t.Errorf("ParseRole(%q) = %v, %v", s, r, err)
		}
	}
	if _, err := config.ParseRole("x"); err == nil {
		t.Error("expected error for unknown role")
	}
}
