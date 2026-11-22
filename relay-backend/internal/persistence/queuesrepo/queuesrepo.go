// Package queuesrepo reads and writes the queues table.
package queuesrepo

import (
	"context"
	"fmt"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// Ensure creates the queue if it does not exist yet. It is safe to call
// concurrently for the same name.
func Ensure(ctx context.Context, q db.Querier, name string) error {
	if _, err := q.Exec(ctx, `INSERT INTO queues (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, name); err != nil {
		return fmt.Errorf("ensure queue %q: %w", name, err)
	}
	return nil
}
