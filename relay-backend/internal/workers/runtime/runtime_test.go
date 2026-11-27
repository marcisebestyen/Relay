package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/finalize"
	rt "github.com/marcisebestyen/relay/relay-backend/internal/workers/runtime"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	cases := map[string]func(h *harness){
		"zero concurrency":   func(h *harness) { h.cfg.Concurrency = 0 },
		"zero poll interval": func(h *harness) { h.cfg.PollInterval = 0 },
		"zero heartbeat":     func(h *harness) { h.cfg.HeartbeatInterval = 0 },
		"zero shutdown":      func(h *harness) { h.cfg.ShutdownTimeout = 0 },
		"no queues":          func(h *harness) { h.cfg.Queues = nil },
		"nil worker id":      func(h *harness) { h.cfg.WorkerID = uuid.Nil },
		"nil registry":       func(h *harness) { h.cfg.Registry = nil },
		"no handlers":        func(h *harness) { h.cfg.Registry = relay.NewRegistry() },
		"nil claimer":        func(h *harness) { h.cfg.Claimer = nil },
		"nil finalizer":      func(h *harness) { h.cfg.Finalizer = nil },
		"nil lifecycle":      func(h *harness) { h.cfg.Lifecycle = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			mutate(h)
			if _, err := rt.New(h.cfg); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestRunRegistersWorkerWithHandlersAndConcurrency(t *testing.T) {
	h := newHarness(t)
	h.cfg.Concurrency = 3
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	waitFor(t, "registration", func() bool { return len(h.lifecycle.registration().Handlers) > 0 })
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reg := h.lifecycle.registration()
	if len(reg.Handlers) != 1 || reg.Handlers[0] != "demo.echo" {
		t.Errorf("handlers = %v, want [demo.echo]", reg.Handlers)
	}
	if reg.Concurrency != 3 {
		t.Errorf("concurrency = %d, want 3", reg.Concurrency)
	}
	if len(reg.Queues) != 1 || reg.Queues[0] != "default" {
		t.Errorf("queues = %v, want [default]", reg.Queues)
	}
}

func TestRunStopsWhenRegistrationFails(t *testing.T) {
	h := newHarness(t, newJob("demo.echo"))
	h.lifecycle.registerErr = errors.New("db down")
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Run(context.Background()); err == nil {
		t.Fatal("want registration error, got nil")
	}
	if h.claimer.callCount() != 0 {
		t.Errorf("claimed %d times after failed registration, want 0", h.claimer.callCount())
	}
}

func TestSuccessfulHandlerCompletesWithResult(t *testing.T) {
	job := newJob("demo.echo")
	h := newHarness(t, job)
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	waitFor(t, "outcome", func() bool { return len(h.finalizer.list()) == 1 })
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	o := h.finalizer.list()[0]
	if o.kind != "" || o.jobID != job.Job.ID {
		t.Fatalf("outcome = %+v, want completion for %s", o, job.Job.ID)
	}
	if string(o.result) != `{"n":1}` {
		t.Errorf("result = %s, want echoed payload", o.result)
	}
}

func TestHandlerErrorsAreClassified(t *testing.T) {
	cases := map[string]struct {
		handler string
		fn      relay.HandlerFunc
		want    finalize.ErrorKind
	}{
		"plain error": {
			handler: "demo.plain",
			fn: func(context.Context, json.RawMessage) (json.RawMessage, error) {
				return nil, errBoom
			},
			want: finalize.KindHandlerError,
		},
		"non-retryable": {
			handler: "demo.nonretry",
			fn: func(context.Context, json.RawMessage) (json.RawMessage, error) {
				return nil, relay.NonRetryable(errBoom)
			},
			want: finalize.KindNonRetryable,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			job := newJob(tc.handler)
			h := newHarness(t, job)
			if err := h.registry.Register(tc.handler, tc.fn); err != nil {
				t.Fatalf("register: %v", err)
			}
			r, err := rt.New(h.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := startRun(ctx, r)
			waitFor(t, "outcome", func() bool { return len(h.finalizer.list()) == 1 })
			cancel()
			if err := waitRun(t, done, 2*time.Second); err != nil {
				t.Fatalf("Run: %v", err)
			}

			o := h.finalizer.list()[0]
			if o.kind != tc.want {
				t.Errorf("kind = %q, want %q", o.kind, tc.want)
			}
		})
	}
}

func TestPanicIsRecordedAndWorkerKeepsRunning(t *testing.T) {
	bad := newJob("demo.panic")
	good := newJob("demo.echo")
	h := newHarness(t, bad, good)
	if err := h.registry.Register("demo.panic", relay.HandlerFunc(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		panic("kaboom")
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	waitFor(t, "two outcomes", func() bool { return len(h.finalizer.list()) == 2 })
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	byJob := map[uuid.UUID]outcome{}
	for _, o := range h.finalizer.list() {
		byJob[o.jobID] = o
	}
	if got := byJob[bad.Job.ID]; got.kind != finalize.KindPanic {
		t.Errorf("panic job kind = %q, want %q", got.kind, finalize.KindPanic)
	}
	if got := byJob[good.Job.ID]; got.kind != "" {
		t.Errorf("good job kind = %q, want completion", got.kind)
	}
}

func TestMissingHandlerFailsAsMissingHandler(t *testing.T) {
	job := newJob("demo.unknown")
	h := newHarness(t, job)
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	waitFor(t, "outcome", func() bool { return len(h.finalizer.list()) == 1 })
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := h.finalizer.list()[0].kind; got != finalize.KindMissingHandler {
		t.Errorf("kind = %q, want %q", got, finalize.KindMissingHandler)
	}
}

func TestHandlerSeesJobInfo(t *testing.T) {
	job := newJob("demo.info")
	job.AttemptNumber = 2
	h := newHarness(t, job)
	seen := make(chan relay.JobInfo, 1)
	if err := h.registry.Register("demo.info", relay.HandlerFunc(func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		info, _ := relay.JobInfoFromContext(ctx)
		seen <- info
		return nil, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	info := <-seen
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := relay.JobInfo{JobID: job.Job.ID, AttemptNumber: 2, Queue: "default", WorkerID: h.workerID}
	if info != want {
		t.Errorf("job info = %+v, want %+v", info, want)
	}
}

func TestClaimErrorDoesNotStopLoop(t *testing.T) {
	job := newJob("demo.echo")
	h := newHarness(t, job)
	h.claimer.errOnce = errors.New("transient")
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	waitFor(t, "outcome after claim error", func() bool { return len(h.finalizer.list()) == 1 })
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestNeverMoreHandlersThanConcurrency(t *testing.T) {
	const total, slots = 30, 3
	jobs := make([]claim.ClaimedJob, 0, total)
	for range total {
		jobs = append(jobs, newJob("demo.slow"))
	}
	h := newHarness(t, jobs...)
	var mu sync.Mutex
	active, peak := 0, 0
	if err := h.registry.Register("demo.slow", relay.HandlerFunc(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		mu.Lock()
		active++
		peak = max(peak, active)
		mu.Unlock()
		time.Sleep(3 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return nil, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	h.cfg.Concurrency = slots
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	waitFor(t, "all outcomes", func() bool { return len(h.finalizer.list()) == total })
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > slots {
		t.Errorf("peak concurrent handlers = %d, want <= %d", peak, slots)
	}
	if peak < 2 {
		t.Errorf("peak concurrent handlers = %d, want parallel runs", peak)
	}
}

func TestShutdownStopsClaimingAndWaitsForRunningJob(t *testing.T) {
	running := newJob("demo.hold")
	waiting := newJob("demo.echo")
	h := newHarness(t, running, waiting)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := h.registry.Register("demo.hold", relay.HandlerFunc(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		close(started)
		<-release
		return nil, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	<-started
	cancel()

	select {
	case err := <-done:
		t.Fatalf("Run returned while a handler was still running, want it to wait (err=%v)", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if h.claimer.remaining() != 1 {
		t.Errorf("unclaimed jobs = %d, want 1 (no claims after shutdown)", h.claimer.remaining())
	}
	order := h.rec.list()
	if len(order) != 2 || order[0] != "complete:"+running.Job.ID.String() || order[1] != "stopped" {
		t.Errorf("event order = %v, want [complete ... stopped]", order)
	}
}

func TestShutdownCancelsHandlersAfterTimeout(t *testing.T) {
	job := newJob("demo.wait")
	h := newHarness(t, job)
	h.cfg.ShutdownTimeout = 100 * time.Millisecond
	started := make(chan struct{})
	sawCancel := make(chan struct{})
	if err := h.registry.Register("demo.wait", relay.HandlerFunc(func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		close(sawCancel)
		return nil, ctx.Err()
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	<-started
	cancel()

	if err := waitRun(t, done, 3*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-sawCancel:
	default:
		t.Fatal("handler context was not cancelled")
	}
	o := h.finalizer.list()
	if len(o) != 1 || o[0].kind != finalize.KindHandlerError {
		t.Fatalf("outcomes = %+v, want one handler_error for the cancelled job", o)
	}
	if order := h.rec.list(); order[len(order)-1] != "stopped" {
		t.Errorf("event order = %v, want stopped last", order)
	}
}

func TestShutdownAbandonsHandlerThatIgnoresCancel(t *testing.T) {
	job := newJob("demo.stuck")
	h := newHarness(t, job)
	h.cfg.ShutdownTimeout = 100 * time.Millisecond
	started := make(chan struct{})
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	if err := h.registry.Register("demo.stuck", relay.HandlerFunc(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		close(started)
		<-stuck
		return nil, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	<-started
	cancel()

	if err := waitRun(t, done, 3*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := len(h.finalizer.list()); n != 0 {
		t.Errorf("finalized %d abandoned attempts, want 0 (lease expiry handles them)", n)
	}
	if order := h.rec.list(); len(order) != 1 || order[0] != "stopped" {
		t.Errorf("event order = %v, want [stopped]", order)
	}
}

func TestRunReturnsPromptlyWhenIdle(t *testing.T) {
	h := newHarness(t)
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	waitFor(t, "a claim attempt", func() bool { return h.claimer.callCount() > 0 })
	cancel()
	if err := waitRun(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if order := h.rec.list(); len(order) != 1 || order[0] != "stopped" {
		t.Errorf("event order = %v, want [stopped]", order)
	}
}

// inFlightClaimer simulates a claim that commits just as shutdown starts: it
// waits for ctx to end, then still hands back a job.
type inFlightClaimer struct {
	job     claim.ClaimedJob
	entered chan struct{}
	once    sync.Once
}

func (c *inFlightClaimer) Claim(ctx context.Context, _ uuid.UUID, _, _ []string) (*claim.ClaimedJob, error) {
	var out *claim.ClaimedJob
	c.once.Do(func() {
		close(c.entered)
		<-ctx.Done()
		j := c.job
		out = &j
	})
	return out, nil
}

func TestClaimCommittedDuringShutdownIsStillRun(t *testing.T) {
	job := newJob("demo.echo")
	h := newHarness(t)
	h.cfg.Claimer = &inFlightClaimer{job: job, entered: make(chan struct{})}
	r, err := rt.New(h.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startRun(ctx, r)
	<-h.cfg.Claimer.(*inFlightClaimer).entered
	cancel()

	if err := waitRun(t, done, 3*time.Second); err != nil {
		t.Fatalf("Run: %v", err)
	}
	o := h.finalizer.list()
	if len(o) != 1 || o[0].jobID != job.Job.ID || o[0].kind != "" {
		t.Fatalf("outcomes = %+v, want the claimed job completed (not stranded in running)", o)
	}
}
