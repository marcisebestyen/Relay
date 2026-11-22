package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

type fakeJobs struct {
	createIn  domain.NewJobInput
	createJob domain.Job
	createErr error
	getJob    domain.Job
	getErr    error
	getID     uuid.UUID
	*fakeHistory
	listIn   domain.JobFilter
	listPage domain.JobPage
	listErr  error
}

func (f *fakeJobs) Create(_ context.Context, in domain.NewJobInput) (domain.Job, error) {
	f.createIn = in
	return f.createJob, f.createErr
}

func (f *fakeJobs) Get(_ context.Context, id uuid.UUID) (domain.Job, error) {
	f.getID = id
	return f.getJob, f.getErr
}

func (f *fakeJobs) List(_ context.Context, in domain.JobFilter) (domain.JobPage, error) {
	f.listIn = in
	return f.listPage, f.listErr
}

var sampleJobID = uuid.MustParse("11111111-2222-3333-4444-555555555555")

func sampleJob() domain.Job {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return domain.Job{
		ID: sampleJobID, Name: "report", Handler: "demo.echo", Payload: json.RawMessage(`{"n":12345678901234567890}`),
		Queue: "reports", Status: domain.JobPending, Priority: 10, ScheduledAt: at,
		MaxAttempts: 3, TimeoutSeconds: 60, CreatedAt: at, UpdatedAt: at,
	}
}

func jobsRouter(svc api.JobService, maxBody int64) http.Handler {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return api.NewRouter(api.Deps{
		Logger:       logger,
		Pinger:       fakePinger{},
		MaxBodyBytes: maxBody,
		Routes:       api.NewJobsHandler(svc, logger).Register,
	})
}

func postJob(h http.Handler, body string) *httptest.ResponseRecorder {
	return do(h, http.MethodPost, "/api/v1/jobs", map[string]string{"Content-Type": "application/json"}, strings.NewReader(body))
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body is not an error envelope: %v (%s)", err, rec.Body)
	}
	return b
}

func TestCreateJob_ReturnsCreatedWithLocation(t *testing.T) {
	svc := &fakeJobs{createJob: sampleJob()}
	rec := postJob(jobsRouter(svc, 1024), `{"handler":"demo.echo"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/jobs/"+sampleJobID.String() {
		t.Errorf("Location = %q", loc)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != sampleJobID.String() || got["status"] != "pending" || got["queue"] != "reports" {
		t.Errorf("body = %s", rec.Body)
	}
	if got["scheduled_at"] != "2026-03-04T05:06:07Z" {
		t.Errorf("scheduled_at = %v", got["scheduled_at"])
	}
	if got["completed_at"] != nil || got["last_error"] != nil {
		t.Errorf("completed_at/last_error should be null: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"payload":{"n":12345678901234567890}`) {
		t.Errorf("payload not kept byte for byte: %s", rec.Body)
	}
}

func TestCreateJob_PassesRequestFieldsToService(t *testing.T) {
	svc := &fakeJobs{createJob: sampleJob()}
	body := `{"handler":"demo.echo","name":"report","queue":"reports","priority":7,"max_attempts":2,
		"timeout_seconds":30,"scheduled_at":"2030-01-02T03:04:05+02:00","payload":{"a":1}}`
	rec := postJob(jobsRouter(svc, 1024), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	in := svc.createIn
	if in.Handler != "demo.echo" || in.Name != "report" || in.Queue != "reports" {
		t.Errorf("strings = %+v", in)
	}
	if in.Priority == nil || *in.Priority != 7 || in.MaxAttempts == nil || *in.MaxAttempts != 2 ||
		in.TimeoutSeconds == nil || *in.TimeoutSeconds != 30 {
		t.Errorf("numbers = %+v", in)
	}
	want := time.Date(2030, 1, 2, 1, 4, 5, 0, time.UTC)
	if in.ScheduledAt == nil || !in.ScheduledAt.Equal(want) || in.ScheduledAt.Location() != time.UTC {
		t.Errorf("scheduled_at = %v", in.ScheduledAt)
	}
	if string(in.Payload) != `{"a":1}` {
		t.Errorf("payload = %s", in.Payload)
	}
}

func TestCreateJob_OmittedOptionalFieldsStayUnset(t *testing.T) {
	svc := &fakeJobs{createJob: sampleJob()}
	postJob(jobsRouter(svc, 1024), `{"handler":"demo.echo"}`)
	in := svc.createIn
	if in.Priority != nil || in.MaxAttempts != nil || in.TimeoutSeconds != nil || in.ScheduledAt != nil || len(in.Payload) != 0 {
		t.Errorf("input = %+v", in)
	}
}

func TestCreateJob_BadRequests(t *testing.T) {
	cases := map[string]string{
		"malformed json": `{"handler":`,
		"empty body":     ``,
		"not an object":  `[1,2]`,
		"unknown field":  `{"handler":"demo.echo","max_attempt":2}`,
		"trailing data":  `{"handler":"demo.echo"} {"x":1}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			svc := &fakeJobs{}
			rec := postJob(jobsRouter(svc, 1024), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if code := decodeError(t, rec).Error.Code; code != "bad_request" {
				t.Errorf("code = %q", code)
			}
		})
	}
}

func TestCreateJob_WrongFieldTypeIsValidationFailure(t *testing.T) {
	rec := postJob(jobsRouter(&fakeJobs{}, 1024), `{"handler":"demo.echo","priority":"high"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	e := decodeError(t, rec).Error
	if e.Code != "validation_failed" || e.Details["priority"] == nil {
		t.Errorf("error = %+v", e)
	}
}

func TestCreateJob_TooLargeReturns413(t *testing.T) {
	rec := postJob(jobsRouter(&fakeJobs{}, 20), `{"handler":"demo.echo","payload":{"k":"too long"}}`)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if code := decodeError(t, rec).Error.Code; code != "payload_too_large" {
		t.Errorf("code = %q", code)
	}
}

func TestCreateJob_ValidationErrorsReturn422WithFields(t *testing.T) {
	svc := &fakeJobs{createErr: domain.ValidationErrors{"handler": "is required", "priority": "must be between 0 and 100"}}
	rec := postJob(jobsRouter(svc, 1024), `{}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	e := decodeError(t, rec).Error
	if e.Code != "validation_failed" || e.Details["handler"] != "is required" || e.Details["priority"] != "must be between 0 and 100" {
		t.Errorf("error = %+v", e)
	}
}

func TestCreateJob_UnexpectedErrorReturns500WithoutLeak(t *testing.T) {
	svc := &fakeJobs{createErr: errors.New("pq: password=hunter2")}
	rec := postJob(jobsRouter(svc, 1024), `{"handler":"demo.echo"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if code := decodeError(t, rec).Error.Code; code != "internal" {
		t.Errorf("code = %q", code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("leaked cause: %s", rec.Body)
	}
}

func TestGetJob_ReturnsJob(t *testing.T) {
	svc := &fakeJobs{getJob: sampleJob()}
	rec := do(jobsRouter(svc, 1024), http.MethodGet, "/api/v1/jobs/"+sampleJobID.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if svc.getID != sampleJobID {
		t.Errorf("service got id %v", svc.getID)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != sampleJobID.String() || got["handler"] != "demo.echo" || got["name"] != "report" {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestGetJob_OmitsEmptyName(t *testing.T) {
	j := sampleJob()
	j.Name = ""
	rec := do(jobsRouter(&fakeJobs{getJob: j}, 1024), http.MethodGet, "/api/v1/jobs/"+sampleJobID.String(), nil, nil)
	if strings.Contains(rec.Body.String(), `"name"`) {
		t.Errorf("name should be omitted: %s", rec.Body)
	}
}

func TestGetJob_InvalidUUIDReturns400Envelope(t *testing.T) {
	rec := do(jobsRouter(&fakeJobs{}, 1024), http.MethodGet, "/api/v1/jobs/not-a-uuid", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if code := decodeError(t, rec).Error.Code; code != "bad_request" {
		t.Errorf("code = %q", code)
	}
}

func TestGetJob_NotFoundReturns404Envelope(t *testing.T) {
	svc := &fakeJobs{getErr: db.ErrNotFound}
	rec := do(jobsRouter(svc, 1024), http.MethodGet, "/api/v1/jobs/"+sampleJobID.String(), nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if code := decodeError(t, rec).Error.Code; code != "not_found" {
		t.Errorf("code = %q", code)
	}
}

func TestGetJob_UnexpectedErrorReturns500(t *testing.T) {
	svc := &fakeJobs{getErr: errors.New("boom")}
	rec := do(jobsRouter(svc, 1024), http.MethodGet, "/api/v1/jobs/"+sampleJobID.String(), nil, nil)
	if rec.Code != http.StatusInternalServerError || decodeError(t, rec).Error.Code != "internal" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestOpenAPISpecRouteStillServedWithJobsRegistered(t *testing.T) {
	rec := do(jobsRouter(&fakeJobs{}, 1024), http.MethodGet, "/api/v1/openapi.yaml", nil, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}
