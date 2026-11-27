//go:build integration

package workers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

type workerRow struct {
	lastHeartbeat time.Time
	stoppedAt     *time.Time
}

func readWorker(t *testing.T, q db.Querier, id uuid.UUID) workerRow {
	t.Helper()
	var r workerRow
	err := q.QueryRow(context.Background(),
		`SELECT last_heartbeat_at, stopped_at FROM workers WHERE id = $1`, id).
		Scan(&r.lastHeartbeat, &r.stoppedAt)
	if err != nil {
		t.Fatalf("read worker %s: %v", id, err)
	}
	return r
}

func TestRegisterHeartbeatStopLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	id, err := workers.NewIdentity("test")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	reg := workers.Registration{
		Queues:      []string{"default"},
		Handlers:    []string{"emails.send"},
		Concurrency: 4,
		StartedAt:   start,
	}

	if err := workers.Register(ctx, pool, id, reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := readWorker(t, pool, id.ID); !got.lastHeartbeat.Equal(start) || got.stoppedAt != nil {
		t.Fatalf("after register: %+v", got)
	}

	beat := start.Add(10 * time.Second)
	if err := workers.Heartbeat(ctx, pool, id.ID, beat); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if got := readWorker(t, pool, id.ID); !got.lastHeartbeat.Equal(beat) {
		t.Fatalf("last_heartbeat_at = %s, want %s", got.lastHeartbeat, beat)
	}

	stop := start.Add(20 * time.Second)
	if err := workers.MarkStopped(ctx, pool, id.ID, stop); err != nil {
		t.Fatalf("MarkStopped: %v", err)
	}
	if got := readWorker(t, pool, id.ID); got.stoppedAt == nil || !got.stoppedAt.Equal(stop) {
		t.Fatalf("stopped_at = %v, want %s", got.stoppedAt, stop)
	}
}

func TestHeartbeatLoopSurvivesOneDBError(t *testing.T) {
	pool := pgtest.NewPool(t)
	id, err := workers.NewIdentity("test")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if err := workers.Register(context.Background(), pool, id, workers.Registration{
		Queues: []string{"default"}, Concurrency: 1, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	results := make(chan error, 10)
	var calls atomic.Int32
	loop := workers.HeartbeatLoop{
		Interval: 5 * time.Millisecond,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Beat: func(ctx context.Context, at time.Time) error {
			if calls.Add(1) == 1 {
				results <- errors.New("simulated db outage")
				return errors.New("simulated db outage")
			}
			err := workers.Heartbeat(ctx, pool, id.ID, at)
			results <- err
			return err
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- loop.Run(ctx) }()

	if err := <-results; err == nil {
		t.Fatal("first beat: want simulated error")
	}
	if err := <-results; err != nil {
		t.Fatalf("beat after outage: %v", err)
	}
	cancel()
	if err := <-runErr; err != nil {
		t.Fatalf("Run returned %v, want nil after cancel", err)
	}
}
