//go:build integration

package migrate_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/migrate"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
	"github.com/marcisebestyen/relay/relay-backend/migrations"
)

const (
	pgCheckViolation      = "23514"
	pgForeignKeyViolation = "23503"
	pgUniqueViolation     = "23505"
)

var coreTables = []string{"queues", "jobs", "job_attempts", "workers", "job_events"}

// newID makes a random uuid string; the schema has no id default on purpose.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func tablePresent(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&ok); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return ok
}

func requireSQLState(t *testing.T, err error, want string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("want postgres error %s, got %v", want, err)
	}
	if pgErr.Code != want {
		t.Fatalf("want sqlstate %s, got %s (%s)", want, pgErr.Code, pgErr.Message)
	}
}

func insertJob(t *testing.T, pool *pgxpool.Pool, status string, priority int) (string, error) {
	t.Helper()
	id := newID()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO jobs (id, handler, payload, status, priority, max_attempts)
		 VALUES ($1, 'h', '{}', $2, $3, 3)`, id, status, priority)
	return id, err
}

func mustInsertJob(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id, err := insertJob(t, pool, "pending", 50)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return id
}

func insertAttempt(pool *pgxpool.Pool, jobID string, number int, status string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO job_attempts (id, job_id, attempt_number, worker_id, status)
		 VALUES ($1, $2, $3, $4, $5)`, newID(), jobID, number, newID(), status)
	return err
}

func count(t *testing.T, pool *pgxpool.Pool, table string, where string, args ...any) int {
	t.Helper()
	var n int
	q := "SELECT count(*) FROM " + table + " WHERE " + where
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestCoreSchemaRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.OpenPool(t, pgtest.NewDatabase(t))
	m, err := migrate.New(pool, migrations.FS)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	for _, name := range coreTables {
		if !tablePresent(t, pool, name) {
			t.Errorf("table %s missing after up", name)
		}
	}

	if _, err := m.Down(ctx); err != nil {
		t.Fatalf("Down: %v", err)
	}
	for _, name := range coreTables {
		if tablePresent(t, pool, name) {
			t.Errorf("table %s still present after down", name)
		}
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up again: %v", err)
	}
	for _, name := range coreTables {
		if !tablePresent(t, pool, name) {
			t.Errorf("table %s missing after second up", name)
		}
	}
}

func TestDefaultQueueIsSeeded(t *testing.T) {
	pool := pgtest.NewPool(t)
	if n := count(t, pool, "queues", "name = 'default'"); n != 1 {
		t.Fatalf("want 1 default queue, got %d", n)
	}
}

func TestJobStatusCheck(t *testing.T) {
	pool := pgtest.NewPool(t)
	for _, status := range []string{"pending", "running", "completed", "failed", "cancelled"} {
		if _, err := insertJob(t, pool, status, 50); err != nil {
			t.Errorf("status %q should be accepted: %v", status, err)
		}
	}
	_, err := insertJob(t, pool, "bogus", 50)
	requireSQLState(t, err, pgCheckViolation)
}

func TestJobPriorityCheck(t *testing.T) {
	pool := pgtest.NewPool(t)
	for _, p := range []int{0, 100} {
		if _, err := insertJob(t, pool, "pending", p); err != nil {
			t.Errorf("priority %d should be accepted: %v", p, err)
		}
	}
	for _, p := range []int{-1, 101} {
		_, err := insertJob(t, pool, "pending", p)
		requireSQLState(t, err, pgCheckViolation)
	}
}

func TestJobDefaults(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := newID()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO jobs (id, handler, payload, max_attempts) VALUES ($1, 'h', '{}', 3)`, id)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	var priority int
	var queue, status string
	var attempts, cycle int
	err = pool.QueryRow(context.Background(),
		`SELECT priority, queue, status, attempt_count, cycle_attempt_count FROM jobs WHERE id = $1`, id,
	).Scan(&priority, &queue, &status, &attempts, &cycle)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if priority != 50 || queue != "default" || status != "pending" || attempts != 0 || cycle != 0 {
		t.Fatalf("defaults wrong: priority=%d queue=%s status=%s attempts=%d cycle=%d", priority, queue, status, attempts, cycle)
	}
}

func TestJobNumericChecks(t *testing.T) {
	pool := pgtest.NewPool(t)
	bad := []string{
		"max_attempts = 0",
		"max_attempts = 101",
		"attempt_count = -1",
		"cycle_attempt_count = -1",
		"timeout_seconds = -1",
	}
	for _, set := range bad {
		id := mustInsertJob(t, pool)
		_, err := pool.Exec(context.Background(), "UPDATE jobs SET "+set+" WHERE id = $1", id)
		requireSQLState(t, err, pgCheckViolation)
	}
	id := mustInsertJob(t, pool)
	if _, err := pool.Exec(context.Background(), "UPDATE jobs SET timeout_seconds = 0 WHERE id = $1", id); err != nil {
		t.Errorf("timeout_seconds 0 should be accepted: %v", err)
	}
}

func TestAttemptStatusCheck(t *testing.T) {
	pool := pgtest.NewPool(t)
	jobID := mustInsertJob(t, pool)
	for i, status := range []string{"running", "completed", "failed", "cancelled", "timed_out", "lost"} {
		if err := insertAttempt(pool, jobID, i+1, status); err != nil {
			t.Errorf("attempt status %q should be accepted: %v", status, err)
		}
	}
	requireSQLState(t, insertAttempt(pool, jobID, 99, "bogus"), pgCheckViolation)
}

func TestAttemptNumberIsUniquePerJob(t *testing.T) {
	pool := pgtest.NewPool(t)
	jobA, jobB := mustInsertJob(t, pool), mustInsertJob(t, pool)
	if err := insertAttempt(pool, jobA, 1, "running"); err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	requireSQLState(t, insertAttempt(pool, jobA, 1, "failed"), pgUniqueViolation)
	if err := insertAttempt(pool, jobB, 1, "running"); err != nil {
		t.Errorf("same number on another job should be accepted: %v", err)
	}
}

func TestForeignKeys(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)

	_, err := pool.Exec(ctx,
		`INSERT INTO jobs (id, handler, payload, queue, max_attempts) VALUES ($1, 'h', '{}', 'nope', 3)`, newID())
	requireSQLState(t, err, pgForeignKeyViolation)

	requireSQLState(t, insertAttempt(pool, newID(), 1, "running"), pgForeignKeyViolation)

	_, err = pool.Exec(ctx, `INSERT INTO job_events (job_id, type) VALUES ($1, 'created')`, newID())
	requireSQLState(t, err, pgForeignKeyViolation)

	if _, err := pool.Exec(ctx, `INSERT INTO queues (name) VALUES ('other')`); err != nil {
		t.Fatalf("insert queue: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO jobs (id, handler, payload, queue, max_attempts) VALUES ($1, 'h', '{}', 'other', 3)`, newID())
	if err != nil {
		t.Fatalf("job in known queue: %v", err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM queues WHERE name = 'other'`)
	requireSQLState(t, err, pgForeignKeyViolation)
}

func TestDeletingJobCascadesToAttemptsAndEvents(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)
	jobID := mustInsertJob(t, pool)
	if err := insertAttempt(pool, jobID, 1, "running"); err != nil {
		t.Fatalf("attempt: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO job_events (job_id, type) VALUES ($1, 'created')`, jobID); err != nil {
		t.Fatalf("event: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, jobID); err != nil {
		t.Fatalf("delete job: %v", err)
	}
	if n := count(t, pool, "job_attempts", "job_id = $1", jobID); n != 0 {
		t.Errorf("want attempts cascaded, %d left", n)
	}
	if n := count(t, pool, "job_events", "job_id = $1", jobID); n != 0 {
		t.Errorf("want events cascaded, %d left", n)
	}
}

func TestOwnerColumnsHaveNoWorkerForeignKey(t *testing.T) {
	pool := pgtest.NewPool(t)
	id := mustInsertJob(t, pool)
	_, err := pool.Exec(context.Background(),
		`UPDATE jobs SET status = 'running', worker_id = $2, claim_token = $3, lease_expires_at = now() WHERE id = $1`,
		id, newID(), newID())
	if err != nil {
		t.Fatalf("job owned by an unregistered worker should be accepted: %v", err)
	}
}

const claimQuery = `SELECT id FROM jobs
	WHERE status = 'pending' AND queue = $1 AND scheduled_at <= now()
	ORDER BY priority DESC, created_at, id
	LIMIT 10
	FOR UPDATE SKIP LOCKED`

// The plan is the planner's own choice on realistic data: no enable_* switches.
func TestClaimQueryUsesClaimIndex(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPool(t)

	seed := []string{
		`INSERT INTO queues (name) SELECT 'q' || g FROM generate_series(1, 3) g`,
		`INSERT INTO jobs (id, handler, payload, queue, status, priority, max_attempts, created_at)
		 SELECT gen_random_uuid(), 'h', '{}',
		        (ARRAY['default','q1','q2','q3'])[1 + g % 4], 'pending',
		        g % 101, 3, now() - make_interval(secs => g)
		 FROM generate_series(1, 2000) g`,
		`INSERT INTO jobs (id, handler, payload, queue, status, priority, max_attempts, completed_at)
		 SELECT gen_random_uuid(), 'h', '{}',
		        (ARRAY['default','q1','q2','q3'])[1 + g % 4],
		        (ARRAY['completed','failed','cancelled'])[1 + g % 3],
		        50, 3, now()
		 FROM generate_series(1, 6000) g`,
		`ANALYZE jobs`,
	}
	for _, q := range seed {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, "EXPLAIN "+claimQuery, "q1")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	text := strings.Join(plan, "\n")
	t.Logf("claim plan:\n%s", text)
	if !strings.Contains(text, "jobs_claim_idx") {
		t.Fatalf("plan does not use jobs_claim_idx:\n%s", text)
	}
}
