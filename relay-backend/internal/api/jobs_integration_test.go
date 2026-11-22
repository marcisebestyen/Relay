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
