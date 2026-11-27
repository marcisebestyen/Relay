// Package finalize closes a claimed attempt and its job. Every write is
// fenced by the claim token, so a stale worker changes nothing.
package finalize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/attemptsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
)

// ErrorKind says why an attempt failed. It is stored in job_attempts.error_kind.
type ErrorKind string

// Error kinds. Later tickets add timeout, cancelled and lost.
const (
	KindHandlerError   ErrorKind = "handler_error"
	KindNonRetryable   ErrorKind = "non_retryable"
	KindPanic          ErrorKind = "panic"
	KindMissingHandler ErrorKind = "missing_handler"
)

// MaxErrorBytes caps stored error text (last_error and attempt error).
const MaxErrorBytes = 4096

// ErrOwnershipLost means the claim token is no longer the job's. Nothing was written.
var ErrOwnershipLost = errors.New("job ownership lost: claim token is stale")

func (k ErrorKind) valid() bool {
	switch k {
	case KindHandlerError, KindNonRetryable, KindPanic, KindMissingHandler:
		return true
	}
	return false
}

// Finalizer writes the outcome of a claimed attempt.
type Finalizer struct {
	pool           *pgxpool.Pool
	maxResultBytes int64
}

// New builds a Finalizer. Results larger than maxResultBytes are dropped.
func New(pool *pgxpool.Pool, maxResultBytes int64) (*Finalizer, error) {
	if pool == nil {
		return nil, errors.New("finalizer needs a database pool")
	}
	if maxResultBytes <= 0 {
		return nil, errors.New("finalizer max result bytes must be > 0")
	}
	return &Finalizer{pool: pool, maxResultBytes: maxResultBytes}, nil
}

// outcome is one final state for the attempt and the job.
type outcome struct {
	jobStatus     domain.JobStatus
	lastError     *string
	attemptStatus domain.AttemptStatus
	attemptErr    *string
	errorKind     *string
	result        []byte
	eventType     string
	eventData     map[string]any
}

// Complete marks the attempt and job completed and stores result, or drops it
// when it is over the size limit. The job still completes.
func (f *Finalizer) Complete(ctx context.Context, cj claim.ClaimedJob, result json.RawMessage) error {
	kept, note := boundResult(result, f.maxResultBytes)
	o := outcome{
		jobStatus:     domain.JobCompleted,
		attemptStatus: domain.AttemptCompleted,
		result:        kept,
		eventType:     eventsrepo.TypeAttemptCompleted,
		eventData:     map[string]any{"attempt_number": cj.AttemptNumber},
	}
	if note != "" {
		o.attemptErr = &note
	}
	return f.finish(ctx, cj, o)
}

// Fail marks the attempt and job failed and stores the truncated error text.
func (f *Finalizer) Fail(ctx context.Context, cj claim.ClaimedJob, err error, kind ErrorKind) error {
	if err == nil {
		return errors.New("fail needs an error; use Complete for success")
	}
	if !kind.valid() {
		return fmt.Errorf("unknown error kind %q", kind)
	}
	text := truncateText(err.Error(), MaxErrorBytes)
	k := string(kind)
	o := outcome{
		jobStatus:     domain.JobFailed,
		lastError:     &text,
		attemptStatus: domain.AttemptFailed,
		attemptErr:    &text,
		errorKind:     &k,
		eventType:     eventsrepo.TypeAttemptFailed,
		eventData:     map[string]any{"attempt_number": cj.AttemptNumber, "error_kind": k},
	}
	return f.finish(ctx, cj, o)
}

// finish runs the job update, the attempt update and the events in one
// transaction. A failed job update means the claim is stale, so nothing is written.
func (f *Finalizer) finish(ctx context.Context, cj claim.ClaimedJob, o outcome) error {
	return db.WithTx(ctx, f.pool, func(tx pgx.Tx) error {
		owned, err := jobsrepo.FinishOwned(ctx, tx, cj.Job.ID, cj.ClaimToken, o.jobStatus, o.lastError)
		if err != nil {
			return err
		}
		if !owned {
			return ErrOwnershipLost
		}
		done, err := attemptsrepo.Finish(ctx, tx, cj.AttemptID, o.attemptStatus, o.attemptErr, o.errorKind, o.result)
		if err != nil {
			return err
		}
		if !done {
			return fmt.Errorf("attempt %s is not running", cj.AttemptID)
		}
		return appendEvents(ctx, tx, cj, o)
	})
}

func appendEvents(ctx context.Context, tx pgx.Tx, cj claim.ClaimedJob, o outcome) error {
	attemptID := cj.AttemptID
	data, err := json.Marshal(o.eventData)
	if err != nil {
		return fmt.Errorf("marshal %s data: %w", o.eventType, err)
	}
	if err := eventsrepo.Append(ctx, tx, eventsrepo.Event{
		JobID: cj.Job.ID, AttemptID: &attemptID, Type: o.eventType, Data: data,
	}); err != nil {
		return err
	}
	statusData, err := json.Marshal(map[string]string{
		"from": string(domain.JobRunning), "to": string(o.jobStatus),
	})
	if err != nil {
		return fmt.Errorf("marshal status data: %w", err)
	}
	return eventsrepo.Append(ctx, tx, eventsrepo.Event{
		JobID: cj.Job.ID, AttemptID: &attemptID, Type: eventsrepo.TypeJobStatusChanged, Data: statusData,
	})
}

// boundResult returns result unchanged when it fits the limit. Otherwise it
// returns nil and a note for the attempt. An empty result stays nil with no note.
func boundResult(result json.RawMessage, limit int64) (json.RawMessage, string) {
	if len(result) == 0 {
		return nil, ""
	}
	if int64(len(result)) > limit {
		return nil, fmt.Sprintf("result dropped: %d bytes exceeds limit of %d", len(result), limit)
	}
	return result, ""
}

// truncateText cuts s to at most max bytes without splitting a UTF-8 character.
func truncateText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
