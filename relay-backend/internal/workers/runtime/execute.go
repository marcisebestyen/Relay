package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/finalize"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

// errPanic marks an error that came from a handler panic.
var errPanic = errors.New("handler panicked")

// execute runs one attempt: dispatch to the handler, then write the outcome.
// It never returns an error; problems are logged.
func (r *Runtime) execute(ctx context.Context, cj claim.ClaimedJob) {
	start := time.Now()
	jobCtx := relay.WithJobInfo(ctx, relay.JobInfo{
		JobID:         cj.Job.ID,
		AttemptNumber: cj.AttemptNumber,
		Queue:         cj.Job.Queue,
		WorkerID:      r.cfg.WorkerID,
	})
	// Finalize must still run after the handler context is cancelled.
	finCtx := context.WithoutCancel(ctx)

	outcome, ferr := r.settle(finCtx, jobCtx, cj)
	r.logAttempt(cj, outcome, time.Since(start), ferr)
}

// settle runs the handler and records its result. It returns the outcome name for logs.
func (r *Runtime) settle(finCtx, jobCtx context.Context, cj claim.ClaimedJob) (string, error) {
	h, ok := r.cfg.Registry.Lookup(cj.Job.Handler)
	if !ok {
		err := fmt.Errorf("no handler registered for %q", cj.Job.Handler)
		return string(finalize.KindMissingHandler), r.cfg.Finalizer.Fail(finCtx, cj, err, finalize.KindMissingHandler)
	}
	result, err := invoke(jobCtx, h, cj.Job.Payload)
	if err != nil {
		kind := classify(err)
		return string(kind), r.cfg.Finalizer.Fail(finCtx, cj, err, kind)
	}
	return "completed", r.cfg.Finalizer.Complete(finCtx, cj, result)
}

// invoke calls h and turns a panic into an errPanic error.
func invoke(ctx context.Context, h relay.Handler, payload json.RawMessage) (result json.RawMessage, err error) {
	defer func() {
		if p := recover(); p != nil {
			result, err = nil, fmt.Errorf("%w: %v", errPanic, p)
		}
	}()
	return h.Handle(ctx, payload)
}

func classify(err error) finalize.ErrorKind {
	switch {
	case errors.Is(err, errPanic):
		return finalize.KindPanic
	case relay.IsNonRetryable(err):
		return finalize.KindNonRetryable
	default:
		return finalize.KindHandlerError
	}
}

func (r *Runtime) logAttempt(cj claim.ClaimedJob, outcome string, d time.Duration, ferr error) {
	attrs := []any{
		"job_id", cj.Job.ID.String(),
		"attempt_id", cj.AttemptID.String(),
		"worker_id", r.cfg.WorkerID.String(),
		"queue", cj.Job.Queue,
		"handler", cj.Job.Handler,
		"outcome", outcome,
		"duration", d.String(),
	}
	switch {
	case ferr == nil:
		r.cfg.Logger.Info("attempt finished", attrs...)
	case errors.Is(ferr, finalize.ErrOwnershipLost):
		r.cfg.Logger.Warn("attempt ownership lost, nothing written", attrs...)
	default:
		r.cfg.Logger.Error("attempt finalize failed", append(attrs, "error", ferr.Error())...)
	}
}
