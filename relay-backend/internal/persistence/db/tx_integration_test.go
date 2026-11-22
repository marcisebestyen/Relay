//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func txPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := pgtest.NewPool(t)
	return pool
}

func queueCount(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM queues WHERE name = $1`, name).Scan(&n); err != nil {
		t.Fatalf("count queues: %v", err)
	}
	return n
}

func insertQueue(tx pgx.Tx, name string) error {
	_, err := tx.Exec(context.Background(), `INSERT INTO queues (name) VALUES ($1)`, name)
	return err
}

func TestWithTxCommitsWhenFnSucceeds(t *testing.T) {
	pool := txPool(t)
	err := db.WithTx(context.Background(), pool, func(tx pgx.Tx) error { return insertQueue(tx, "committed") })
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if got := queueCount(t, pool, "committed"); got != 1 {
		t.Fatalf("queue rows = %d, want 1", got)
	}
}

func TestWithTxRollsBackAndReturnsFnError(t *testing.T) {
	pool := txPool(t)
	boom := errors.New("boom")
	err := db.WithTx(context.Background(), pool, func(tx pgx.Tx) error {
		if err := insertQueue(tx, "rolled-back"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got := queueCount(t, pool, "rolled-back"); got != 0 {
		t.Fatalf("queue rows = %d, want 0", got)
	}
}

func TestWithTxRollsBackAndRepanics(t *testing.T) {
	pool := txPool(t)
	func() {
		defer func() {
			if r := recover(); r != "kaboom" {
				t.Fatalf("recovered %v, want kaboom", r)
			}
		}()
		_ = db.WithTx(context.Background(), pool, func(tx pgx.Tx) error {
			if err := insertQueue(tx, "panicked"); err != nil {
				return err
			}
			panic("kaboom")
		})
	}()
	if got := queueCount(t, pool, "panicked"); got != 0 {
		t.Fatalf("queue rows = %d, want 0", got)
	}
	// The pool connection must be usable again after the panic.
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestWithTxRollsBackWhenContextIsCancelled(t *testing.T) {
	pool := txPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := insertQueue(tx, "cancelled"); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := queueCount(t, pool, "cancelled"); got != 0 {
		t.Fatalf("queue rows = %d, want 0", got)
	}
	if stat := pool.Stat(); stat.AcquiredConns() != 0 {
		t.Fatalf("acquired conns = %d, want 0 (connection leaked)", stat.AcquiredConns())
	}
}

func TestWithTxReturnsBeginError(t *testing.T) {
	pool := txPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := db.WithTx(ctx, pool, func(pgx.Tx) error { t.Fatal("fn must not run"); return nil })
	if err == nil {
		t.Fatal("want begin error")
	}
}
