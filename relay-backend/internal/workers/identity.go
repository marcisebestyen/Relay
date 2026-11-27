// Package workers manages a worker process's row in the workers table:
// registration, heartbeats, clean stop and liveness classification.
package workers

import (
	"fmt"
	"os"

	"github.com/google/uuid"
)

// Identity says which process a worker row belongs to. The ID is new for each
// process start, so a restart never reuses an old worker row.
type Identity struct {
	ID       uuid.UUID
	Hostname string
	PID      int
	Version  string
}

// NewIdentity builds an Identity for the current process.
func NewIdentity(version string) (Identity, error) {
	host, err := os.Hostname()
	if err != nil {
		return Identity{}, fmt.Errorf("read hostname: %w", err)
	}
	return Identity{
		ID:       uuid.New(),
		Hostname: host,
		PID:      os.Getpid(),
		Version:  version,
	}, nil
}
