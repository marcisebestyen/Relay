//go:build integration

package pgtest_test

import (
	"context"
	"os"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func TestDatabasesAreIsolated(t *testing.T) {
	ctx := context.Background()
	a := pgtest.NewPool(t)
	b := pgtest.NewPool(t)

	if _, err := a.Exec(ctx, "CREATE TABLE only_in_a (id int)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	var visible bool
	if err := b.QueryRow(ctx, "SELECT to_regclass('only_in_a') IS NOT NULL").Scan(&visible); err != nil {
		t.Fatalf("query: %v", err)
	}
	if visible {
		t.Error("table created in one test database is visible in another")
	}
}

func TestNewPoolAppliesMigrations(t *testing.T) {
	var n int
	err := pgtest.NewPool(t).QueryRow(context.Background(), "SELECT count(*) FROM goose_db_version").Scan(&n)
	if err != nil || n == 0 {
		t.Fatalf("expected goose version rows: n=%d err=%v", n, err)
	}
}
