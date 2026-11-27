// Package claim lets a worker take one eligible pending job, in one short
// transaction, with a lease and a fencing token.
package claim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/attemptsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
)

// ClaimedJob is a job this worker now owns for one attempt.
type ClaimedJob struct {
	Job domain.Job
	// AttemptID and AttemptNumber identify the job_attempts row.
	AttemptID     uuid.UUID
	AttemptNumber int
	// ClaimToken must be sent back on every owner write, so a stale worker
	// cannot overwrite a job another worker has reclaimed.
	ClaimToken     uuid.UUID
	LeaseExpiresAt time.Time
	// AttemptStartedAt is the database time the attempt started.
	AttemptStartedAt time.Time
	// Deadline is the attempt start plus the job timeout. Nil when the job has no timeout.
	Deadline *time.Time
}

// Claimer takes jobs for workers. Lease times use the database clock.
type Claimer struct {
	pool  *pgxpool.Pool
	lease time.Duration
}

// NewClaimer builds a Claimer that gives each claim a lease of lease length.
func NewClaimer(pool *pgxpool.Pool, lease time.Duration) (*Claimer, error) {
	if pool == nil {
		return nil, errors.New("claimer needs a database pool")
	}
	if lease <= 0 {
		return nil, errors.New("claimer lease must be > 0")
	}
	return &Claimer{pool: pool, lease: lease}, nil
}

// claimSQL picks the best eligible job and locks it. SKIP LOCKED lets other
// workers skip rows this transaction holds. Ownership, the lease and the
// counters are set in the same statement.
const claimSQL = `
WITH candidate AS (
	SELECT id FROM jobs
	WHERE status = 'pending'
	  AND queue = ANY($1)
	  AND handler = ANY($2)
	  AND scheduled_at <= now()
	  AND cancel_requested_at IS NULL
	ORDER BY priority DESC, created_at ASC, id ASC
	LIMIT 1
	FOR UPDATE SKIP LOCKED
)
UPDATE jobs j SET
	status = 'running',
	worker_id = $3,
	claim_token = $4,
	lease_expires_at = now() + make_interval(secs => $5),
	started_at = COALESCE(j.started_at, now()),
	attempt_count = j.attempt_count + 1,
	cycle_attempt_count = j.cycle_attempt_count + 1,
	updated_at = now()
FROM candidate c
WHERE j.id = c.id
RETURNING j.id, j.attempt_count, j.lease_expires_at, now()`

// Claim takes one pending job from queues whose handler is in handlers. It
// returns nil, nil when nothing is eligible. Handler execution and network
// calls never run inside the claim transaction.
func (c *Claimer) Claim(ctx context.Context, workerID uuid.UUID, queues, handlers []string) (*ClaimedJob, error) {
	var out *ClaimedJob
	err := db.WithTx(ctx, c.pool, func(tx pgx.Tx) error {
		claimed, err := c.claimOne(ctx, tx, workerID, queues, handlers)
		if err != nil {
			return err
		}
		out = claimed
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	return out, nil
}

func (c *Claimer) claimOne(ctx context.Context, tx pgx.Tx, workerID uuid.UUID, queues, handlers []string) (*ClaimedJob, error) {
	var (
		jobID         uuid.UUID
		attemptNumber int
		leaseExpires  time.Time
		now           time.Time
	)
	token := uuid.New()
	err := tx.QueryRow(ctx, claimSQL, queues, handlers, workerID, token, c.lease.Seconds()).
		Scan(&jobID, &attemptNumber, &leaseExpires, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select and lock job: %w", err)
	}

	job, err := jobsrepo.GetByID(ctx, tx, jobID)
	if err != nil {
		return nil, fmt.Errorf("read claimed job %s: %w", jobID, err)
	}

	attemptID := uuid.New()
	err = attemptsrepo.Insert(ctx, tx, domain.JobAttempt{
		ID:            attemptID,
		JobID:         jobID,
		AttemptNumber: attemptNumber,
		WorkerID:      workerID,
		Status:        domain.AttemptRunning,
		StartedAt:     now,
	})
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(map[string]any{"attempt_number": attemptNumber, "worker_id": workerID})
	if err != nil {
		return nil, fmt.Errorf("marshal attempt event data: %w", err)
	}
	err = eventsrepo.Append(ctx, tx, eventsrepo.Event{
		JobID:     jobID,
		AttemptID: &attemptID,
		Type:      eventsrepo.TypeAttemptStarted,
		Data:      data,
	})
	if err != nil {
		return nil, err
	}

	return &ClaimedJob{
		Job:              job,
		AttemptID:        attemptID,
		AttemptNumber:    attemptNumber,
		ClaimToken:       token,
		LeaseExpiresAt:   leaseExpires.UTC(),
		AttemptStartedAt: now.UTC(),
		Deadline:         deadlineFor(now.UTC(), job.TimeoutSeconds),
	}, nil
}

// deadlineFor returns start plus timeoutSeconds, or nil when there is no timeout (0).
func deadlineFor(start time.Time, timeoutSeconds int) *time.Time {
	if timeoutSeconds == 0 {
		return nil
	}
	d := start.Add(time.Duration(timeoutSeconds) * time.Second)
	return &d
}
