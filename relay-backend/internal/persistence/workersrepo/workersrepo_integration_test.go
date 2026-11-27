//go:build integration

package workersrepo_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/workersrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

var start = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func newWorker() workersrepo.NewWorker {
	return workersrepo.NewWorker{
		ID:          uuid.New(),
		Hostname:    "host-a",
		PID:         42,
		Version:     "test",
		Queues:      []string{"default"},
		Handlers:    []string{"emails.send"},
		Concurrency: 4,
		StartedAt:   start,
	}
}

func TestInsertStoresWorkerRow(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	w := newWorker()
	if err := workersrepo.Insert(ctx, pool, w); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var (
		hostname    string
		queues      []string
		handlers    []string
		concurrency int
		started     time.Time
		last        time.Time
	)
	err := pool.QueryRow(ctx, `
		SELECT hostname, queues, handlers, concurrency, started_at, last_heartbeat_at
		FROM workers WHERE id = $1`, w.ID).
		Scan(&hostname, &queues, &handlers, &concurrency, &started, &last)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if hostname != "host-a" || concurrency != 4 {
		t.Errorf("hostname=%q concurrency=%d", hostname, concurrency)
	}
	if len(queues) != 1 || queues[0] != "default" || len(handlers) != 1 || handlers[0] != "emails.send" {
		t.Errorf("queues=%v handlers=%v", queues, handlers)
	}
	if !started.Equal(start) || !last.Equal(start) {
		t.Errorf("started_at=%s last_heartbeat_at=%s, want %s", started, last, start)
	}
}

func TestInsertStoresNilListsAsEmptyArrays(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	w := newWorker()
	w.Queues = nil
	w.Handlers = nil
	if err := workersrepo.Insert(ctx, pool, w); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var queuesNotNull, handlersNotNull bool
	err := pool.QueryRow(ctx, `
		SELECT queues IS NOT NULL, handlers IS NOT NULL FROM workers WHERE id = $1`, w.ID).
		Scan(&queuesNotNull, &handlersNotNull)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !queuesNotNull || !handlersNotNull {
		t.Fatal("nil lists stored as NULL, want empty arrays")
	}
}

func TestHeartbeatAdvancesLastHeartbeat(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	w := newWorker()
	if err := workersrepo.Insert(ctx, pool, w); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	beat := start.Add(10 * time.Second)
	if err := workersrepo.Heartbeat(ctx, pool, w.ID, beat); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	var last time.Time
	if err := pool.QueryRow(ctx, `SELECT last_heartbeat_at FROM workers WHERE id = $1`, w.ID).Scan(&last); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !last.Equal(beat) {
		t.Fatalf("last_heartbeat_at = %s, want %s", last, beat)
	}
}

func TestHeartbeatUnknownWorkerReturnsNotFound(t *testing.T) {
	pool := pgtest.NewPool(t)
	err := workersrepo.Heartbeat(context.Background(), pool, uuid.New(), start)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMarkStoppedSetsStoppedAt(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	w := newWorker()
	if err := workersrepo.Insert(ctx, pool, w); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	stop := start.Add(time.Minute)
	if err := workersrepo.MarkStopped(ctx, pool, w.ID, stop); err != nil {
		t.Fatalf("MarkStopped: %v", err)
	}
	var stopped time.Time
	if err := pool.QueryRow(ctx, `SELECT stopped_at FROM workers WHERE id = $1`, w.ID).Scan(&stopped); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !stopped.Equal(stop) {
		t.Fatalf("stopped_at = %s, want %s", stopped, stop)
	}
}

func TestMarkStoppedKeepsFirstStopTime(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	w := newWorker()
	if err := workersrepo.Insert(ctx, pool, w); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	first := start.Add(time.Minute)
	if err := workersrepo.MarkStopped(ctx, pool, w.ID, first); err != nil {
		t.Fatalf("first MarkStopped: %v", err)
	}
	if err := workersrepo.MarkStopped(ctx, pool, w.ID, first.Add(time.Hour)); err != nil {
		t.Fatalf("second MarkStopped: %v", err)
	}
	var stopped time.Time
	if err := pool.QueryRow(ctx, `SELECT stopped_at FROM workers WHERE id = $1`, w.ID).Scan(&stopped); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !stopped.Equal(first) {
		t.Fatalf("stopped_at = %s, want first stop %s", stopped, first)
	}
}

func TestMarkStoppedUnknownWorkerReturnsNotFound(t *testing.T) {
	pool := pgtest.NewPool(t)
	err := workersrepo.MarkStopped(context.Background(), pool, uuid.New(), start)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
