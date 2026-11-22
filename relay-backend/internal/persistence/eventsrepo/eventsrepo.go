// Package eventsrepo appends rows to the job_events table.
package eventsrepo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// TypeJobCreated is the event written when a job is first stored.
const TypeJobCreated = "job.created"

// Event is one history entry of a job. Data must stay small and never hold
// the job payload.
type Event struct {
	JobID     uuid.UUID
	AttemptID *uuid.UUID
	Type      string
	Data      json.RawMessage
}

// Append inserts e. created_at comes from the database default.
func Append(ctx context.Context, q db.Querier, e Event) error {
	var data any
	if len(e.Data) > 0 {
		data = []byte(e.Data)
	}
	_, err := q.Exec(ctx,
		`INSERT INTO job_events (job_id, attempt_id, type, data) VALUES ($1, $2, $3, $4)`,
		e.JobID, e.AttemptID, e.Type, data)
	if err != nil {
		return fmt.Errorf("append %s event for job %s: %w", e.Type, e.JobID, err)
	}
	return nil
}
