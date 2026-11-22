//go:build integration

package jobsrepo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/queuesrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

type seed struct {
	queue   string
	handler string
	status  domain.JobStatus
	at      time.Time
	id      uuid.UUID
	worker  *uuid.UUID
}

// insert stores one job per seed and returns them in the same order.
func insert(t *testing.T, pool *pgxpool.Pool, seeds ...seed) []domain.Job {
	t.Helper()
	ctx := context.Background()
	out := make([]domain.Job, 0, len(seeds))
	for _, s := range seeds {
		job := newJob()
		job.Payload = json.RawMessage(`{"secret":"payload"}`)
		if s.queue != "" {
			job.Queue = s.queue
		}
		if s.handler != "" {
			job.Handler = s.handler
		}
		if s.status != "" {
			job.Status = s.status
		}
		if !s.at.IsZero() {
			job.CreatedAt, job.UpdatedAt = s.at, s.at
		}
		if s.id != uuid.Nil {
			job.ID = s.id
		}
		if err := queuesrepo.Ensure(ctx, pool, job.Queue); err != nil {
			t.Fatalf("Ensure queue: %v", err)
		}
		if err := jobsrepo.Create(ctx, pool, job); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if s.worker != nil {
			if _, err := pool.Exec(ctx, `UPDATE jobs SET worker_id = $1 WHERE id = $2`, *s.worker, job.ID); err != nil {
				t.Fatalf("set worker: %v", err)
			}
		}
		out = append(out, job)
	}
	return out
}

func filter() domain.JobFilter {
	return domain.JobFilter{Sort: domain.SortCreatedAtDesc, Limit: domain.MaxListLimit}
}

func list(t *testing.T, pool *pgxpool.Pool, f domain.JobFilter) domain.JobPage {
	t.Helper()
	page, err := jobsrepo.List(context.Background(), pool, f)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return page
}

func ids(jobs []domain.Job) []uuid.UUID {
	out := make([]uuid.UUID, len(jobs))
	for i, j := range jobs {
		out[i] = j.ID
	}
	return out
}

func sameIDs(t *testing.T, got []domain.Job, want ...domain.Job) {
	t.Helper()
	g, w := ids(got), ids(want)
	slices.SortFunc(g, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	slices.SortFunc(w, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	if !slices.Equal(g, w) {
		t.Fatalf("ids = %v, want %v", g, w)
	}
}

func TestListEmptyTable(t *testing.T) {
	page := list(t, pgtest.NewPool(t), filter())

	if len(page.Items) != 0 || page.Next != nil {
		t.Fatalf("page = %+v", page)
	}
}

func TestListLeavesPayloadOut(t *testing.T) {
	pool := pgtest.NewPool(t)
	want := insert(t, pool, seed{})[0]

	page := list(t, pool, filter())

	if len(page.Items) != 1 || page.Items[0].Payload != nil {
		t.Fatalf("items = %+v", page.Items)
	}
	got := page.Items[0]
	got.Payload = want.Payload
	if got.ID != want.ID || got.Handler != want.Handler || got.Queue != want.Queue || got.Status != want.Status ||
		!got.CreatedAt.Equal(want.CreatedAt) || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestListFilterByStatusMatchesAnyGivenStatus(t *testing.T) {
	pool := pgtest.NewPool(t)
	jobs := insert(t, pool,
		seed{status: domain.JobPending}, seed{status: domain.JobRunning},
		seed{status: domain.JobFailed}, seed{status: domain.JobCompleted})
	f := filter()

	f.Statuses = []domain.JobStatus{domain.JobPending}
	sameIDs(t, list(t, pool, f).Items, jobs[0])

	f.Statuses = []domain.JobStatus{domain.JobRunning, domain.JobFailed}
	sameIDs(t, list(t, pool, f).Items, jobs[1], jobs[2])
}

func TestListFilterByQueueMatchesAnyGivenQueue(t *testing.T) {
	pool := pgtest.NewPool(t)
	jobs := insert(t, pool, seed{queue: "a"}, seed{queue: "b"}, seed{queue: "c"})
	f := filter()

	f.Queues = []string{"a", "c"}

	sameIDs(t, list(t, pool, f).Items, jobs[0], jobs[2])
}

func TestListStatusAndQueueCombineWithAnd(t *testing.T) {
	pool := pgtest.NewPool(t)
	jobs := insert(t, pool,
		seed{queue: "a", status: domain.JobPending}, seed{queue: "a", status: domain.JobFailed},
		seed{queue: "b", status: domain.JobPending}, seed{queue: "b", status: domain.JobFailed},
		seed{queue: "c", status: domain.JobPending})
	f := filter()
	f.Statuses = []domain.JobStatus{domain.JobPending, domain.JobFailed}
	f.Queues = []string{"a", "b"}

	sameIDs(t, list(t, pool, f).Items, jobs[:4]...)

	f.Statuses = []domain.JobStatus{domain.JobFailed}
	f.Queues = []string{"a"}
	sameIDs(t, list(t, pool, f).Items, jobs[1])
}

func TestListFilterByHandlerIsExact(t *testing.T) {
	pool := pgtest.NewPool(t)
	jobs := insert(t, pool, seed{handler: "demo.echo"}, seed{handler: "demo.echo2"}, seed{handler: "other"})
	f := filter()
	f.Handler = "demo.echo"

	sameIDs(t, list(t, pool, f).Items, jobs[0])
}

func TestListCreatedRangeIncludesAfterAndExcludesBefore(t *testing.T) {
	pool := pgtest.NewPool(t)
	jobs := insert(t, pool,
		seed{at: base}, seed{at: base.Add(time.Hour)}, seed{at: base.Add(2 * time.Hour)}, seed{at: base.Add(3 * time.Hour)})
	after, before := base.Add(time.Hour), base.Add(3*time.Hour)
	f := filter()
	f.CreatedAfter, f.CreatedBefore = &after, &before

	sameIDs(t, list(t, pool, f).Items, jobs[1], jobs[2])
}

func TestListFilterByWorker(t *testing.T) {
	pool := pgtest.NewPool(t)
	w1, w2 := uuid.New(), uuid.New()
	jobs := insert(t, pool, seed{worker: &w1}, seed{worker: &w2}, seed{})
	f := filter()
	f.WorkerID = &w1

	sameIDs(t, list(t, pool, f).Items, jobs[0])
}

func TestListFilterByIDPrefix(t *testing.T) {
	pool := pgtest.NewPool(t)
	a := uuid.MustParse("abcd0000-0000-4000-8000-000000000001")
	b := uuid.MustParse("abcd0000-0000-4000-8000-000000000002")
	c := uuid.MustParse("abce0000-0000-4000-8000-000000000003")
	jobs := insert(t, pool, seed{id: a}, seed{id: b}, seed{id: c})
	tests := []struct {
		prefix string
		want   []domain.Job
	}{
		{"abcd", jobs[:2]},
		{"ABCD0000-0000", jobs[:2]},
		{"abc", jobs},
		{a.String(), jobs[:1]},
		{strings.ReplaceAll(c.String(), "-", ""), jobs[2:]},
		{"ffff", nil},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			f := filter()
			f.IDPrefix = tt.prefix

			sameIDs(t, list(t, pool, f).Items, tt.want...)
		})
	}
}

func TestListFiltersCombineAcrossKinds(t *testing.T) {
	pool := pgtest.NewPool(t)
	w := uuid.New()
	jobs := insert(t, pool,
		seed{queue: "a", handler: "h1", status: domain.JobFailed, at: base, worker: &w},
		seed{queue: "a", handler: "h2", status: domain.JobFailed, at: base, worker: &w},
		seed{queue: "a", handler: "h1", status: domain.JobFailed, at: base.Add(-time.Hour), worker: &w},
		seed{queue: "a", handler: "h1", status: domain.JobPending, at: base, worker: &w})
	after := base.Add(-time.Minute)
	f := filter()
	f.Statuses, f.Queues, f.Handler, f.WorkerID, f.CreatedAfter =
		[]domain.JobStatus{domain.JobFailed}, []string{"a"}, "h1", &w, &after

	sameIDs(t, list(t, pool, f).Items, jobs[0])
}

// seedTies makes n jobs spread over groups distinct created_at values, so
// several jobs share one timestamp.
func seedTies(t *testing.T, pool *pgxpool.Pool, n, groups int) []domain.Job {
	t.Helper()
	seeds := make([]seed, n)
	for i := range seeds {
		seeds[i] = seed{at: base.Add(time.Duration(i%groups) * time.Second)}
	}
	return insert(t, pool, seeds...)
}

func lessAsc(a, b domain.Job) int {
	if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
		return c
	}
	return bytes.Compare(a.ID[:], b.ID[:])
}

func TestListPaginationVisitsEveryJobOnceInBothOrders(t *testing.T) {
	pool := pgtest.NewPool(t)
	all := seedTies(t, pool, 23, 4)
	asc := slices.Clone(all)
	slices.SortFunc(asc, lessAsc)
	desc := slices.Clone(asc)
	slices.Reverse(desc)
	tests := []struct {
		sort domain.JobSort
		want []domain.Job
	}{
		{domain.SortCreatedAtAsc, asc},
		{domain.SortCreatedAtDesc, desc},
	}
	for _, tt := range tests {
		t.Run(string(tt.sort), func(t *testing.T) {
			f := filter()
			f.Sort, f.Limit = tt.sort, 5
			var got []domain.Job
			pages := 0
			for {
				page := list(t, pool, f)
				got = append(got, page.Items...)
				pages++
				if page.Next == nil {
					break
				}
				if pages > 10 {
					t.Fatal("too many pages")
				}
				f.After = page.Next
			}

			if pages != 5 {
				t.Errorf("pages = %d, want 5", pages)
			}
			if !slices.Equal(ids(got), ids(tt.want)) {
				t.Fatalf("order differs:\n got  %v\n want %v", ids(got), ids(tt.want))
			}
		})
	}
}

func TestListPaginationWithAllTimestampsEqual(t *testing.T) {
	pool := pgtest.NewPool(t)
	all := seedTies(t, pool, 12, 1)
	f := filter()
	f.Limit = 5
	seen := map[uuid.UUID]bool{}

	for {
		page := list(t, pool, f)
		for _, j := range page.Items {
			if seen[j.ID] {
				t.Fatalf("job %s returned twice", j.ID)
			}
			seen[j.ID] = true
		}
		if page.Next == nil {
			break
		}
		f.After = page.Next
	}

	if len(seen) != len(all) {
		t.Errorf("saw %d jobs, want %d", len(seen), len(all))
	}
}

func TestListPaginationKeepsFiltersAcrossPages(t *testing.T) {
	pool := pgtest.NewPool(t)
	seeds := make([]seed, 0, 12)
	for i := range 12 {
		status := domain.JobPending
		if i%3 == 0 {
			status = domain.JobFailed
		}
		seeds = append(seeds, seed{status: status, at: base.Add(time.Duration(i%2) * time.Second)})
	}
	insert(t, pool, seeds...)
	f := filter()
	f.Statuses, f.Limit = []domain.JobStatus{domain.JobFailed}, 3
	total := 0

	for {
		page := list(t, pool, f)
		for _, j := range page.Items {
			if j.Status != domain.JobFailed {
				t.Fatalf("got status %s", j.Status)
			}
		}
		total += len(page.Items)
		if page.Next == nil {
			break
		}
		f.After = page.Next
	}

	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
}

func TestListLimitBounds(t *testing.T) {
	pool := pgtest.NewPool(t)
	seedTies(t, pool, 4, 4)
	tests := []struct {
		limit    int
		wantLen  int
		wantNext bool
	}{
		{1, 1, true},
		{3, 3, true},
		{4, 4, false},
		{5, 4, false},
		{domain.MaxListLimit, 4, false},
	}
	for _, tt := range tests {
		f := filter()
		f.Limit = tt.limit

		page := list(t, pool, f)

		if len(page.Items) != tt.wantLen || (page.Next != nil) != tt.wantNext {
			t.Errorf("limit %d: len = %d, next = %v; want %d, %v", tt.limit, len(page.Items), page.Next != nil, tt.wantLen, tt.wantNext)
		}
	}
}

func TestListNextCursorPointsAtLastItem(t *testing.T) {
	pool := pgtest.NewPool(t)
	seedTies(t, pool, 3, 3)
	f := filter()
	f.Limit = 2

	page := list(t, pool, f)

	last := page.Items[len(page.Items)-1]
	if page.Next == nil || page.Next.ID != last.ID || !page.Next.CreatedAt.Equal(last.CreatedAt) {
		t.Fatalf("next = %+v, last = %+v", page.Next, last)
	}
}
