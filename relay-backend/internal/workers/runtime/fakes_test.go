package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/finalize"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
	rt "github.com/marcisebestyen/relay/relay-backend/internal/workers/runtime"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

// recorder keeps one ordered log of events across fakes.
type recorder struct {
	mu    sync.Mutex
	items []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, s)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.items...)
}

// fakeClaimer hands out preset jobs, then nil (nothing eligible).
type fakeClaimer struct {
	mu      sync.Mutex
	jobs    []claim.ClaimedJob
	calls   int
	errOnce error
}

func (c *fakeClaimer) Claim(_ context.Context, _ uuid.UUID, _, _ []string) (*claim.ClaimedJob, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.errOnce != nil {
		err := c.errOnce
		c.errOnce = nil
		return nil, err
	}
	if len(c.jobs) == 0 {
		return nil, nil
	}
	j := c.jobs[0]
	c.jobs = c.jobs[1:]
	return &j, nil
}

func (c *fakeClaimer) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *fakeClaimer) remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.jobs)
}

// outcome is one finalize call. kind is empty for Complete.
type outcome struct {
	jobID  uuid.UUID
	kind   finalize.ErrorKind
	result json.RawMessage
	err    string
}

// fakeFinalizer records every finalize call.
type fakeFinalizer struct {
	rec      *recorder
	mu       sync.Mutex
	outcomes []outcome
}

func (f *fakeFinalizer) Complete(_ context.Context, cj claim.ClaimedJob, result json.RawMessage) error {
	f.add(outcome{jobID: cj.Job.ID, result: result})
	f.rec.add("complete:" + cj.Job.ID.String())
	return nil
}

func (f *fakeFinalizer) Fail(_ context.Context, cj claim.ClaimedJob, err error, kind finalize.ErrorKind) error {
	f.add(outcome{jobID: cj.Job.ID, kind: kind, err: err.Error()})
	f.rec.add("fail:" + string(kind) + ":" + cj.Job.ID.String())
	return nil
}

func (f *fakeFinalizer) add(o outcome) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcomes = append(f.outcomes, o)
}

func (f *fakeFinalizer) list() []outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]outcome(nil), f.outcomes...)
}

// fakeLifecycle records registration, heartbeats and the clean stop.
type fakeLifecycle struct {
	rec         *recorder
	registerErr error
	mu          sync.Mutex
	reg         workers.Registration
	beats       int
}

func (l *fakeLifecycle) Register(_ context.Context, reg workers.Registration) error {
	if l.registerErr != nil {
		return l.registerErr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reg = reg
	return nil
}

func (l *fakeLifecycle) Heartbeat(context.Context, time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.beats++
	return nil
}

func (l *fakeLifecycle) MarkStopped(context.Context, time.Time) error {
	l.rec.add("stopped")
	return nil
}

func (l *fakeLifecycle) registration() workers.Registration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reg
}

// harness is a runtime config wired to fakes.
type harness struct {
	cfg       rt.Config
	claimer   *fakeClaimer
	finalizer *fakeFinalizer
	lifecycle *fakeLifecycle
	rec       *recorder
	workerID  uuid.UUID
	registry  *relay.Registry
}

func newHarness(t *testing.T, jobs ...claim.ClaimedJob) *harness {
	t.Helper()
	rec := &recorder{}
	h := &harness{
		claimer:   &fakeClaimer{jobs: jobs},
		finalizer: &fakeFinalizer{rec: rec},
		lifecycle: &fakeLifecycle{rec: rec},
		rec:       rec,
		workerID:  uuid.New(),
		registry:  relay.NewRegistry(),
	}
	if err := h.registry.Register("demo.echo", relay.HandlerFunc(func(_ context.Context, p json.RawMessage) (json.RawMessage, error) {
		return p, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	h.cfg = rt.Config{
		WorkerID:          h.workerID,
		Queues:            []string{"default"},
		Registry:          h.registry,
		Concurrency:       1,
		PollInterval:      time.Millisecond,
		HeartbeatInterval: time.Hour,
		ShutdownTimeout:   5 * time.Second,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Claimer:           h.claimer,
		Finalizer:         h.finalizer,
		Lifecycle:         h.lifecycle,
	}
	return h
}

func newJob(handler string) claim.ClaimedJob {
	return claim.ClaimedJob{
		Job: domain.Job{
			ID:      uuid.New(),
			Handler: handler,
			Queue:   "default",
			Payload: json.RawMessage(`{"n":1}`),
		},
		AttemptID:     uuid.New(),
		AttemptNumber: 1,
		ClaimToken:    uuid.New(),
	}
}

// startRun runs the runtime in a goroutine. The returned channel gets Run's error.
func startRun(ctx context.Context, r *rt.Runtime) <-chan error {
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	return done
}

// waitFor polls cond until it is true or the test fails.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitRun(t *testing.T, done <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		t.Fatalf("Run did not return within %s", within)
		return nil
	}
}

var errBoom = errors.New("boom")
