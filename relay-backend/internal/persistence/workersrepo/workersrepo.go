// Package workersrepo writes the workers table.
package workersrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// NewWorker is a workers row to insert when a worker process starts.
type NewWorker struct {
	ID          uuid.UUID
	Hostname    string
	PID         int
	Version     string
	Queues      []string
	Handlers    []string
	Concurrency int
	StartedAt   time.Time
}

// Insert adds a worker row. Its first heartbeat is StartedAt. Nil lists are
// stored as empty arrays, because the columns are NOT NULL.
func Insert(ctx context.Context, q db.Querier, w NewWorker) error {
	if w.Queues == nil {
		w.Queues = []string{}
	}
	if w.Handlers == nil {
		w.Handlers = []string{}
	}
	_, err := q.Exec(ctx, `
		INSERT INTO workers
			(id, hostname, pid, version, queues, handlers, concurrency, started_at, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		w.ID, w.Hostname, w.PID, w.Version, w.Queues, w.Handlers, w.Concurrency, w.StartedAt)
	if err != nil {
		return fmt.Errorf("insert worker %s: %w", w.ID, err)
	}
	return nil
}

// Heartbeat sets last_heartbeat_at for worker id. It returns db.ErrNotFound
// when no such worker exists.
func Heartbeat(ctx context.Context, q db.Querier, id uuid.UUID, at time.Time) error {
	tag, err := q.Exec(ctx, `UPDATE workers SET last_heartbeat_at = $2 WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("heartbeat worker %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("heartbeat worker %s: %w", id, db.ErrNotFound)
	}
	return nil
}

// MarkStopped sets stopped_at for worker id. A worker that is already stopped
// keeps its first stop time. It returns db.ErrNotFound when no such worker exists.
func MarkStopped(ctx context.Context, q db.Querier, id uuid.UUID, at time.Time) error {
	tag, err := q.Exec(ctx, `UPDATE workers SET stopped_at = COALESCE(stopped_at, $2) WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("mark worker %s stopped: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("mark worker %s stopped: %w", id, db.ErrNotFound)
	}
	return nil
}
