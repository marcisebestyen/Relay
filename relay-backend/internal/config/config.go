// Package config loads and validates Relay configuration from RELAY_ env vars.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full Relay configuration, grouped by area.
type Config struct {
	Database  Database
	HTTP      HTTP
	Auth      Auth
	Worker    Worker
	Lease     Lease
	Retry     Retry
	Timeouts  Timeouts
	Shutdown  Shutdown
	Limits    Limits
	Telemetry Telemetry
}

// Database holds the Postgres connection settings.
type Database struct {
	URL string
}

// HTTP holds the API listen settings.
type HTTP struct {
	Addr string
}

// Auth holds API authentication settings. An empty Token disables auth.
type Auth struct {
	Token string
}

// Worker holds worker pool settings.
type Worker struct {
	Queues      []string
	Concurrency int
}

// Lease holds job lease and heartbeat durations.
type Lease struct {
	HeartbeatInterval time.Duration
	Duration          time.Duration
}

// Retry holds retry defaults.
type Retry struct {
	DefaultMaxAttempts int
	BaseDelay          time.Duration
	MaxDelay           time.Duration
}

// Timeouts holds execution timeout defaults. A zero Default means no timeout.
type Timeouts struct {
	Default           time.Duration
	CancelGracePeriod time.Duration
}

// Shutdown holds graceful shutdown settings.
type Shutdown struct {
	Timeout time.Duration
}

// Limits holds request and payload limits.
type Limits struct {
	MaxPayloadBytes int64
}

// Telemetry holds logging settings. Exporters are added by later tickets.
type Telemetry struct {
	LogLevel string
}

// LookupFunc reads one variable, like os.LookupEnv.
type LookupFunc func(key string) (string, bool)

// Variable describes one supported env var and its default.
type Variable struct {
	Name    string
	Default string
}

type spec struct {
	Variable
	set func(c *Config, raw string) error
}

var specs = []spec{
	text("RELAY_DATABASE_URL", "", func(c *Config) *string { return &c.Database.URL }),
	text("RELAY_HTTP_ADDR", ":8080", func(c *Config) *string { return &c.HTTP.Addr }),
	text("RELAY_API_TOKEN", "", func(c *Config) *string { return &c.Auth.Token }),
	list("RELAY_WORKER_QUEUES", "default", func(c *Config) *[]string { return &c.Worker.Queues }),
	number("RELAY_WORKER_CONCURRENCY", "4", func(c *Config) *int { return &c.Worker.Concurrency }),
	duration("RELAY_HEARTBEAT_INTERVAL", "10s", func(c *Config) *time.Duration { return &c.Lease.HeartbeatInterval }),
	duration("RELAY_LEASE_DURATION", "30s", func(c *Config) *time.Duration { return &c.Lease.Duration }),
	number("RELAY_DEFAULT_MAX_ATTEMPTS", "3", func(c *Config) *int { return &c.Retry.DefaultMaxAttempts }),
	duration("RELAY_RETRY_BASE_DELAY", "1s", func(c *Config) *time.Duration { return &c.Retry.BaseDelay }),
	duration("RELAY_RETRY_MAX_DELAY", "5m", func(c *Config) *time.Duration { return &c.Retry.MaxDelay }),
	duration("RELAY_DEFAULT_TIMEOUT", "0", func(c *Config) *time.Duration { return &c.Timeouts.Default }),
	duration("RELAY_CANCEL_GRACE_PERIOD", "30s", func(c *Config) *time.Duration { return &c.Timeouts.CancelGracePeriod }),
	duration("RELAY_SHUTDOWN_TIMEOUT", "30s", func(c *Config) *time.Duration { return &c.Shutdown.Timeout }),
	number("RELAY_MAX_PAYLOAD_BYTES", "262144", func(c *Config) *int64 { return &c.Limits.MaxPayloadBytes }),
	text("RELAY_LOG_LEVEL", "info", func(c *Config) *string { return &c.Telemetry.LogLevel }),
}

// Variables lists every supported env var with its default.
func Variables() []Variable {
	out := make([]Variable, len(specs))
	for i, s := range specs {
		out[i] = s.Variable
	}
	return out
}

// Load builds a Config from lookup, applying defaults. An empty value counts as
// unset. All parse errors are returned together. Call Validate afterwards.
func Load(lookup LookupFunc) (Config, error) {
	var cfg Config
	var errs []error
	for _, s := range specs {
		raw, ok := lookup(s.Name)
		if !ok || raw == "" {
			raw = s.Default
		}
		if err := s.set(&cfg, raw); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name, err))
		}
	}
	return cfg, errors.Join(errs...)
}

// LoadFromEnv is Load over the process env, backed by the .env file at
// dotEnvPath when it exists. Real env vars always win over the file.
func LoadFromEnv(dotEnvPath string) (Config, error) {
	file := map[string]string{}
	f, err := os.Open(dotEnvPath) //nolint:gosec // path is chosen by the caller
	switch {
	case err == nil:
		defer f.Close()
		if file, err = ParseDotEnv(f); err != nil {
			return Config{}, fmt.Errorf("read %s: %w", dotEnvPath, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return Config{}, fmt.Errorf("open %s: %w", dotEnvPath, err)
	}
	return Load(func(key string) (string, bool) {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			return v, true
		}
		v, ok := file[key]
		return v, ok
	})
}

func text(name, def string, field func(*Config) *string) spec {
	return spec{Variable{name, def}, func(c *Config, raw string) error {
		*field(c) = raw
		return nil
	}}
}

func number[T int | int64](name, def string, field func(*Config) *T) spec {
	return spec{Variable{name, def}, func(c *Config, raw string) error {
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid integer %q", raw)
		}
		*field(c) = T(n)
		return nil
	}}
}

func duration(name, def string, field func(*Config) *time.Duration) spec {
	return spec{Variable{name, def}, func(c *Config, raw string) error {
		d, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("invalid duration %q (want e.g. 30s, 5m)", raw)
		}
		*field(c) = d
		return nil
	}}
}

func list(name, def string, field func(*Config) *[]string) spec {
	return spec{Variable{name, def}, func(c *Config, raw string) error {
		var items []string
		for _, part := range strings.Split(raw, ",") {
			if p := strings.TrimSpace(part); p != "" {
				items = append(items, p)
			}
		}
		*field(c) = items
		return nil
	}}
}
