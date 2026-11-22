//go:build integration

package jobsrepo_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

var base = time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)

func newJob() domain.Job {
	return domain.Job{
		ID:          uuid.New(),
		Handler:     "demo.echo",
		Payload:     json.RawMessage(`{}`),
		Queue:       "default",
		Status:      domain.JobPending,
		Priority:    50,
		ScheduledAt: base.Add(time.Hour),
		MaxAttempts: 3,
		CreatedAt:   base,
		UpdatedAt:   base.Add(time.Minute),
	}
}

func sameJSON(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got %s: %v", got, err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("unmarshal want %s: %v", want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestCreateGetRoundTripAllFields(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	done := base.Add(2 * time.Hour)
	lastErr := "boom"
	job := newJob()
	job.Name = "nightly report"
	job.Payload = json.RawMessage(`{"b": [1, 2, {"c": null}],   "a": "x"}`)
	job.Status = domain.JobFailed
	job.Priority = 7
	job.TimeoutSeconds = 90
	job.CompletedAt = &done
	job.LastError = &lastErr

	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := jobsrepo.GetByID(ctx, pool, job.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	sameJSON(t, got.Payload, job.Payload)
	got.Payload = job.Payload
	if !reflect.DeepEqual(got, job) {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, job)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Fatalf("created_at location = %v, want UTC", got.CreatedAt.Location())
	}
}

func TestCreateGetRoundTripOptionalFieldsUnset(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	job := newJob()
	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := jobsrepo.GetByID(ctx, pool, job.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "" || got.TimeoutSeconds != 0 || got.CompletedAt != nil || got.LastError != nil {
		t.Fatalf("unset fields not zero: %+v", got)
	}

	var nameNull, timeoutNull bool
	err = pool.QueryRow(ctx, `SELECT name IS NULL, timeout_seconds IS NULL FROM jobs WHERE id = $1`, job.ID).Scan(&nameNull, &timeoutNull)
	if err != nil || !nameNull || !timeoutNull {
		t.Fatalf("name null = %v, timeout null = %v, err = %v; want both NULL", nameNull, timeoutNull, err)
	}
}

func TestCreateLeavesCountersAtZero(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	job := newJob()
	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var attempts, cycle int
	err := pool.QueryRow(ctx, `SELECT attempt_count, cycle_attempt_count FROM jobs WHERE id = $1`, job.ID).Scan(&attempts, &cycle)
	if err != nil || attempts != 0 || cycle != 0 {
		t.Fatalf("attempts = %d, cycle = %d, err = %v", attempts, cycle, err)
	}
}

func TestGetByIDNotFound(t *testing.T) {
	pool := pgtest.NewPool(t)
	_, err := jobsrepo.GetByID(context.Background(), pool, uuid.New())
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want db.ErrNotFound", err)
	}
}

func TestCreateUnknownQueueFails(t *testing.T) {
	pool := pgtest.NewPool(t)
	job := newJob()
	job.Queue = "missing"
	if err := jobsrepo.Create(context.Background(), pool, job); err == nil {
		t.Fatal("want foreign key error")
	}
}

func TestCreateDuplicateIDFails(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	job := newJob()
	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := jobsrepo.Create(ctx, pool, job); err == nil {
		t.Fatal("want duplicate key error")
	}
}
