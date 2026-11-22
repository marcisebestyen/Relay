//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

type attemptsPage struct {
	Items []struct {
		AttemptNumber int    `json:"attempt_number"`
		DurationMs    *int64 `json:"duration_ms"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type eventsPage struct {
	Items []struct {
		ID   int64          `json:"id"`
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func createJobVia(t *testing.T, h http.Handler, body string) string {
	t.Helper()
	rec := postJob(h, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	return strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/jobs/")
}

func seedAttempts(t *testing.T, pool *pgxpool.Pool, jobID string, count int) {
	t.Helper()
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	// Numbers are inserted in reverse so table order cannot fake the sort.
	for n := count; n >= 1; n-- {
		var finished *time.Time
		status := "failed"
		if n == count {
			status = "running"
		} else {
			end := start.Add(time.Duration(n) * time.Second)
			finished = &end
		}
		_, err := pool.Exec(context.Background(), `
			INSERT INTO job_attempts (id, job_id, attempt_number, worker_id, status, started_at, finished_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, uuid.New(), jobID, n, uuid.New(), status, start, finished)
		if err != nil {
			t.Fatalf("seed attempt: %v", err)
		}
	}
}

func get[T any](t *testing.T, h http.Handler, path string) T {
	t.Helper()
	rec := getPath(h, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return out
}

func TestJobAttempts_OrderedAndPaged(t *testing.T) {
	pool := pgtest.NewPool(t)
	h := jobsRouterOn(pool)
	id := createJobVia(t, h, `{"handler":"demo.echo"}`)
	seedAttempts(t, pool, id, 5)
	base := "/api/v1/jobs/" + id + "/attempts"

	var seen []int
	path := base + "?limit=2"
	for pages := 0; pages < 10; pages++ {
		page := get[attemptsPage](t, h, path)
		for _, a := range page.Items {
			seen = append(seen, a.AttemptNumber)
		}
		if page.NextCursor == nil {
			break
		}
		if len(page.Items) != 2 {
			t.Fatalf("full page expected while next_cursor is set: %+v", page)
		}
		path = base + "?limit=2&cursor=" + *page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("seen = %v", seen)
	}
	for i, n := range seen {
		if n != i+1 {
			t.Fatalf("seen = %v, want 1..5 in order", seen)
		}
	}
}

func TestJobAttempts_DerivedDurationAndExactLastPage(t *testing.T) {
	pool := pgtest.NewPool(t)
	h := jobsRouterOn(pool)
	id := createJobVia(t, h, `{"handler":"demo.echo"}`)
	seedAttempts(t, pool, id, 3)

	page := get[attemptsPage](t, h, "/api/v1/jobs/"+id+"/attempts?limit=3")
	if page.NextCursor != nil {
		t.Errorf("next_cursor = %q on an exact last page", *page.NextCursor)
	}
	if len(page.Items) != 3 || page.Items[0].DurationMs == nil || *page.Items[0].DurationMs != 1000 ||
		page.Items[1].DurationMs == nil || *page.Items[1].DurationMs != 2000 || page.Items[2].DurationMs != nil {
		t.Errorf("durations = %+v", page.Items)
	}
}

func TestJobAttempts_NoAttemptsIsEmptyList(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	id := createJobVia(t, h, `{"handler":"demo.echo"}`)
	rec := getPath(h, "/api/v1/jobs/"+id+"/attempts")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestJobHistory_UnknownJobIs404(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	for _, suffix := range []string{"attempts", "events"} {
		rec := getPath(h, "/api/v1/jobs/"+uuid.NewString()+"/"+suffix)
		if rec.Code != http.StatusNotFound || decodeError(t, rec).Error.Code != "not_found" {
			t.Errorf("%s: got %d %s", suffix, rec.Code, rec.Body)
		}
	}
}

func TestJobEvents_OrderedAndPaged(t *testing.T) {
	pool := pgtest.NewPool(t)
	h := jobsRouterOn(pool)
	id := createJobVia(t, h, `{"handler":"demo.echo"}`)
	// The job.created event is stamped now; these are later and inserted out of order.
	base := time.Now().UTC().Add(time.Hour)
	for i, typ := range []string{"attempt.completed", "attempt.started", "retry.scheduled"} {
		at := base.Add(time.Duration([]int{2, 0, 1}[i]) * time.Minute)
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO job_events (job_id, type, created_at) VALUES ($1, $2, $3)`, id, typ, at); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"job.created", "attempt.started", "retry.scheduled", "attempt.completed"}

	var seen []string
	path := "/api/v1/jobs/" + id + "/events?limit=3"
	for pages := 0; pages < 10; pages++ {
		page := get[eventsPage](t, h, path)
		for _, e := range page.Items {
			seen = append(seen, e.Type)
		}
		if page.NextCursor == nil {
			break
		}
		path = "/api/v1/jobs/" + id + "/events?limit=3&cursor=" + *page.NextCursor
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("events = %v, want %v", seen, want)
	}
}

func TestJobEvents_DataNeverContainsJobPayload(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	id := createJobVia(t, h, `{"handler":"demo.echo","payload":{"secret":"PAYLOAD-MARKER-4711","nested":{"k":"PAYLOAD-MARKER-4711"}}}`)

	rec := getPath(h, "/api/v1/jobs/"+id+"/events")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "PAYLOAD-MARKER-4711") || strings.Contains(rec.Body.String(), `"payload"`) {
		t.Errorf("events leak the job payload: %s", rec.Body)
	}
	page := get[eventsPage](t, h, "/api/v1/jobs/"+id+"/events")
	if len(page.Items) != 1 || page.Items[0].Type != "job.created" ||
		page.Items[0].Data["handler"] != "demo.echo" || page.Items[0].Data["queue"] != "default" {
		t.Errorf("events = %+v", page.Items)
	}
}
