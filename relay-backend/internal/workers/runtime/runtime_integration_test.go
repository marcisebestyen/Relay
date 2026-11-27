//go:build integration

package runtime_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/finalize"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
	rt "github.com/marcisebestyen/relay/relay-backend/internal/workers/runtime"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

const sleepHandler = "demo.sleep"

func createJobs(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	svc := jobs.NewService(pool, domain.Defaults{Queue: "default", Priority: 50, MaxAttempts: 3}, nil, nil)
	for range n {
		if _, err := svc.Create(context.Background(), domain.NewJobInput{Handler: sleepHandler, Queue: "default"}); err != nil {
			t.Fatalf("create job: %v", err)
		}
	}
}

func countJobs(t *testing.T, pool *pgxpool.Pool, status domain.JobStatus) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM jobs WHERE status = $1", string(status)).Scan(&n); err != nil {
		t.Fatalf("count %s jobs: %v", status, err)
	}
	return n
}

func waitForCount(t *testing.T, pool *pgxpool.Pool, status domain.JobStatus, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for countJobs(t, pool, status) < want {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s jobs = %d, want %d", status, countJobs(t, pool, status), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// newIntegrationRuntime builds a runtime on real claim, finalize and lifecycle code.
func newIntegrationRuntime(t *testing.T, pool *pgxpool.Pool, concurrency int, h relay.Handler) (*rt.Runtime, uuid.UUID) {
	t.Helper()
	reg := relay.NewRegistry()
	if err := reg.Register(sleepHandler, h); err != nil {
		t.Fatalf("register: %v", err)
	}
	id := workers.Identity{ID: uuid.New(), Hostname: "test", PID: os.Getpid(), Version: "test"}
	claimer, err := claim.NewClaimer(pool, 30*time.Second)
	if err != nil {
		t.Fatalf("claimer: %v", err)
	}
	fin, err := finalize.New(pool, 64<<10)
	if err != nil {
		t.Fatalf("finalizer: %v", err)
	}
	r, err := rt.New(rt.Config{
		WorkerID:          id.ID,
		Queues:            []string{"default"},
		Registry:          reg,
		Concurrency:       concurrency,
		PollInterval:      5 * time.Millisecond,
		HeartbeatInterval: time.Hour,
		ShutdownTimeout:   5 * time.Second,
		Claimer:           claimer,
		Finalizer:         fin,
		Lifecycle:         rt.NewLifecycle(pool, id),
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	return r, id.ID
}

func TestFiftyJobsCompleteWithConcurrencyFour(t *testing.T) {
	pool := pgtest.NewPool(t)
	createJobs(t, pool, 50)

	var mu sync.Mutex
	active, peak := 0, 0
	r, workerID := newIntegrationRuntime(t, pool, 4, relay.HandlerFunc(func(_ context.Context, p json.RawMessage) (json.RawMessage, error) {
		mu.Lock()
		active++
		peak = max(peak, active)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return p, nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	waitForCount(t, pool, domain.JobCompleted, 50)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > 4 || peak < 2 {
		t.Errorf("peak concurrent handlers = %d, want between 2 and 4", peak)
	}
	assertWorkerStopped(t, pool, workerID)
}

func TestShutdownMidRunFinalizesRunningAndLeavesRestPending(t *testing.T) {
	pool := pgtest.NewPool(t)
	createJobs(t, pool, 20)

	r, workerID := newIntegrationRuntime(t, pool, 2, relay.HandlerFunc(func(_ context.Context, p json.RawMessage) (json.RawMessage, error) {
		time.Sleep(50 * time.Millisecond)
		return p, nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	waitForCount(t, pool, domain.JobCompleted, 3)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not exit after shutdown")
	}

	if running := countJobs(t, pool, domain.JobRunning); running != 0 {
		t.Errorf("running jobs after shutdown = %d, want 0", running)
	}
	completed := countJobs(t, pool, domain.JobCompleted)
	pending := countJobs(t, pool, domain.JobPending)
	if completed < 3 || completed+pending != 20 {
		t.Errorf("completed=%d pending=%d, want completed>=3 and completed+pending=20", completed, pending)
	}
	assertWorkerStopped(t, pool, workerID)
}

func assertWorkerStopped(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	var stopped bool
	if err := pool.QueryRow(context.Background(), "SELECT stopped_at IS NOT NULL FROM workers WHERE id = $1", id).Scan(&stopped); err != nil {
		t.Fatalf("read worker row: %v", err)
	}
	if !stopped {
		t.Error("worker row has no stopped_at after clean shutdown")
	}
}
