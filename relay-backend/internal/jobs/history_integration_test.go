//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestListAttempts_UnknownJobIsNotFound(t *testing.T) {
	svc := newService(pgtest.NewPool(t))
	_, err := svc.ListAttempts(context.Background(), uuid.New(), 0, 10)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want db.ErrNotFound", err)
	}
}

func TestListEvents_UnknownJobIsNotFound(t *testing.T) {
	svc := newService(pgtest.NewPool(t))
	_, err := svc.ListEvents(context.Background(), uuid.New(), 0, 10)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want db.ErrNotFound", err)
	}
}

func TestListAttempts_KnownJobWithoutAttemptsIsEmptyNotError(t *testing.T) {
	ctx := context.Background()
	svc := newService(pgtest.NewPool(t))
	job, err := svc.Create(ctx, domain.NewJobInput{Handler: "demo.echo"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListAttempts(ctx, job.ID, 0, 10)
	if err != nil || len(page.Items) != 0 || page.HasMore {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestListAttempts_ReportsHasMoreOnlyWhenRowsRemain(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	job, err := svc.Create(ctx, domain.NewJobInput{Handler: "demo.echo"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for n := 1; n <= 3; n++ {
		_, err := pool.Exec(ctx, `INSERT INTO job_attempts (id, job_id, attempt_number, worker_id, status, started_at)
			VALUES ($1, $2, $3, $4, 'failed', $5)`, uuid.New(), job.ID, n, uuid.New(), at)
		if err != nil {
			t.Fatal(err)
		}
	}

	first, err := svc.ListAttempts(ctx, job.ID, 0, 2)
	if err != nil || len(first.Items) != 2 || !first.HasMore {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
	last, err := svc.ListAttempts(ctx, job.ID, 2, 2)
	if err != nil || len(last.Items) != 1 || last.HasMore || last.Items[0].AttemptNumber != 3 {
		t.Fatalf("last = %+v, err = %v", last, err)
	}
	exact, err := svc.ListAttempts(ctx, job.ID, 1, 2)
	if err != nil || len(exact.Items) != 2 || exact.HasMore {
		t.Fatalf("exact = %+v, err = %v", exact, err)
	}
}

func TestListEvents_ReturnsCreatedEventAndPages(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	job, err := svc.Create(ctx, domain.NewJobInput{Handler: "demo.echo"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO job_events (job_id, type) VALUES ($1, 'attempt.started')`, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.ListEvents(ctx, job.ID, 0, 1)
	if err != nil || len(first.Items) != 1 || !first.HasMore || first.Items[0].Type != "job.created" {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
	second, err := svc.ListEvents(ctx, job.ID, first.Items[0].ID, 1)
	if err != nil || len(second.Items) != 1 || second.HasMore || second.Items[0].Type != "attempt.started" {
		t.Fatalf("second = %+v, err = %v", second, err)
	}
}
