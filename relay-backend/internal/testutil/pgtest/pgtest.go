// Package pgtest gives integration tests an isolated Postgres database.
//
// Call Main from TestMain: it starts one container for the test package.
// Each test then calls NewPool or NewDatabase to get its own database.
package pgtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/migrate"
	"github.com/marcisebestyen/relay/relay-backend/migrations"
)

const (
	image          = "postgres:17"
	adminDatabase  = "postgres"
	startupTimeout = 2 * time.Minute
	cleanupTimeout = 30 * time.Second
)

var (
	adminURL string
	counter  atomic.Int64
)

// Main starts the shared container, runs the tests and removes the container.
// Use it as: func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }.
func Main(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	ctr, err := postgres.Run(ctx, image,
		postgres.WithDatabase(adminDatabase),
		postgres.WithUsername("relay"),
		postgres.WithPassword("relay"),
		postgres.BasicWaitStrategies(),
	)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgtest: start postgres container (is Docker running?):", err)
		return 1
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := ctr.Terminate(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "pgtest: terminate container:", err)
		}
	}()

	ctx, cancel = context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	if adminURL, err = ctr.ConnectionString(ctx, "sslmode=disable"); err != nil {
		fmt.Fprintln(os.Stderr, "pgtest: read connection string:", err)
		return 1
	}
	return m.Run()
}

// NewDatabase creates an empty database for t and returns its URL.
// The database is dropped when the test ends.
func NewDatabase(t *testing.T) string {
	t.Helper()
	if adminURL == "" {
		t.Fatal("pgtest: call pgtest.Main from TestMain first")
	}
	ctx := context.Background()
	name := fmt.Sprintf("relay_test_%d", counter.Add(1))

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("pgtest: connect admin: %v", err)
	}
	defer admin.Close(ctx)

	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		conn, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			t.Errorf("pgtest: connect admin for cleanup: %v", err)
			return
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("pgtest: drop database: %v", err)
		}
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("pgtest: parse admin url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// NewPool creates an isolated database, applies every migration and returns a
// pool for it. The pool is closed and the database dropped when the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := OpenPool(t, NewDatabase(t))
	m, err := migrate.New(pool, migrations.FS)
	if err != nil {
		t.Fatalf("pgtest: create migrator: %v", err)
	}
	if _, err := m.Up(context.Background()); err != nil {
		t.Fatalf("pgtest: migrate up: %v", err)
	}
	return pool
}

// OpenPool opens a pool for url that is closed when the test ends.
func OpenPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := db.Open(context.Background(), config.Database{URL: url})
	if err != nil {
		t.Fatalf("pgtest: open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
