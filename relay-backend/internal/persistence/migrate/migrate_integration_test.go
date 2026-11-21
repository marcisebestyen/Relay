//go:build integration

package migrate_test

import (
	"context"
	"os"
	"testing"
	"testing/fstest"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/migrate"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
	"github.com/marcisebestyen/relay/relay-backend/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

var fixture = fstest.MapFS{
	"00001_widgets.sql": {Data: []byte("-- +goose Up\nCREATE TABLE widgets (id int PRIMARY KEY);\n-- +goose Down\nDROP TABLE widgets;\n")},
	"00002_gadgets.sql": {Data: []byte("-- +goose Up\nCREATE TABLE gadgets (id int PRIMARY KEY);\n-- +goose Down\nDROP TABLE gadgets;\n")},
}

func tableExists(t *testing.T, name string, query func(string, ...any) (bool, error)) bool {
	t.Helper()
	ok, err := query("SELECT to_regclass($1) IS NOT NULL", name)
	if err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return ok
}

func TestUpDownRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.OpenPool(t, pgtest.NewDatabase(t))
	m, err := migrate.New(pool, fixture)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	exists := func(sql string, args ...any) (bool, error) {
		var ok bool
		err := pool.QueryRow(ctx, sql, args...).Scan(&ok)
		return ok, err
	}

	applied, err := m.Up(ctx)
	if err != nil || len(applied) != 2 {
		t.Fatalf("Up: applied=%v err=%v", applied, err)
	}
	if !tableExists(t, "widgets", exists) || !tableExists(t, "gadgets", exists) {
		t.Fatal("tables missing after up")
	}

	res, err := m.Down(ctx)
	if err != nil || res.Version != 2 {
		t.Fatalf("Down: res=%+v err=%v", res, err)
	}
	if tableExists(t, "gadgets", exists) || !tableExists(t, "widgets", exists) {
		t.Fatal("down should roll back exactly one step")
	}

	if applied, err = m.Up(ctx); err != nil || len(applied) != 1 {
		t.Fatalf("second Up: applied=%v err=%v", applied, err)
	}
}

func TestStatusListsAppliedVersions(t *testing.T) {
	ctx := context.Background()
	m, err := migrate.New(pgtest.OpenPool(t, pgtest.NewDatabase(t)), fixture)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	before, err := m.Status(ctx)
	if err != nil || len(before) != 2 || before[0].Applied || before[1].Applied {
		t.Fatalf("before up: %+v err=%v", before, err)
	}
	if _, err := m.Down(ctx); err == nil {
		t.Error("Down with nothing applied should fail")
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	after, err := m.Status(ctx)
	if err != nil || len(after) != 2 || !after[0].Applied || !after[1].Applied || after[0].AppliedAt.IsZero() {
		t.Fatalf("after up: %+v err=%v", after, err)
	}
}

func TestEmbeddedMigrationsRoundTrip(t *testing.T) {
	ctx := context.Background()
	m, err := migrate.New(pgtest.OpenPool(t, pgtest.NewDatabase(t)), migrations.FS)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if _, err := m.Down(ctx); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up again: %v", err)
	}
}
