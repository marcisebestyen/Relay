// Package attemptsrepo reads the job_attempts table.
package attemptsrepo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// ListByJob returns up to limit attempts of a job ordered by attempt number,
// starting after the number afterNumber. afterNumber 0 starts at the beginning.
func ListByJob(ctx context.Context, q db.Querier, jobID uuid.UUID, afterNumber, limit int) ([]domain.JobAttempt, error) {
	rows, err := q.Query(ctx, `
		SELECT id, job_id, attempt_number, worker_id, status, started_at, finished_at, error, result
		FROM job_attempts
		WHERE job_id = $1 AND attempt_number > $2
		ORDER BY attempt_number
		LIMIT $3`, jobID, afterNumber, limit)
	if err != nil {
		return nil, fmt.Errorf("list attempts of job %s: %w", jobID, err)
	}
	defer rows.Close()

	attempts := []domain.JobAttempt{}
	for rows.Next() {
		var (
			a      domain.JobAttempt
			status string
			result []byte
		)
		err := rows.Scan(&a.ID, &a.JobID, &a.AttemptNumber, &a.WorkerID, &status,
			&a.StartedAt, &a.FinishedAt, &a.Error, &result)
		if err != nil {
			return nil, fmt.Errorf("scan attempt of job %s: %w", jobID, err)
		}
		a.Status = domain.AttemptStatus(status)
		a.StartedAt = a.StartedAt.UTC()
		if a.FinishedAt != nil {
			t := a.FinishedAt.UTC()
			a.FinishedAt = &t
		}
		if result != nil {
			a.Result = json.RawMessage(result)
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list attempts of job %s: %w", jobID, err)
	}
	return attempts, nil
}
