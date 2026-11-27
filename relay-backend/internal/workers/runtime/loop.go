package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
)

// claimLoop takes a job only when an execution slot is free, so the number of
// running handlers never exceeds Concurrency. It returns when st.ctx ends.
func (r *Runtime) claimLoop(st *run) {
	for {
		select {
		case <-st.ctx.Done():
			return
		case st.slots <- struct{}{}:
		}

		cj, err := r.cfg.Claimer.Claim(st.ctx, r.cfg.WorkerID, r.cfg.Queues, r.handlers)
		if cj != nil {
			// The claim already committed. Run it even during shutdown, or it stays
			// in running until its lease expires.
			r.startAttempt(st, *cj)
			continue
		}
		<-st.slots
		if st.ctx.Err() != nil {
			return
		}
		if err != nil {
			r.cfg.Logger.Warn("claim failed, will retry", "error", err)
		}
		if !sleepCtx(st.ctx, r.cfg.PollInterval) {
			return
		}
	}
}

// startAttempt runs one claimed job in its own goroutine. The slot is freed when it ends.
func (r *Runtime) startAttempt(st *run, cj claim.ClaimedJob) {
	st.wg.Add(1)
	st.running.Add(1)
	go func() {
		defer st.wg.Done()
		defer st.running.Add(-1)
		defer func() { <-st.slots }()
		r.execute(st.handlerCtx, cj)
	}()
}

// drain waits for running attempts to finish. After ShutdownTimeout it cancels
// their handler contexts and waits a short time more. Attempts still running
// after that are left to lease expiry.
func (r *Runtime) drain(st *run) {
	r.cfg.Logger.Info("shutting down worker", "timeout", r.cfg.ShutdownTimeout.String(), "running", st.running.Load())
	if waitTimeout(&st.wg, r.cfg.ShutdownTimeout) {
		return
	}
	r.cfg.Logger.Warn("shutdown timeout reached, cancelling running handlers", "running", st.running.Load())
	st.cancelHandler()
	if waitTimeout(&st.wg, cancelDrainTimeout) {
		return
	}
	r.cfg.Logger.Warn("abandoning handlers that ignored cancel, lease expiry will recover them", "running", st.running.Load())
}

// waitTimeout reports whether wg finished within d.
func waitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// sleepCtx waits d and reports false when ctx ends first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
