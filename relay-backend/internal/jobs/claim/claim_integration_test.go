//go:build integration

package claim_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/queuesrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

const testLease = 30 * time.Second

var (
	createdBase = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	longAgo     = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	farFuture   = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
)

type jobSpec struct {
	handler     string
	queue       string
	priority    int
	createdAt   time.Time
	scheduledAt time.Time
	timeout     int
}

// defaultSpec is due now, in the default queue, at normal priority.
func defaultSpec(handler string) jobSpec {
	return jobSpec{handler: handler, queue: "default", priority: 50, createdAt: createdBase, scheduledAt: longAgo}
}

func insertJob(t *testing.T, pool *pgxpool.Pool, s jobSpec) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if err := queuesrepo.Ensure(ctx, pool, s.queue); err != nil {
		t.Fatalf("ensure queue: %v", err)
	}
	job := domain.Job{
		ID:             uuid.New(),
		Handler:        s.handler,
		Payload:        json.RawMessage(`{}`),
		Queue:          s.queue,
		Status:         domain.JobPending,
		Priority:       s.priority,
		ScheduledAt:    s.scheduledAt,
		MaxAttempts:    3,
		TimeoutSeconds: s.timeout,
		CreatedAt:      s.createdAt,
		UpdatedAt:      s.createdAt,
	}
	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return job.ID
}

func newClaimer(t *testing.T, pool *pgxpool.Pool) *claim.Claimer {
	t.Helper()
	c, err := claim.NewClaimer(pool, testLease)
	if err != nil {
		t.Fatalf("NewClaimer: %v", err)
	}
	return c
}

func mustClaim(t *testing.T, c *claim.Claimer, worker uuid.UUID, queues, handlers []string) *claim.ClaimedJob {
	t.Helper()
	got, err := c.Claim(context.Background(), worker, queues, handlers)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return got
}

var allQueues = []string{"default", "other"}

func TestClaimReturnsNilWhenNothingEligible(t *testing.T) {
	pool := pgtest.NewPool(t)
	got := mustClaim(t, newClaimer(t, pool), uuid.New(), allQueues, []string{"demo.echo"})
	if got != nil {
		t.Fatalf("Claim on empty table = %+v, want nil", got)
	}
}

func TestClaimNeverClaimsFutureScheduledJobs(t *testing.T) {
	pool := pgtest.NewPool(t)
	future := defaultSpec("demo.echo")
	future.scheduledAt = farFuture
	insertJob(t, pool, future)

	got := mustClaim(t, newClaimer(t, pool), uuid.New(), allQueues, []string{"demo.echo"})
	if got != nil {
		t.Fatalf("claimed future job early: %+v", got)
	}
}

func TestClaimSkipsCancelRequestedJobs(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := insertJob(t, pool, defaultSpec("demo.echo"))
	if _, err := pool.Exec(context.Background(),
		`UPDATE jobs SET cancel_requested_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("set cancel_requested_at: %v", err)
	}

	got := mustClaim(t, newClaimer(t, pool), uuid.New(), allQueues, []string{"demo.echo"})
	if got != nil {
		t.Fatalf("claimed cancel-requested job: %+v", got)
	}
}

func TestClaimPicksHigherPriorityThenOldestFirst(t *testing.T) {
	pool := pgtest.NewPool(t)
	low := defaultSpec("demo.echo")
	low.priority = 10
	low.createdAt = createdBase.Add(-time.Hour)
	lowID := insertJob(t, pool, low)

	highNew := defaultSpec("demo.echo")
	highNew.priority = 90
	highNew.createdAt = createdBase.Add(time.Minute)
	highNewID := insertJob(t, pool, highNew)

	highOld := defaultSpec("demo.echo")
	highOld.priority = 90
	highOld.createdAt = createdBase
	highOldID := insertJob(t, pool, highOld)

	c := newClaimer(t, pool)
	worker := uuid.New()
	var order []uuid.UUID
	for range 3 {
		got := mustClaim(t, c, worker, allQueues, []string{"demo.echo"})
		if got == nil {
			t.Fatal("Claim returned nil before all jobs were claimed")
		}
		order = append(order, got.Job.ID)
	}
	want := []uuid.UUID{highOldID, highNewID, lowID}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("claim order = %v, want %v", order, want)
		}
	}
}

func TestClaimFiltersByQueueAndHandler(t *testing.T) {
	pool := pgtest.NewPool(t)
	wrongQueue := defaultSpec("demo.echo")
	wrongQueue.queue = "other"
	wrongQueue.createdAt = createdBase.Add(-time.Hour)
	wrongQueueID := insertJob(t, pool, wrongQueue)

	wrongHandler := defaultSpec("demo.unknown")
	wrongHandler.createdAt = createdBase.Add(-time.Hour)
	wrongHandlerID := insertJob(t, pool, wrongHandler)

	matchID := insertJob(t, pool, defaultSpec("demo.echo"))

	c := newClaimer(t, pool)
	worker := uuid.New()
	got := mustClaim(t, c, worker, []string{"default"}, []string{"demo.echo"})
	if got == nil || got.Job.ID != matchID {
		t.Fatalf("claimed %+v, want job %s", got, matchID)
	}
	if next := mustClaim(t, c, worker, []string{"default"}, []string{"demo.echo"}); next != nil {
		t.Fatalf("claimed a second job from filtered set: %+v", next)
	}

	for _, id := range []uuid.UUID{wrongQueueID, wrongHandlerID} {
		var status string
		if err := pool.QueryRow(context.Background(),
			`SELECT status FROM jobs WHERE id = $1`, id).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status != "pending" {
			t.Fatalf("skipped job %s has status %q, want pending", id, status)
		}
	}
}

func TestClaimSetsOwnershipAttemptAndEvent(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	id := insertJob(t, pool, jobSpec{
		handler: "demo.echo", queue: "default", priority: 50,
		createdAt: createdBase, scheduledAt: longAgo, timeout: 60,
	})
	worker := uuid.New()

	got := mustClaim(t, newClaimer(t, pool), worker, allQueues, []string{"demo.echo"})
	if got == nil {
		t.Fatal("Claim returned nil, want a job")
	}
	if got.Job.ID != id || got.Job.Status != domain.JobRunning {
		t.Fatalf("claimed job = %s/%s, want %s/running", got.Job.ID, got.Job.Status, id)
	}
	if got.AttemptNumber != 1 || got.AttemptID == uuid.Nil || got.ClaimToken == uuid.Nil {
		t.Fatalf("unexpected attempt fields: %+v", got)
	}
	if got.Deadline == nil || !got.Deadline.Equal(got.AttemptStartedAt.Add(60*time.Second)) {
		t.Fatalf("deadline = %v, want attempt start + 60s", got.Deadline)
	}

	var (
		status, dbWorker, token        string
		attemptCount, cycleCount       int
		leaseExpires, startedAt, dbNow time.Time
	)
	err := pool.QueryRow(ctx, `
		SELECT status, worker_id::text, claim_token::text, attempt_count, cycle_attempt_count,
		       lease_expires_at, started_at, now()
		FROM jobs WHERE id = $1`, id).
		Scan(&status, &dbWorker, &token, &attemptCount, &cycleCount, &leaseExpires, &startedAt, &dbNow)
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	if status != "running" || dbWorker != worker.String() || token != got.ClaimToken.String() {
		t.Fatalf("ownership wrong: status=%s worker=%s token=%s", status, dbWorker, token)
	}
	if attemptCount != 1 || cycleCount != 1 {
		t.Fatalf("counters = %d/%d, want 1/1", attemptCount, cycleCount)
	}
	if startedAt.IsZero() || !leaseExpires.After(dbNow) {
		t.Fatalf("started_at=%v lease=%v now=%v: lease must be in the future", startedAt, leaseExpires, dbNow)
	}
	if !got.LeaseExpiresAt.Equal(leaseExpires) {
		t.Fatalf("ClaimedJob lease %v, db lease %v", got.LeaseExpiresAt, leaseExpires)
	}

	var (
		attemptWorker, attemptStatus string
		attemptNumber                int
	)
	err = pool.QueryRow(ctx, `
		SELECT attempt_number, worker_id::text, status FROM job_attempts WHERE id = $1`, got.AttemptID).
		Scan(&attemptNumber, &attemptWorker, &attemptStatus)
	if err != nil {
		t.Fatalf("read attempt: %v", err)
	}
	if attemptNumber != 1 || attemptWorker != worker.String() || attemptStatus != "running" {
		t.Fatalf("attempt row = %d/%s/%s, want 1/%s/running", attemptNumber, attemptWorker, attemptStatus, worker)
	}

	var (
		eventType    string
		eventAttempt *uuid.UUID
		eventCount   int
	)
	err = pool.QueryRow(ctx, `
		SELECT type, attempt_id FROM job_events WHERE job_id = $1 AND type = 'attempt.started'`, id).
		Scan(&eventType, &eventAttempt)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if eventAttempt == nil || *eventAttempt != got.AttemptID {
		t.Fatalf("attempt.started event attempt_id = %v, want %s", eventAttempt, got.AttemptID)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job_events WHERE job_id = $1`, id).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("events for job = %d, want 1 (attempt.started only; helper inserts without job.created)", eventCount)
	}
}

func TestClaimNoDeadlineWithoutTimeout(t *testing.T) {
	pool := pgtest.NewPool(t)
	insertJob(t, pool, defaultSpec("demo.echo"))
	got := mustClaim(t, newClaimer(t, pool), uuid.New(), allQueues, []string{"demo.echo"})
	if got == nil || got.Deadline != nil {
		t.Fatalf("claimed %+v, want a job with nil deadline", got)
	}
}

func TestReclaimKeepsFirstStartedAtAndCountsAttempts(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	id := insertJob(t, pool, defaultSpec("demo.echo"))
	c := newClaimer(t, pool)

	first := mustClaim(t, c, uuid.New(), allQueues, []string{"demo.echo"})
	if first == nil {
		t.Fatal("first claim returned nil")
	}
	var firstStarted time.Time
	if err := pool.QueryRow(ctx, `SELECT started_at FROM jobs WHERE id = $1`, id).Scan(&firstStarted); err != nil {
		t.Fatalf("read started_at: %v", err)
	}
	// Simulate a lost lease: the job goes back to pending for a retry.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET status = 'pending' WHERE id = $1`, id); err != nil {
		t.Fatalf("reset to pending: %v", err)
	}

	second := mustClaim(t, c, uuid.New(), allQueues, []string{"demo.echo"})
	if second == nil {
		t.Fatal("second claim returned nil")
	}
	if second.AttemptNumber != 2 || second.ClaimToken == first.ClaimToken {
		t.Fatalf("second claim = attempt %d, token reused=%v; want attempt 2 and a new token",
			second.AttemptNumber, second.ClaimToken == first.ClaimToken)
	}
	var (
		secondStarted    time.Time
		attempts, cycles int
	)
	if err := pool.QueryRow(ctx,
		`SELECT started_at, attempt_count, cycle_attempt_count FROM jobs WHERE id = $1`, id).
		Scan(&secondStarted, &attempts, &cycles); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if !secondStarted.Equal(firstStarted) {
		t.Fatalf("started_at changed on reclaim: %v -> %v", firstStarted, secondStarted)
	}
	if attempts != 2 || cycles != 2 {
		t.Fatalf("counters = %d/%d, want 2/2", attempts, cycles)
	}
}

func TestConcurrentClaimsClaimEachJobExactlyOnce(t *testing.T) {
	const (
		workers = 20
		jobs    = 50
	)
	pool := pgtest.NewPool(t)
	for range jobs {
		insertJob(t, pool, defaultSpec("demo.echo"))
	}
	c := newClaimer(t, pool)

	var (
		mu      sync.Mutex
		claimed = map[uuid.UUID]int{}
		wg      sync.WaitGroup
	)
	for range workers {
		wg.Add(1)
		go func(worker uuid.UUID) {
			defer wg.Done()
			for {
				got, err := c.Claim(context.Background(), worker, allQueues, []string{"demo.echo"})
				if err != nil {
					t.Errorf("concurrent Claim: %v", err)
					return
				}
				if got == nil {
					return
				}
				mu.Lock()
				claimed[got.Job.ID]++
				mu.Unlock()
			}
		}(uuid.New())
	}
	wg.Wait()

	if len(claimed) != jobs {
		t.Fatalf("claimed %d distinct jobs, want %d", len(claimed), jobs)
	}
	for id, n := range claimed {
		if n != 1 {
			t.Fatalf("job %s claimed %d times", id, n)
		}
	}
}
