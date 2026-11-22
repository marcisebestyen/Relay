// Package jobsrepo reads and writes the jobs table.
package jobsrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

const selectColumns = `id, name, handler, payload, queue, status, priority, scheduled_at,
	max_attempts, timeout_seconds, created_at, updated_at, completed_at, last_error`

// Create inserts job. Attempt counters and ownership columns keep their
// database defaults. Empty name and zero timeout are stored as NULL.
func Create(ctx context.Context, q db.Querier, job domain.Job) error {
	var name *string
	if job.Name != "" {
		name = &job.Name
	}
	var timeout *int
	if job.TimeoutSeconds != 0 {
		timeout = &job.TimeoutSeconds
	}
	_, err := q.Exec(ctx, `
		INSERT INTO jobs (id, name, handler, payload, queue, status, priority, scheduled_at,
			max_attempts, timeout_seconds, created_at, updated_at, completed_at, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		job.ID, name, job.Handler, []byte(job.Payload), job.Queue, string(job.Status), job.Priority,
		job.ScheduledAt.UTC(), job.MaxAttempts, timeout, job.CreatedAt.UTC(), job.UpdatedAt.UTC(),
		job.CompletedAt, job.LastError)
	if err != nil {
		return fmt.Errorf("insert job %s: %w", job.ID, err)
	}
	return nil
}

// GetByID returns the job or db.ErrNotFound.
func GetByID(ctx context.Context, q db.Querier, id uuid.UUID) (domain.Job, error) {
	var (
		job     domain.Job
		name    *string
		payload []byte
		status  string
		timeout *int
	)
	err := q.QueryRow(ctx, `SELECT `+selectColumns+` FROM jobs WHERE id = $1`, id).Scan(
		&job.ID, &name, &job.Handler, &payload, &job.Queue, &status, &job.Priority, &job.ScheduledAt,
		&job.MaxAttempts, &timeout, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt, &job.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, db.ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("select job %s: %w", id, err)
	}
	if name != nil {
		job.Name = *name
	}
	if timeout != nil {
		job.TimeoutSeconds = *timeout
	}
	job.Payload = json.RawMessage(payload)
	job.Status = domain.JobStatus(status)
	job.ScheduledAt = job.ScheduledAt.UTC()
	job.CreatedAt = job.CreatedAt.UTC()
	job.UpdatedAt = job.UpdatedAt.UTC()
	if job.CompletedAt != nil {
		t := job.CompletedAt.UTC()
		job.CompletedAt = &t
	}
	return job, nil
}
