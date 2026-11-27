// Package runtime runs a worker process: it claims jobs into bounded execution
// slots, dispatches them to handlers, and shuts down gracefully.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/finalize"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

// cancelDrainTimeout bounds how long shutdown waits for handlers after it
// cancels their contexts. Handlers that still run after it are abandoned.
const cancelDrainTimeout = 2 * time.Second

// ErrNoHandlers means the registry is empty, so the worker would claim nothing.
var ErrNoHandlers = errors.New("no handlers registered: register at least one handler before starting a worker")

// Claimer takes one eligible job for a worker, or nil when none is eligible.
type Claimer interface {
	Claim(ctx context.Context, workerID uuid.UUID, queues, handlers []string) (*claim.ClaimedJob, error)
}

// Finalizer writes the outcome of a claimed attempt.
type Finalizer interface {
	Complete(ctx context.Context, cj claim.ClaimedJob, result json.RawMessage) error
	Fail(ctx context.Context, cj claim.ClaimedJob, err error, kind finalize.ErrorKind) error
}

// Lifecycle records this worker's row: registration, heartbeats and clean stop.
type Lifecycle interface {
	Register(ctx context.Context, reg workers.Registration) error
	Heartbeat(ctx context.Context, at time.Time) error
	MarkStopped(ctx context.Context, at time.Time) error
}

// Config holds everything a Runtime needs. Every field is required except Logger and Now.
type Config struct {
	WorkerID          uuid.UUID
	Queues            []string
	Registry          *relay.Registry
	Concurrency       int
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	ShutdownTimeout   time.Duration
	// Logger receives per-attempt logs. Nil means slog.Default.
	Logger *slog.Logger
	// Now gives the time for registration and stop. Nil means time.Now.
	Now       func() time.Time
	Claimer   Claimer
	Finalizer Finalizer
	Lifecycle Lifecycle
}

// Runtime is one worker process. Build it with New and start it with Run.
type Runtime struct {
	cfg      Config
	handlers []string
}

// New checks cfg and returns a Runtime. It fails when the worker would have
// nothing to do, such as no handlers.
func New(cfg Config) (*Runtime, error) {
	switch {
	case cfg.WorkerID == uuid.Nil:
		return nil, errors.New("runtime needs a worker id")
	case len(cfg.Queues) == 0:
		return nil, errors.New("runtime needs at least one queue")
	case cfg.Registry == nil || len(cfg.Registry.IDs()) == 0:
		return nil, ErrNoHandlers
	case cfg.Concurrency < 1:
		return nil, errors.New("runtime concurrency must be >= 1")
	case cfg.PollInterval <= 0:
		return nil, errors.New("runtime poll interval must be > 0")
	case cfg.HeartbeatInterval <= 0:
		return nil, errors.New("runtime heartbeat interval must be > 0")
	case cfg.ShutdownTimeout <= 0:
		return nil, errors.New("runtime shutdown timeout must be > 0")
	case cfg.Claimer == nil, cfg.Finalizer == nil, cfg.Lifecycle == nil:
		return nil, errors.New("runtime needs a claimer, finalizer and lifecycle")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Runtime{cfg: cfg, handlers: cfg.Registry.IDs()}, nil
}

// run holds the state of one Run call.
type run struct {
	ctx           context.Context // stops claiming
	handlerCtx    context.Context // stops handlers; cancelled only after the grace period
	cancelHandler context.CancelFunc
	slots         chan struct{}
	wg            sync.WaitGroup
	running       atomic.Int64
}

// Run registers the worker, claims and runs jobs until ctx ends, then shuts
// down: no new claims, running handlers get ShutdownTimeout, leftovers are
// cancelled and abandoned to lease expiry, and the worker row is marked stopped.
// It returns an error when registration or the stop record fails.
func (r *Runtime) Run(ctx context.Context) error {
	now := r.cfg.Now().UTC()
	reg := workers.Registration{
		Queues:      r.cfg.Queues,
		Handlers:    r.handlers,
		Concurrency: r.cfg.Concurrency,
		StartedAt:   now,
	}
	if err := r.cfg.Lifecycle.Register(ctx, reg); err != nil {
		return fmt.Errorf("register worker: %w", err)
	}

	hbCtx, stopHeartbeat := context.WithCancel(context.WithoutCancel(ctx))
	hbDone := make(chan error, 1)
	go func() { hbDone <- r.heartbeat(hbCtx) }()

	handlerCtx, cancelHandler := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelHandler()
	st := &run{
		ctx:           ctx,
		handlerCtx:    handlerCtx,
		cancelHandler: cancelHandler,
		slots:         make(chan struct{}, r.cfg.Concurrency),
	}

	r.claimLoop(st)
	r.drain(st)

	stopHeartbeat()
	hbErr := <-hbDone
	if hbErr != nil {
		return fmt.Errorf("heartbeat: %w", hbErr)
	}
	if err := r.cfg.Lifecycle.MarkStopped(context.WithoutCancel(ctx), r.cfg.Now().UTC()); err != nil {
		return fmt.Errorf("mark worker stopped: %w", err)
	}
	return nil
}

func (r *Runtime) heartbeat(ctx context.Context) error {
	loop := workers.HeartbeatLoop{
		Interval: r.cfg.HeartbeatInterval,
		Now:      r.cfg.Now,
		Logger:   r.cfg.Logger,
		Beat:     r.cfg.Lifecycle.Heartbeat,
	}
	return loop.Run(ctx)
}
