//go:build integration

package queuesrepo_test

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/queuesrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func TestEnsureCreatesNewQueue(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	if err := queuesrepo.Ensure(ctx, pool, "emails"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM queues WHERE name = 'emails'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, err = %v, want 1", n, err)
	}
}

func TestEnsureExistingQueueIsNoOp(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	for range 2 {
		if err := queuesrepo.Ensure(ctx, pool, "default"); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM queues WHERE name = 'default'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, err = %v, want 1", n, err)
	}
}

func TestEnsureConcurrentOnSameNewQueue(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	const workers = 20
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- db.WithTx(ctx, pool, func(tx pgx.Tx) error { return queuesrepo.Ensure(ctx, tx, "burst") })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Ensure: %v", err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM queues WHERE name = 'burst'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, err = %v, want 1", n, err)
	}
}

func TestEnsureWrapsError(t *testing.T) {
	pool := pgtest.NewPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := queuesrepo.Ensure(ctx, pool, "x")
	if err == nil {
		t.Fatal("want error")
	}
}
