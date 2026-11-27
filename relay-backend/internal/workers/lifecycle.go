package workers

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/workersrepo"
)

// Registration is what a worker advertises when it starts.
type Registration struct {
	Queues      []string
	Handlers    []string
	Concurrency int
	StartedAt   time.Time
}

// Register inserts the worker row for id. Call it once, before the first heartbeat.
func Register(ctx context.Context, q db.Querier, id Identity, reg Registration) error {
	return workersrepo.Insert(ctx, q, workersrepo.NewWorker{
		ID:          id.ID,
		Hostname:    id.Hostname,
		PID:         id.PID,
		Version:     id.Version,
		Queues:      reg.Queues,
		Handlers:    reg.Handlers,
		Concurrency: reg.Concurrency,
		StartedAt:   reg.StartedAt,
	})
}

// Heartbeat records that worker id is alive at at.
func Heartbeat(ctx context.Context, q db.Querier, id uuid.UUID, at time.Time) error {
	if err := workersrepo.Heartbeat(ctx, q, id, at); err != nil {
		return fmt.Errorf("worker heartbeat: %w", err)
	}
	return nil
}

// MarkStopped records a clean shutdown of worker id at at.
func MarkStopped(ctx context.Context, q db.Querier, id uuid.UUID, at time.Time) error {
	if err := workersrepo.MarkStopped(ctx, q, id, at); err != nil {
		return fmt.Errorf("worker mark stopped: %w", err)
	}
	return nil
}
