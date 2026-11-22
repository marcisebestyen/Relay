//go:build integration

package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

var jobDefaults = domain.Defaults{Queue: "default", Priority: 50, MaxAttempts: 3, TimeoutSeconds: 0}

func jobsRouterOn(pool *pgxpool.Pool) http.Handler {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := jobs.NewService(pool, jobDefaults, nil, nil)
	return api.NewRouter(api.Deps{
		Logger: logger, Pinger: pool, MaxBodyBytes: 4096,
		Routes: api.NewJobsHandler(svc, logger).Register,
	})
}

func TestJobs_CreateThenGetRoundTrip(t *testing.T) {
	pool := pgtest.NewPool(t)
	h := jobsRouterOn(pool)
	rec := postJob(h, `{"handler":"demo.echo","name":"report","queue":"Reports","payload":{"n":12345678901234567890}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	loc := rec.Header().Get("Location")

	// A fresh router on the same pool stands in for an API restart.
	got := do(jobsRouterOn(pool), http.MethodGet, loc, nil, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get = %d %s", got.Code, got.Body)
	}
	if got.Body.String() != rec.Body.String() {
		t.Errorf("get body differs from create body\ncreate: %s\nget:    %s", rec.Body, got.Body)
	}
	var j map[string]any
	if err := json.Unmarshal(got.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if j["status"] != "pending" || j["queue"] != "reports" || j["name"] != "report" {
		t.Errorf("job = %s", got.Body)
	}
	if !strings.Contains(got.Body.String(), `"payload":{"n":12345678901234567890}`) {
		t.Errorf("payload changed: %s", got.Body)
	}
}

func TestJobs_DefaultsApplied(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	rec := postJob(h, `{"handler":"demo.echo"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	var j map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if j["queue"] != "default" || j["priority"] != float64(50) || j["max_attempts"] != float64(3) ||
		j["timeout_seconds"] != float64(0) || j["status"] != "pending" {
		t.Errorf("defaults = %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"payload":{}`) {
		t.Errorf("payload default = %s", rec.Body)
	}
}

func TestJobs_FutureScheduledAtReturnedUnchangedInUTC(t *testing.T) {
	pool := pgtest.NewPool(t)
	at := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	// Sent with a +02:00 offset; must come back as the same instant in UTC.
	sent := at.In(time.FixedZone("x", 2*3600)).Format(time.RFC3339)
	rec := postJob(jobsRouterOn(pool), `{"handler":"demo.echo","scheduled_at":"`+sent+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	got := do(jobsRouterOn(pool), http.MethodGet, rec.Header().Get("Location"), nil, nil)
	var j struct {
		ScheduledAt string `json:"scheduled_at"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if want := at.Format(time.RFC3339); j.ScheduledAt != want {
		t.Errorf("scheduled_at = %q, want %q", j.ScheduledAt, want)
	}
}

func TestJobs_ValidationFailureReturns422AndStoresNothing(t *testing.T) {
	pool := pgtest.NewPool(t)
	rec := postJob(jobsRouterOn(pool), `{"handler":"Bad Handler","priority":999}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	e := decodeError(t, rec).Error
	if e.Details["handler"] == nil || e.Details["priority"] == nil {
		t.Errorf("details = %v", e.Details)
	}
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("jobs rows = %d, err %v", n, err)
	}
}

func TestJobs_UnknownIDReturns404(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	rec := do(h, http.MethodGet, "/api/v1/jobs/99999999-9999-9999-9999-999999999999", nil, nil)
	if rec.Code != http.StatusNotFound || decodeError(t, rec).Error.Code != "not_found" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

type listBody struct {
	Items []struct {
		ID      string `json:"id"`
		Queue   string `json:"queue"`
		Handler string `json:"handler"`
		Status  string `json:"status"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func getListBody(t *testing.T, h http.Handler, query string) listBody {
	t.Helper()
	rec := getList(h, query)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %q = %d %s", query, rec.Code, rec.Body)
	}
	var b listBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestJobs_ListFiltersAndLeavesPayloadOut(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	for _, body := range []string{
		`{"handler":"demo.echo","queue":"a","payload":{"secret":1}}`,
		`{"handler":"demo.echo","queue":"b"}`,
		`{"handler":"other","queue":"a"}`,
	} {
		if rec := postJob(h, body); rec.Code != http.StatusCreated {
			t.Fatalf("create = %d %s", rec.Code, rec.Body)
		}
	}

	all := getList(h, "")
	if strings.Contains(all.Body.String(), "payload") || strings.Contains(all.Body.String(), "secret") {
		t.Errorf("list leaks payload: %s", all.Body)
	}
	if got := getListBody(t, h, "queue=a"); len(got.Items) != 2 {
		t.Errorf("queue=a returned %d items", len(got.Items))
	}
	if got := getListBody(t, h, "queue=a&queue=b&handler=demo.echo"); len(got.Items) != 2 {
		t.Errorf("queue OR + handler returned %d items", len(got.Items))
	}
	if got := getListBody(t, h, "queue=b&handler=other"); len(got.Items) != 0 || got.NextCursor != nil {
		t.Errorf("AND of non matching filters returned %+v", got)
	}
	if got := getListBody(t, h, "status=pending&status=failed&sort=created_at"); len(got.Items) != 3 {
		t.Errorf("status returned %d items", len(got.Items))
	}
}

func TestJobs_ListPagesThroughEveryJobOnceInBothOrders(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	created := map[string]bool{}
	for range 7 {
		rec := postJob(h, `{"handler":"demo.echo"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d %s", rec.Code, rec.Body)
		}
		created[strings.TrimPrefix(rec.Header().Get("Location"), "/api/v1/jobs/")] = true
	}
	for _, sort := range []string{"created_at", "-created_at"} {
		t.Run(sort, func(t *testing.T) {
			seen := map[string]bool{}
			query := "limit=3&sort=" + sort
			for pages := 0; ; pages++ {
				if pages > 3 {
					t.Fatal("too many pages")
				}
				page := getListBody(t, h, query)
				for _, it := range page.Items {
					if seen[it.ID] {
						t.Fatalf("job %s returned twice", it.ID)
					}
					seen[it.ID] = true
				}
				if page.NextCursor == nil {
					break
				}
				query = "limit=3&sort=" + sort + "&cursor=" + *page.NextCursor
			}
			if len(seen) != len(created) {
				t.Errorf("saw %d jobs, want %d", len(seen), len(created))
			}
		})
	}
}

func TestJobs_ListRejectsBadParams(t *testing.T) {
	h := jobsRouterOn(pgtest.NewPool(t))
	tests := []struct {
		query  string
		status int
		field  string
	}{
		{"limit=0", http.StatusUnprocessableEntity, "limit"},
		{"limit=201", http.StatusUnprocessableEntity, "limit"},
		{"limit=abc", http.StatusUnprocessableEntity, "limit"},
		{"status=weird", http.StatusUnprocessableEntity, "status"},
		{"sort=priority", http.StatusUnprocessableEntity, "sort"},
		{"id_prefix=zz", http.StatusUnprocessableEntity, "id_prefix"},
		{"queue=Bad%20Queue", http.StatusUnprocessableEntity, "queue"},
		{"worker_id=nope", http.StatusUnprocessableEntity, "worker_id"},
		{"cursor=tampered", http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			rec := getList(h, tt.query)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if tt.field != "" && decodeError(t, rec).Error.Details[tt.field] == nil {
				t.Errorf("details = %v", decodeError(t, rec).Error.Details)
			}
		})
	}
}
