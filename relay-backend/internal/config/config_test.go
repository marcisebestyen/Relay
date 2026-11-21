package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
)

func mapLookup(m map[string]string) config.LookupFunc {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := config.Load(mapLookup(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := map[string]bool{
		"http addr":     cfg.HTTP.Addr == ":8080",
		"token empty":   cfg.Auth.Token == "",
		"queues":        reflect.DeepEqual(cfg.Worker.Queues, []string{"default"}),
		"concurrency":   cfg.Worker.Concurrency == 4,
		"heartbeat":     cfg.Lease.HeartbeatInterval == 10*time.Second,
		"lease":         cfg.Lease.Duration == 30*time.Second,
		"max attempts":  cfg.Retry.DefaultMaxAttempts == 3,
		"retry base":    cfg.Retry.BaseDelay == time.Second,
		"retry max":     cfg.Retry.MaxDelay == 5*time.Minute,
		"timeout":       cfg.Timeouts.Default == 0,
		"cancel grace":  cfg.Timeouts.CancelGracePeriod == 30*time.Second,
		"shutdown":      cfg.Shutdown.Timeout == 30*time.Second,
		"payload bytes": cfg.Limits.MaxPayloadBytes == 262144,
		"log level":     cfg.Telemetry.LogLevel == "info",
		"db url empty":  cfg.Database.URL == "",
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("default wrong: %s", name)
		}
	}
}

func TestLoadAppliesOverrides(t *testing.T) {
	cfg, err := config.Load(mapLookup(map[string]string{
		"RELAY_DATABASE_URL":       "postgres://x",
		"RELAY_HTTP_ADDR":          ":9000",
		"RELAY_API_TOKEN":          "tok",
		"RELAY_WORKER_QUEUES":      "a, b ,c",
		"RELAY_WORKER_CONCURRENCY": "8",
		"RELAY_LEASE_DURATION":     "1m",
		"RELAY_MAX_PAYLOAD_BYTES":  "1024",
		"RELAY_LOG_LEVEL":          "debug",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Database.URL != "postgres://x" || cfg.HTTP.Addr != ":9000" || cfg.Auth.Token != "tok" {
		t.Errorf("string overrides not applied: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.Worker.Queues, []string{"a", "b", "c"}) {
		t.Errorf("queues = %v", cfg.Worker.Queues)
	}
	if cfg.Worker.Concurrency != 8 || cfg.Lease.Duration != time.Minute || cfg.Limits.MaxPayloadBytes != 1024 {
		t.Errorf("numeric overrides not applied: %+v", cfg)
	}
	if cfg.Telemetry.LogLevel != "debug" {
		t.Errorf("log level = %q", cfg.Telemetry.LogLevel)
	}
}

func TestLoadTreatsEmptyValueAsUnset(t *testing.T) {
	cfg, err := config.Load(mapLookup(map[string]string{"RELAY_HTTP_ADDR": ""}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("addr = %q, want default", cfg.HTTP.Addr)
	}
}

func TestLoadReportsAllParseErrors(t *testing.T) {
	_, err := config.Load(mapLookup(map[string]string{
		"RELAY_WORKER_CONCURRENCY": "many",
		"RELAY_LEASE_DURATION":     "soon",
		"RELAY_MAX_PAYLOAD_BYTES":  "big",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"RELAY_WORKER_CONCURRENCY", "RELAY_LEASE_DURATION", "RELAY_MAX_PAYLOAD_BYTES"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %s", err, want)
		}
	}
}

func TestEnvVarOverridesDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env", "RELAY_HTTP_ADDR=:1111\nRELAY_LOG_LEVEL=warn\n")
	t.Setenv("RELAY_HTTP_ADDR", ":2222")

	cfg, err := config.LoadFromEnv(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTP.Addr != ":2222" {
		t.Errorf("env should win, got %q", cfg.HTTP.Addr)
	}
	if cfg.Telemetry.LogLevel != "warn" {
		t.Errorf(".env value should apply when env unset, got %q", cfg.Telemetry.LogLevel)
	}
}

func TestLoadFromEnvIgnoresMissingDotEnv(t *testing.T) {
	cfg, err := config.LoadFromEnv(t.TempDir() + "/nope.env")
	if err != nil {
		t.Fatalf("missing .env must not fail: %v", err)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("addr = %q", cfg.HTTP.Addr)
	}
}

func TestLoadFromEnvFailsOnMalformedDotEnv(t *testing.T) {
	path := writeFile(t, t.TempDir(), ".env", "not a pair\n")
	if _, err := config.LoadFromEnv(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestExampleFileMatchesStruct(t *testing.T) {
	f, err := openFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := config.ParseDotEnv(f)
	if err != nil {
		t.Fatal(err)
	}

	vars := config.Variables()
	if len(vars) != countLeafFields(reflect.TypeOf(config.Config{})) {
		t.Errorf("%d variables but struct has %d leaf fields", len(vars), countLeafFields(reflect.TypeOf(config.Config{})))
	}
	want := map[string]string{}
	for _, v := range vars {
		want[v.Name] = v.Default
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(".env.example out of sync with config.Variables()\n got: %v\nwant: %v", got, want)
	}
}

func countLeafFields(t reflect.Type) int {
	n := 0
	for i := range t.NumField() {
		if f := t.Field(i); f.Type.Kind() == reflect.Struct {
			n += countLeafFields(f.Type)
		} else {
			n++
		}
	}
	return n
}
