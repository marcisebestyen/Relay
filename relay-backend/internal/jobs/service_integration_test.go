//go:build integration

package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/queuesrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

var (
	fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	defaults = domain.Defaults{Queue: "default", Priority: 50, MaxAttempts: 3, TimeoutSeconds: 0}
)

func newService(pool *pgxpool.Pool) *jobs.Service {
	return jobs.NewService(pool, defaults, nil, func() time.Time { return fixedNow })
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	// table is a test constant, never user input.
	switch table {
	case "jobs", "job_events", "queues":
	default:
		t.Fatalf("unexpected table %q", table)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestCreateAndGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	prio, timeout := 10, 120
	created, err := svc.Create(ctx, domain.NewJobInput{
		Name: "report", Handler: "demo.echo", Queue: "reports",
		Payload: json.RawMessage(`{"z": 1, "a": [true]}`), Priority: &prio, TimeoutSeconds: &timeout,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != domain.JobPending || created.CreatedAt != fixedNow {
		t.Fatalf("unexpected job: %+v", created)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var gp, wp any
	if err := json.Unmarshal(got.Payload, &gp); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if err := json.Unmarshal(created.Payload, &wp); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if !reflect.DeepEqual(gp, wp) {
		t.Fatalf("payload = %s, want %s", got.Payload, created.Payload)
	}
	got.Payload = created.Payload
	if !reflect.DeepEqual(got, created) {
		t.Fatalf("got %+v, want %+v", got, created)
	}
}

func TestCreateEmptyPayloadStoresEmptyObject(t *testing.T) {
	ctx := context.Background()
	svc := newService(pgtest.NewPool(t))
	created, err := svc.Create(ctx, domain.NewJobInput{Handler: "demo.echo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil || string(got.Payload) != "{}" {
		t.Fatalf("payload = %s, err = %v, want {}", got.Payload, err)
	}
}

func TestCreateNewQueueCreatesQueueRow(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	if _, err := svc.Create(ctx, domain.NewJobInput{Handler: "h", Queue: "fresh"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM queues WHERE name = 'fresh'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("queue rows = %d, err = %v", n, err)
	}
}

func TestCreateOnExistingQueueSucceeds(t *testing.T) {
	svc := newService(pgtest.NewPool(t))
	if _, err := svc.Create(context.Background(), domain.NewJobInput{Handler: "h"}); err != nil {
		t.Fatalf("Create on default queue: %v", err)
	}
}

func TestConcurrentCreatesOnSameNewQueue(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	const workers = 20
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Create(ctx, domain.NewJobInput{Handler: "h", Queue: "burst"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Create: %v", err)
		}
	}
	var queues, jobsN int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM queues WHERE name = 'burst'`).Scan(&queues); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE queue = 'burst'`).Scan(&jobsN); err != nil {
		t.Fatal(err)
	}
	if queues != 1 || jobsN != workers {
		t.Fatalf("queues = %d, jobs = %d, want 1 and %d", queues, jobsN, workers)
	}
}

func TestCreateWritesJobCreatedEventWithoutPayload(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	job, err := svc.Create(ctx, domain.NewJobInput{Handler: "demo.echo", Queue: "q1", Payload: json.RawMessage(`{"secret":"s3"}`)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var typ string
	var data []byte
	if err := pool.QueryRow(ctx, `SELECT type, data FROM job_events WHERE job_id = $1`, job.ID).Scan(&typ, &data); err != nil {
		t.Fatalf("select event: %v", err)
	}
	if typ != "job.created" {
		t.Fatalf("type = %q", typ)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("data: %v", err)
	}
	want := map[string]any{"queue": "q1", "handler": "demo.echo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("data = %v, want %v", got, want)
	}
}

func TestCreateInvalidInputReturnsValidationErrorsAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	jobsBefore, queuesBefore := count(t, pool, "jobs"), count(t, pool, "queues")

	_, err := svc.Create(ctx, domain.NewJobInput{Handler: "BAD HANDLER", Queue: "newq", Payload: json.RawMessage(`{not json`)})
	var ve domain.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationErrors", err)
	}
	if _, ok := ve["handler"]; !ok {
		t.Fatalf("missing handler error: %v", ve)
	}
	if _, ok := ve["payload"]; !ok {
		t.Fatalf("missing payload error: %v", ve)
	}
	if count(t, pool, "jobs") != jobsBefore || count(t, pool, "queues") != queuesBefore || count(t, pool, "job_events") != 0 {
		t.Fatal("invalid input wrote rows")
	}
}

func TestCreateInvalidPayloadAloneIsReported(t *testing.T) {
	svc := newService(pgtest.NewPool(t))
	_, err := svc.Create(context.Background(), domain.NewJobInput{Handler: "h", Payload: json.RawMessage(`[1,`)})
	var ve domain.ValidationErrors
	if !errors.As(err, &ve) || len(ve) != 1 || ve["payload"] == "" {
		t.Fatalf("err = %v, want only a payload error", err)
	}
}

func TestGetNotFound(t *testing.T) {
	svc := newService(pgtest.NewPool(t))
	_, err := svc.Get(context.Background(), uuid.New())
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v, want db.ErrNotFound", err)
	}
}

// The service runs these three steps in one WithTx. A failing last step must
// leave neither the job nor the new queue behind.
func TestFailedEventInsertRollsBackJobAndQueue(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	job := domain.Job{ID: uuid.New(), Handler: "h", Payload: json.RawMessage(`{}`), Queue: "doomed",
		Status: domain.JobPending, Priority: 50, MaxAttempts: 3, CreatedAt: fixedNow, UpdatedAt: fixedNow, ScheduledAt: fixedNow}

	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := queuesrepo.Ensure(ctx, tx, job.Queue); err != nil {
			return err
		}
		if err := jobsrepo.Create(ctx, tx, job); err != nil {
			return err
		}
		return eventsrepo.Append(ctx, tx, eventsrepo.Event{JobID: job.ID, Type: eventsrepo.TypeJobCreated, Data: json.RawMessage(`{broken`)})
	})
	if err == nil {
		t.Fatal("want event insert error")
	}
	if _, err := jobsrepo.GetByID(ctx, pool, job.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("job still present: err = %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM queues WHERE name = 'doomed'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("queue rows = %d, err = %v, want 0", n, err)
	}
}

func TestCreateReturnsErrorWhenDatabaseFails(t *testing.T) {
	svc := newService(pgtest.NewPool(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Create(ctx, domain.NewJobInput{Handler: "h"}); err == nil {
		t.Fatal("want error from cancelled context")
	}
}

func TestListReturnsCreatedJobsWithoutPayload(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	svc := newService(pool)
	created, err := svc.Create(ctx, domain.NewJobInput{Handler: "demo.echo", Payload: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	page, err := svc.List(ctx, domain.JobFilter{Sort: domain.SortCreatedAtDesc, Limit: 10})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != created.ID || page.Items[0].Payload != nil || page.Next != nil {
		t.Fatalf("page = %+v", page)
	}
}

func TestListRejectsInvalidFilterWithoutQuerying(t *testing.T) {
	svc := jobs.NewService(nil, defaults, nil, nil)

	_, err := svc.List(context.Background(), domain.JobFilter{Sort: domain.SortCreatedAtDesc, Limit: 0})

	var ve domain.ValidationErrors
	if !errors.As(err, &ve) || ve["limit"] == "" {
		t.Fatalf("err = %v, want ValidationErrors for limit", err)
	}
}
