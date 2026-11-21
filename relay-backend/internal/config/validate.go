package config

import (
	"errors"
	"fmt"

	"github.com/marcisebestyen/relay/relay-backend/internal/observability/logging"
)

// Role selects which settings Validate requires.
type Role string

// Supported process roles.
const (
	RoleCombined Role = "combined"
	RoleAPI      Role = "api"
	RoleWorker   Role = "worker"
)

// ParseRole converts a string to a Role.
func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleCombined, RoleAPI, RoleWorker:
		return r, nil
	default:
		return "", fmt.Errorf("invalid role %q: want combined, api or worker", s)
	}
}

// Validate checks the settings needed by role and returns every problem at once.
func (c Config) Validate(role Role) error {
	if _, err := ParseRole(string(role)); err != nil {
		return err
	}
	var errs []error
	add := func(cond bool, format string, args ...any) {
		if !cond {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	add(c.Database.URL != "", "RELAY_DATABASE_URL is required")
	if _, err := logging.ParseLevel(c.Telemetry.LogLevel); err != nil {
		errs = append(errs, fmt.Errorf("RELAY_LOG_LEVEL: %w", err))
	}
	add(c.Shutdown.Timeout > 0, "RELAY_SHUTDOWN_TIMEOUT must be > 0")
	add(c.Retry.DefaultMaxAttempts >= 1, "RELAY_DEFAULT_MAX_ATTEMPTS must be >= 1")
	add(c.Timeouts.Default >= 0, "RELAY_DEFAULT_TIMEOUT must be >= 0")

	if role != RoleWorker {
		add(c.HTTP.Addr != "", "RELAY_HTTP_ADDR must not be empty")
		add(c.Limits.MaxPayloadBytes > 0, "RELAY_MAX_PAYLOAD_BYTES must be > 0")
	}
	if role != RoleAPI {
		errs = append(errs, c.validateWorker()...)
	}
	return errors.Join(errs...)
}

func (c Config) validateWorker() []error {
	var errs []error
	add := func(cond bool, format string, args ...any) {
		if !cond {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	add(len(c.Worker.Queues) > 0, "RELAY_WORKER_QUEUES needs at least one queue")
	add(c.Worker.Concurrency >= 1, "RELAY_WORKER_CONCURRENCY must be >= 1")
	add(c.Lease.Duration > 0, "RELAY_LEASE_DURATION must be > 0")
	add(c.Lease.HeartbeatInterval > 0, "RELAY_HEARTBEAT_INTERVAL must be > 0")
	if c.Lease.HeartbeatInterval > 0 && c.Lease.Duration > 0 {
		add(c.Lease.HeartbeatInterval < c.Lease.Duration,
			"RELAY_HEARTBEAT_INTERVAL (%s) must be < RELAY_LEASE_DURATION (%s)", c.Lease.HeartbeatInterval, c.Lease.Duration)
	}
	add(c.Retry.BaseDelay > 0, "RELAY_RETRY_BASE_DELAY must be > 0")
	add(c.Retry.MaxDelay > 0, "RELAY_RETRY_MAX_DELAY must be > 0")
	if c.Retry.BaseDelay > 0 && c.Retry.MaxDelay > 0 {
		add(c.Retry.MaxDelay >= c.Retry.BaseDelay,
			"RELAY_RETRY_MAX_DELAY (%s) must be >= RELAY_RETRY_BASE_DELAY (%s)", c.Retry.MaxDelay, c.Retry.BaseDelay)
	}
	add(c.Timeouts.CancelGracePeriod > 0, "RELAY_CANCEL_GRACE_PERIOD must be > 0")
	return errs
}
