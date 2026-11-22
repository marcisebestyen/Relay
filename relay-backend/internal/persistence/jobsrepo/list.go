package jobsrepo

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// The payload column is left out on purpose: lists stay small.
const listColumns = `id, name, handler, queue, status, priority, scheduled_at,
	max_attempts, timeout_seconds, created_at, updated_at, completed_at, last_error`

// listQuery collects WHERE clauses. Every clause is fixed text; values only
// ever travel as numbered parameters.
type listQuery struct {
	where []string
	args  []any
}

// param stores v and returns its placeholder.
func (b *listQuery) param(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// List returns one page of jobs, without payloads, for the filter. The filter
// must be valid. It reads one extra row to know whether another page exists.
func List(ctx context.Context, q db.Querier, f domain.JobFilter) (domain.JobPage, error) {
	var b listQuery
	if len(f.Statuses) > 0 {
		statuses := make([]string, len(f.Statuses))
		for i, s := range f.Statuses {
			statuses[i] = string(s)
		}
		b.where = append(b.where, "status = ANY("+b.param(statuses)+")")
	}
	if len(f.Queues) > 0 {
		b.where = append(b.where, "queue = ANY("+b.param(f.Queues)+")")
	}
	if f.Handler != "" {
		b.where = append(b.where, "handler = "+b.param(f.Handler))
	}
	if f.CreatedAfter != nil {
		b.where = append(b.where, "created_at >= "+b.param(f.CreatedAfter.UTC()))
	}
	if f.CreatedBefore != nil {
		b.where = append(b.where, "created_at < "+b.param(f.CreatedBefore.UTC()))
	}
	if f.WorkerID != nil {
		b.where = append(b.where, "worker_id = "+b.param(*f.WorkerID))
	}
	if lo, hi, ok := f.IDRange(); ok {
		b.where = append(b.where, "id BETWEEN "+b.param(lo)+" AND "+b.param(hi))
	}

	cmp, order := "<", "created_at DESC, id DESC"
	if f.Sort == domain.SortCreatedAtAsc {
		cmp, order = ">", "created_at ASC, id ASC"
	}
	if f.After != nil {
		b.where = append(b.where, "(created_at, id) "+cmp+" ("+b.param(f.After.CreatedAt.UTC())+", "+b.param(f.After.ID)+")")
	}

	query := "SELECT " + listColumns + " FROM jobs"
	if len(b.where) > 0 {
		query += " WHERE " + strings.Join(b.where, " AND ")
	}
	query += " ORDER BY " + order + " LIMIT " + b.param(f.Limit+1)

	rows, err := q.Query(ctx, query, b.args...)
	if err != nil {
		return domain.JobPage{}, fmt.Errorf("select jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]domain.Job, 0, f.Limit+1)
	for rows.Next() {
		job, err := scanSummary(rows)
		if err != nil {
			return domain.JobPage{}, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return domain.JobPage{}, fmt.Errorf("select jobs: %w", err)
	}

	page := domain.JobPage{Items: jobs}
	if len(jobs) > f.Limit {
		page.Items = jobs[:f.Limit]
		last := page.Items[f.Limit-1]
		page.Next = &domain.JobCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func scanSummary(row pgx.Row) (domain.Job, error) {
	var (
		job     domain.Job
		name    *string
		status  string
		timeout *int
	)
	err := row.Scan(&job.ID, &name, &job.Handler, &job.Queue, &status, &job.Priority, &job.ScheduledAt,
		&job.MaxAttempts, &timeout, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt, &job.LastError)
	if err != nil {
		return domain.Job{}, fmt.Errorf("scan job: %w", err)
	}
	fill(&job, name, status, timeout)
	return job, nil
}
