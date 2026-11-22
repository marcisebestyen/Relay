// Package eventsrepo appends rows to the job_events table.
package eventsrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// Event types. The API spec lists the same values; clients must tolerate
// types added later.
const (
	TypeJobCreated           = "job.created"
	TypeAttemptStarted       = "attempt.started"
	TypeAttemptCompleted     = "attempt.completed"
	TypeAttemptFailed        = "attempt.failed"
	TypeAttemptCancelled     = "attempt.cancelled"
	TypeAttemptTimedOut      = "attempt.timed_out"
	TypeAttemptLost          = "attempt.lost"
	TypeRetryScheduled       = "retry.scheduled"
	TypeManualRetryRequested = "manual_retry.requested"
	TypeCancelRequested      = "cancel.requested"
	TypeJobStatusChanged     = "job.status_changed"
)

// KnownTypes lists every event type defined so far.
var KnownTypes = []string{
	TypeJobCreated, TypeAttemptStarted, TypeAttemptCompleted, TypeAttemptFailed, TypeAttemptCancelled,
	TypeAttemptTimedOut, TypeAttemptLost, TypeRetryScheduled, TypeManualRetryRequested,
	TypeCancelRequested, TypeJobStatusChanged,
}

// Event is one history entry of a job. Data must stay small and never hold
// the job payload. ID and CreatedAt are set when an event is read.
type Event struct {
	ID        int64
	CreatedAt time.Time
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

// ListByJob returns up to limit events of a job in (created_at, id) order,
// starting after the event afterID. afterID 0 starts at the beginning. An
// afterID that does not belong to the job yields no events.
func ListByJob(ctx context.Context, q db.Querier, jobID uuid.UUID, afterID int64, limit int) ([]Event, error) {
	rows, err := q.Query(ctx, `
		SELECT id, job_id, attempt_id, type, data, created_at
		FROM job_events
		WHERE job_id = $1
		  AND ($2::bigint = 0 OR (created_at, id) > (SELECT created_at, id FROM job_events WHERE id = $2 AND job_id = $1))
		ORDER BY created_at, id
		LIMIT $3`, jobID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list events of job %s: %w", jobID, err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var (
			e    Event
			data []byte
		)
		if err := rows.Scan(&e.ID, &e.JobID, &e.AttemptID, &e.Type, &data, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event of job %s: %w", jobID, err)
		}
		e.CreatedAt = e.CreatedAt.UTC()
		if data != nil {
			e.Data = json.RawMessage(data)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events of job %s: %w", jobID, err)
	}
	return events, nil
}
