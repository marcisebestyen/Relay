//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func TestOpenPingsDatabase(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, config.Database{URL: pgtest.NewDatabase(t)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("query: %v (got %d)", err, one)
	}
}

func TestCloseReturnsWhenIdle(t *testing.T) {
	pool := pgtest.OpenPool(t, pgtest.NewDatabase(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Close(ctx, pool); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCloseGivesUpWhenContextEnds(t *testing.T) {
	pool := pgtest.OpenPool(t, pgtest.NewDatabase(t))
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := db.Close(ctx, pool); err == nil {
		t.Fatal("expected context error while a connection is held")
	}
}
