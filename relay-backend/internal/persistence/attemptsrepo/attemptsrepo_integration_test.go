//go:build integration

package attemptsrepo_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/attemptsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func seedJob(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	job := domain.Job{ID: uuid.New(), Handler: "h", Payload: json.RawMessage(`{}`), Queue: "default",
		Status: domain.JobPending, Priority: 50, MaxAttempts: 3}
	if err := jobsrepo.Create(context.Background(), pool, job); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	return job.ID
}

func seedAttempt(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, number int, status string, started time.Time, finished *time.Time, errText *string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO job_attempts (id, job_id, attempt_number, worker_id, status, started_at, finished_at, error, result)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.New(), jobID, number, uuid.New(), status, started, finished, errText, []byte(`{"ok":true}`))
	if err != nil {
		t.Fatalf("seed attempt %d: %v", number, err)
	}
}

func numbers(as []domain.JobAttempt) []int {
	out := make([]int, len(as))
	for i, a := range as {
		out[i] = a.AttemptNumber
	}
	return out
}

func TestListByJob_OrdersByAttemptNumberAndIsolatesJobs(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID, other := seedJob(t, pool), seedJob(t, pool)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, n := range []int{3, 1, 2} {
		seedAttempt(t, pool, jobID, n, "failed", at, &at, nil)
	}
	seedAttempt(t, pool, other, 1, "failed", at, &at, nil)

	got, err := attemptsrepo.ListByJob(ctx, pool, jobID, 0, 10)
	if err != nil {
		t.Fatalf("ListByJob: %v", err)
	}
	if !reflect.DeepEqual(numbers(got), []int{1, 2, 3}) {
		t.Fatalf("order = %v", numbers(got))
	}
	for _, a := range got {
		if a.JobID != jobID {
			t.Errorf("attempt of another job: %+v", a)
		}
	}
}

func TestListByJob_AfterAndLimit(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID := seedJob(t, pool)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for n := 1; n <= 5; n++ {
		seedAttempt(t, pool, jobID, n, "failed", at, &at, nil)
	}

	got, err := attemptsrepo.ListByJob(ctx, pool, jobID, 2, 2)
	if err != nil {
		t.Fatalf("ListByJob: %v", err)
	}
	if !reflect.DeepEqual(numbers(got), []int{3, 4}) {
		t.Errorf("numbers = %v, want [3 4]", numbers(got))
	}
}

func TestListByJob_MapsAllFields(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID := seedJob(t, pool)
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := start.Add(1500 * time.Millisecond)
	msg := "boom"
	seedAttempt(t, pool, jobID, 1, "failed", start, &end, &msg)
	seedAttempt(t, pool, jobID, 2, "running", start, nil, nil)

	got, err := attemptsrepo.ListByJob(ctx, pool, jobID, 0, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("ListByJob = %v, %v", got, err)
	}
	a := got[0]
	if a.Status != domain.AttemptFailed || !a.StartedAt.Equal(start) || a.StartedAt.Location() != time.UTC ||
		a.FinishedAt == nil || !a.FinishedAt.Equal(end) || a.FinishedAt.Location() != time.UTC ||
		a.Error == nil || *a.Error != "boom" || a.WorkerID == uuid.Nil {
		t.Errorf("finished attempt = %+v", a)
	}
	if string(a.Result) != `{"ok": true}` {
		t.Errorf("result = %s", a.Result)
	}
	if r := got[1]; r.Status != domain.AttemptRunning || r.FinishedAt != nil || r.Error != nil {
		t.Errorf("running attempt = %+v", r)
	}
}

func TestListByJob_NoRowsReturnsEmpty(t *testing.T) {
	pool := pgtest.NewPool(t)
	got, err := attemptsrepo.ListByJob(context.Background(), pool, seedJob(t, pool), 0, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("ListByJob = %v, %v", got, err)
	}
}
