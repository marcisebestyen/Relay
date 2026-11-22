//go:build integration

package eventsrepo_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func seedJob(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	job := domain.Job{ID: uuid.New(), Handler: "h", Payload: json.RawMessage(`{}`), Queue: "default",
		Status: domain.JobPending, Priority: 50, MaxAttempts: 3}
	if err := jobsrepo.Create(context.Background(), pool, job); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	return job.ID
}

func seedEvent(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, typ string, createdAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO job_events (job_id, type, data, created_at) VALUES ($1, $2, '{"k":"v"}', $3)`,
		jobID, typ, createdAt)
	if err != nil {
		t.Fatalf("seed event %s: %v", typ, err)
	}
}

func types(es []eventsrepo.Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Type
	}
	return out
}

func TestListByJob_OrdersByCreatedAtThenIDAndIsolatesJobs(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID, other := seedJob(t, pool), seedJob(t, pool)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	// Inserted out of time order; the two tie rows share a timestamp.
	seedEvent(t, pool, jobID, "late", base.Add(time.Minute))
	seedEvent(t, pool, jobID, "early", base)
	seedEvent(t, pool, jobID, "tie-first", base.Add(30*time.Second))
	seedEvent(t, pool, jobID, "tie-second", base.Add(30*time.Second))
	seedEvent(t, pool, other, "foreign", base)

	got, err := eventsrepo.ListByJob(ctx, pool, jobID, 0, 10)
	if err != nil {
		t.Fatalf("ListByJob: %v", err)
	}
	want := []string{"early", "tie-first", "tie-second", "late"}
	if !reflect.DeepEqual(types(got), want) {
		t.Fatalf("order = %v, want %v", types(got), want)
	}
}

func TestListByJob_AfterIDContinuesInTimeOrder(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID := seedJob(t, pool)
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seedEvent(t, pool, jobID, "third", base.Add(2*time.Minute))
	seedEvent(t, pool, jobID, "first", base)
	seedEvent(t, pool, jobID, "second", base.Add(time.Minute))

	page1, err := eventsrepo.ListByJob(ctx, pool, jobID, 0, 2)
	if err != nil || !reflect.DeepEqual(types(page1), []string{"first", "second"}) {
		t.Fatalf("page 1 = %v, %v", types(page1), err)
	}
	page2, err := eventsrepo.ListByJob(ctx, pool, jobID, page1[1].ID, 2)
	if err != nil || !reflect.DeepEqual(types(page2), []string{"third"}) {
		t.Fatalf("page 2 = %v, %v", types(page2), err)
	}
}

func TestListByJob_UnknownCursorReturnsNothing(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID := seedJob(t, pool)
	seedEvent(t, pool, jobID, "a", time.Now())

	got, err := eventsrepo.ListByJob(ctx, pool, jobID, 999999, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("ListByJob = %v, %v", got, err)
	}
}

func TestListByJob_MapsFields(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID := seedJob(t, pool)
	attempt := uuid.New()
	if err := eventsrepo.Append(ctx, pool, eventsrepo.Event{JobID: jobID, AttemptID: &attempt, Type: eventsrepo.TypeJobCreated,
		Data: json.RawMessage(`{"queue":"default"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := eventsrepo.Append(ctx, pool, eventsrepo.Event{JobID: jobID, Type: "no-data"}); err != nil {
		t.Fatal(err)
	}

	got, err := eventsrepo.ListByJob(ctx, pool, jobID, 0, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("ListByJob = %v, %v", got, err)
	}
	e := got[0]
	if e.ID == 0 || e.JobID != jobID || e.AttemptID == nil || *e.AttemptID != attempt ||
		e.CreatedAt.IsZero() || e.CreatedAt.Location() != time.UTC || string(e.Data) != `{"queue": "default"}` {
		t.Errorf("event = %+v", e)
	}
	if got[1].AttemptID != nil || len(got[1].Data) != 0 {
		t.Errorf("event without data = %+v", got[1])
	}
}
