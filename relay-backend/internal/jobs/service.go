// Package jobs holds the job use cases. It knows nothing about HTTP.
package jobs

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
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/queuesrepo"
)

var errInvalidPayload = errors.New("must be valid JSON")

// Service creates and reads jobs.
type Service struct {
	pool     *pgxpool.Pool
	defaults domain.Defaults
	newID    func() uuid.UUID
	now      func() time.Time
}

// NewService builds a Service. A nil newID or now falls back to uuid.New and
// the current UTC time; tests pass fixed ones.
func NewService(pool *pgxpool.Pool, defaults domain.Defaults, newID func() uuid.UUID, now func() time.Time) *Service {
	if newID == nil {
		newID = uuid.New
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{pool: pool, defaults: defaults, newID: newID, now: now}
}

// Create validates in, then stores the job, its queue and a job.created event
// in one transaction. Invalid input returns domain.ValidationErrors and
// touches no database.
func (s *Service) Create(ctx context.Context, in domain.NewJobInput) (domain.Job, error) {
	payloadErr := normalizePayload(&in)
	job, err := domain.NewJob(s.newID(), in, s.defaults, s.now().UTC())
	if payloadErr != nil {
		errs := domain.ValidationErrors{}
		_ = errors.As(err, &errs)
		errs["payload"] = payloadErr.Error()
		return domain.Job{}, errs
	}
	if err != nil {
		return domain.Job{}, err
	}

	data, err := json.Marshal(map[string]string{"queue": job.Queue, "handler": job.Handler})
	if err != nil {
		return domain.Job{}, fmt.Errorf("marshal event data: %w", err)
	}
	err = db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := queuesrepo.Ensure(ctx, tx, job.Queue); err != nil {
			return err
		}
		if err := jobsrepo.Create(ctx, tx, job); err != nil {
			return err
		}
		return eventsrepo.Append(ctx, tx, eventsrepo.Event{JobID: job.ID, Type: eventsrepo.TypeJobCreated, Data: data})
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

// Get returns the job or db.ErrNotFound.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Job, error) {
	return jobsrepo.GetByID(ctx, s.pool, id)
}

// normalizePayload turns an empty payload into {} (the column is NOT NULL)
// and reports a payload that is not valid JSON.
func normalizePayload(in *domain.NewJobInput) error {
	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage(`{}`)
		return nil
	}
	if !json.Valid(in.Payload) {
		return errInvalidPayload
	}
	return nil
}
