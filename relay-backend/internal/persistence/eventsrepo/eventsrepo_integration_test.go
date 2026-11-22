//go:build integration

package eventsrepo_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func TestAppendStoresEvent(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	job := domain.Job{ID: uuid.New(), Handler: "h", Payload: json.RawMessage(`{}`), Queue: "default",
		Status: domain.JobPending, Priority: 50, MaxAttempts: 3}
	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	attempt := uuid.New()
	err := eventsrepo.Append(ctx, pool, eventsrepo.Event{
		JobID: job.ID, AttemptID: &attempt, Type: "custom", Data: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	var (
		typ     string
		gotAtt  *uuid.UUID
		data    []byte
		hasTime bool
	)
	err = pool.QueryRow(ctx, `SELECT type, attempt_id, data, created_at IS NOT NULL FROM job_events WHERE job_id = $1`, job.ID).
		Scan(&typ, &gotAtt, &data, &hasTime)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if typ != "custom" || gotAtt == nil || *gotAtt != attempt || string(data) != `{"a": 1}` || !hasTime {
		t.Fatalf("got type=%q attempt=%v data=%s time=%v", typ, gotAtt, data, hasTime)
	}
}

func TestAppendWithoutDataStoresNull(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	job := domain.Job{ID: uuid.New(), Handler: "h", Payload: json.RawMessage(`{}`), Queue: "default",
		Status: domain.JobPending, Priority: 50, MaxAttempts: 3}
	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	if err := eventsrepo.Append(ctx, pool, eventsrepo.Event{JobID: job.ID, Type: "bare"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	var isNull bool
	if err := pool.QueryRow(ctx, `SELECT data IS NULL FROM job_events WHERE job_id = $1`, job.ID).Scan(&isNull); err != nil || !isNull {
		t.Fatalf("data null = %v, err = %v", isNull, err)
	}
}

func TestAppendUnknownJobFails(t *testing.T) {
	pool := pgtest.NewPool(t)
	err := eventsrepo.Append(context.Background(), pool, eventsrepo.Event{JobID: uuid.New(), Type: "x"})
	if err == nil {
		t.Fatal("want foreign key error")
	}
}
