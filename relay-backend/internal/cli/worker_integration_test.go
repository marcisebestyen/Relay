//go:build integration

package cli_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/cli"
	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func TestWorkerProcessesJobsAndStopsOnCancel(t *testing.T) {
	url, pool := setupDatabase(t)
	cfg := loadConfig(t, url, config.RoleWorker)
	cfg.Worker.Concurrency = 2
	cfg.Worker.PollInterval = 5 * time.Millisecond

	reg := relay.NewRegistry()
	if err := reg.Register("demo.echo", relay.HandlerFunc(func(_ context.Context, p json.RawMessage) (json.RawMessage, error) {
		return p, nil
	})); err != nil {
		t.Fatalf("register: %v", err)
	}

	svc := jobs.NewService(pool, domain.Defaults{Queue: "default", Priority: 50, MaxAttempts: 3}, nil, nil)
	job, err := svc.Create(context.Background(), domain.NewJobInput{Handler: "demo.echo", Queue: "default"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- cli.RunWorker(ctx, cfg, reg, newTestLogger(), "test") }()

	waitForJobStatus(t, pool, job.ID, domain.JobCompleted)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWorker: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not exit after shutdown")
	}
}

func TestWorkerRefusesWithoutHandlersBeforeTouchingDatabase(t *testing.T) {
	url := "postgres://nobody:secret@127.0.0.1:1/none?sslmode=disable"
	cfg := loadConfig(t, url, config.RoleWorker)
	err := cli.RunWorker(context.Background(), cfg, relay.NewRegistry(), newTestLogger(), "test")
	if err == nil || err.Error() != "no handlers registered: register at least one handler before starting a worker" {
		t.Fatalf("want no-handlers error without a DB connection, got %v", err)
	}
}
