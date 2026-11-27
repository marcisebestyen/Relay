//go:build integration

package cli_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/cli"
	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/migrate"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
	"github.com/marcisebestyen/relay/relay-backend/migrations"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func TestStartServesAPIAndProcessesJobsInOneProcess(t *testing.T) {
	url, pool := setupDatabase(t)
	cfg := loadConfig(t, url, config.RoleCombined)

	reg := relay.NewRegistry()
	if err := reg.Register("demo.echo", relay.HandlerFunc(func(_ context.Context, p json.RawMessage) (json.RawMessage, error) {
		return p, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- cli.RunStart(ctx, cfg, reg, newTestLogger(), "test", l) }()

	svc := jobs.NewService(pool, domain.Defaults{Queue: "default", Priority: 50, MaxAttempts: 3}, nil, nil)
	job, err := svc.Create(context.Background(), domain.NewJobInput{Handler: "demo.echo", Queue: "default"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	waitForJobStatus(t, pool, job.ID, domain.JobCompleted)

	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runStart: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("start did not exit after shutdown")
	}
}

// setupDatabase creates a migrated database and returns its URL and a pool on it.
func setupDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	url := pgtest.NewDatabase(t)
	pool := pgtest.OpenPool(t, url)
	m, err := migrate.New(pool, migrations.FS)
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}
	if _, err := m.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return url, pool
}

func loadConfig(t *testing.T, url string, role config.Role) config.Config {
	t.Helper()
	cfg, err := config.Load(func(k string) (string, bool) {
		if k == "RELAY_DATABASE_URL" {
			return url, true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := cfg.Validate(role); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return cfg
}

// waitForJobStatus polls until job id has status want or the test fails.
func waitForJobStatus(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, want domain.JobStatus) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var status string
		if err := pool.QueryRow(context.Background(), "SELECT status FROM jobs WHERE id = $1", id).Scan(&status); err != nil {
			t.Fatalf("read job: %v", err)
		}
		if status == string(want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job still %q, want %q", status, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
