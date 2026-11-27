package runtime

import (
	"context"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
)

// dbLifecycle records this worker's row in the database.
type dbLifecycle struct {
	q  db.Querier
	id workers.Identity
}

// NewLifecycle returns a Lifecycle that writes worker id's row through q.
func NewLifecycle(q db.Querier, id workers.Identity) Lifecycle {
	return dbLifecycle{q: q, id: id}
}

func (l dbLifecycle) Register(ctx context.Context, reg workers.Registration) error {
	return workers.Register(ctx, l.q, l.id, reg)
}

func (l dbLifecycle) Heartbeat(ctx context.Context, at time.Time) error {
	return workers.Heartbeat(ctx, l.q, l.id.ID, at)
}

func (l dbLifecycle) MarkStopped(ctx context.Context, at time.Time) error {
	return workers.MarkStopped(ctx, l.q, l.id.ID, at)
}
