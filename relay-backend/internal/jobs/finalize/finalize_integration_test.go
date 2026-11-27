//go:build integration

package finalize_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/finalize"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/attemptsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/queuesrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

const (
	testLease     = 30 * time.Second
	testMaxResult = 64 << 10
	handler       = "demo.echo"
)

var allQueues = []string{"default"}

func insertPendingJob(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if err := queuesrepo.Ensure(ctx, pool, "default"); err != nil {
		t.Fatalf("ensure queue: %v", err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	job := domain.Job{
		ID:          uuid.New(),
		Handler:     handler,
		Payload:     json.RawMessage(`{"secret":"do-not-leak"}`),
		Queue:       "default",
		Status:      domain.JobPending,
		Priority:    50,
		ScheduledAt: now.Add(-time.Hour),
		MaxAttempts: 3,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := jobsrepo.Create(ctx, pool, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return job.ID
}

func claimJob(t *testing.T, pool *pgxpool.Pool) *claim.ClaimedJob {
	t.Helper()
	c, err := claim.NewClaimer(pool, testLease)
	if err != nil {
		t.Fatalf("NewClaimer: %v", err)
	}
	got, err := c.Claim(context.Background(), uuid.New(), allQueues, []string{handler})
	if err != nil || got == nil {
		t.Fatalf("Claim = (%v, %v), want a job", got, err)
	}
	return got
}

func newFinalizer(t *testing.T, pool *pgxpool.Pool) *finalize.Finalizer {
	t.Helper()
	f, err := finalize.New(pool, testMaxResult)
	if err != nil {
		t.Fatalf("New finalizer: %v", err)
	}
	return f
}

// ownership reads the fencing columns, which domain.Job does not expose.
func ownership(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) (workerID, claimToken *uuid.UUID, lease *time.Time) {
	t.Helper()
	err := pool.QueryRow(context.Background(),
		`SELECT worker_id, claim_token, lease_expires_at FROM jobs WHERE id = $1`, id).
		Scan(&workerID, &claimToken, &lease)
	if err != nil {
		t.Fatalf("read ownership: %v", err)
	}
	return workerID, claimToken, lease
}

func errorKind(t *testing.T, pool *pgxpool.Pool, attemptID uuid.UUID) *string {
	t.Helper()
	var kind *string
	err := pool.QueryRow(context.Background(),
		`SELECT error_kind FROM job_attempts WHERE id = $1`, attemptID).Scan(&kind)
	if err != nil {
		t.Fatalf("read error_kind: %v", err)
	}
	return kind
}

func eventTypes(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) []string {
	t.Helper()
	events, err := eventsrepo.ListByJob(context.Background(), pool, jobID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.Type)
	}
	return types
}

func attemptOf(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) domain.JobAttempt {
	t.Helper()
	attempts, err := attemptsrepo.ListByJob(context.Background(), pool, jobID, 0, 10)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("ListByJob = (%d attempts, %v), want 1", len(attempts), err)
	}
	return attempts[0]
}

func TestComplete_StoresResultAndClearsOwnership(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := insertPendingJob(t, pool)
	cj := claimJob(t, pool)

	if err := newFinalizer(t, pool).Complete(context.Background(), *cj, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	job, err := jobsrepo.GetByID(context.Background(), pool, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if job.Status != domain.JobCompleted || job.CompletedAt == nil {
		t.Fatalf("job = status %s completed_at %v, want completed with timestamp", job.Status, job.CompletedAt)
	}
	if w, tok, lease := ownership(t, pool, id); w != nil || tok != nil || lease != nil {
		t.Fatalf("ownership not cleared: worker=%v token=%v lease=%v", w, tok, lease)
	}

	attempt := attemptOf(t, pool, id)
	if attempt.Status != domain.AttemptCompleted || attempt.FinishedAt == nil {
		t.Fatalf("attempt = status %s finished_at %v, want completed with timestamp", attempt.Status, attempt.FinishedAt)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(attempt.Result, &result); err != nil || !result.OK {
		t.Fatalf("attempt result = %s (%v), want stored {\"ok\":true}", attempt.Result, err)
	}

	got := eventTypes(t, pool, id)
	// job.created comes from the jobs service, not the bare repo insert used here.
	want := []string{eventsrepo.TypeAttemptStarted,
		eventsrepo.TypeAttemptCompleted, eventsrepo.TypeJobStatusChanged}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestFail_StoresTruncatedErrorKindAndClearsOwnership(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := insertPendingJob(t, pool)
	cj := claimJob(t, pool)

	longErr := errors.New(strings.Repeat("e", 10000))
	if err := newFinalizer(t, pool).Fail(context.Background(), *cj, longErr, finalize.KindHandlerError); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	job, err := jobsrepo.GetByID(context.Background(), pool, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if job.Status != domain.JobFailed {
		t.Fatalf("job status = %s, want failed", job.Status)
	}
	if job.LastError == nil || len(*job.LastError) != finalize.MaxErrorBytes {
		t.Fatalf("last_error len = %v, want %d", job.LastError, finalize.MaxErrorBytes)
	}
	if w, tok, lease := ownership(t, pool, id); w != nil || tok != nil || lease != nil {
		t.Fatalf("ownership not cleared: worker=%v token=%v lease=%v", w, tok, lease)
	}

	attempt := attemptOf(t, pool, id)
	if attempt.Status != domain.AttemptFailed || attempt.Error == nil || len(*attempt.Error) != finalize.MaxErrorBytes {
		t.Fatalf("attempt = status %s error %v, want failed with truncated error", attempt.Status, attempt.Error)
	}
	if kind := errorKind(t, pool, attempt.ID); kind == nil || *kind != "handler_error" {
		t.Fatalf("error_kind = %v, want handler_error", kind)
	}
	if strings.Contains(*job.LastError, "do-not-leak") {
		t.Fatal("stored error contains payload")
	}
}

func TestFinalize_StaleClaimToken_ChangesNothingAndReturnsOwnershipLost(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := insertPendingJob(t, pool)
	stale := claimJob(t, pool)

	// Simulate lease recovery: the job goes back to pending and is claimed again.
	if _, err := pool.Exec(context.Background(),
		`UPDATE jobs SET status = 'pending' WHERE id = $1`, id); err != nil {
		t.Fatalf("reset job: %v", err)
	}
	fresh := claimJob(t, pool)
	f := newFinalizer(t, pool)

	if err := f.Complete(context.Background(), *stale, json.RawMessage(`{}`)); !errors.Is(err, finalize.ErrOwnershipLost) {
		t.Fatalf("stale Complete err = %v, want ErrOwnershipLost", err)
	}
	if err := f.Fail(context.Background(), *stale, errors.New("boom"), finalize.KindHandlerError); !errors.Is(err, finalize.ErrOwnershipLost) {
		t.Fatalf("stale Fail err = %v, want ErrOwnershipLost", err)
	}

	job, err := jobsrepo.GetByID(context.Background(), pool, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if job.Status != domain.JobRunning || job.LastError != nil {
		t.Fatalf("job = status %s last_error %v, want unchanged running", job.Status, job.LastError)
	}
	_, tok, _ := ownership(t, pool, id)
	if tok == nil || *tok != fresh.ClaimToken {
		t.Fatalf("claim token = %v, want fresh token %s", tok, fresh.ClaimToken)
	}
	attempts, err := attemptsrepo.ListByJob(context.Background(), pool, id, 0, 10)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("ListByJob = (%d attempts, %v), want 2", len(attempts), err)
	}
	if attempts[0].Status != domain.AttemptRunning || attempts[0].Error != nil {
		t.Fatalf("stale attempt = status %s error %v, want running and untouched", attempts[0].Status, attempts[0].Error)
	}
}

func TestComplete_OversizedResult_DropsResultWithNote(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := insertPendingJob(t, pool)
	cj := claimJob(t, pool)
	f, err := finalize.New(pool, 16)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	big := json.RawMessage(`{"data":"` + strings.Repeat("x", 100) + `"}`)
	if err := f.Complete(context.Background(), *cj, big); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	job, err := jobsrepo.GetByID(context.Background(), pool, id)
	if err != nil || job.Status != domain.JobCompleted {
		t.Fatalf("job = (%s, %v), want completed", job.Status, err)
	}
	attempt := attemptOf(t, pool, id)
	if attempt.Result != nil {
		t.Fatalf("result = %s, want dropped", attempt.Result)
	}
	if attempt.Error == nil || !strings.Contains(*attempt.Error, "result dropped") {
		t.Fatalf("attempt error = %v, want result dropped note", attempt.Error)
	}
}

func TestComplete_InjectedAttemptFailure_RollsBackJobUpdate(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := insertPendingJob(t, pool)
	cj := claimJob(t, pool)
	ctx := context.Background()

	// Test-only trigger: the attempt update fails after the job update ran.
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_attempt_update() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected failure'; END $$;
		CREATE TRIGGER fail_attempt BEFORE UPDATE ON job_attempts
		FOR EACH ROW EXECUTE FUNCTION fail_attempt_update();`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	if err := newFinalizer(t, pool).Complete(ctx, *cj, json.RawMessage(`{}`)); err == nil {
		t.Fatal("Complete returned nil despite injected failure")
	}

	job, err := jobsrepo.GetByID(ctx, pool, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if job.Status != domain.JobRunning {
		t.Fatalf("job status = %s, want running (rolled back)", job.Status)
	}
	if _, tok, _ := ownership(t, pool, id); tok == nil || *tok != cj.ClaimToken {
		t.Fatalf("ownership changed after rollback: token=%v", tok)
	}
	for _, typ := range eventTypes(t, pool, id) {
		if typ == eventsrepo.TypeAttemptCompleted || typ == eventsrepo.TypeJobStatusChanged {
			t.Fatalf("event %s written despite rollback", typ)
		}
	}
}
