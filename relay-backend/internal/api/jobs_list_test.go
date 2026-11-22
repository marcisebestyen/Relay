package api_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

func getList(h http.Handler, query string) *httptest.ResponseRecorder {
	return do(h, http.MethodGet, "/api/v1/jobs?"+query, nil, nil)
}

func TestListJobs_DefaultsToNewestFirstLimit50(t *testing.T) {
	svc := &fakeJobs{}

	rec := getList(jobsRouter(svc, 1024), "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	in := svc.listIn
	if in.Sort != domain.SortCreatedAtDesc || in.Limit != 50 || in.After != nil || len(in.Statuses) != 0 || len(in.Queues) != 0 {
		t.Errorf("filter = %+v", in)
	}
}

func TestListJobs_PassesEveryParamToService(t *testing.T) {
	svc := &fakeJobs{}
	worker := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	cursor := base64.RawURLEncoding.EncodeToString([]byte("2026-03-04T05:06:07.123456Z|" + sampleJobID.String()))

	rec := getList(jobsRouter(svc, 1024), "status=pending&status=failed&queue=a&queue=b&handler=demo.echo"+
		"&created_after=2026-01-01T02:00:00%2B02:00&created_before=2026-02-01T00:00:00Z"+
		"&worker_id="+worker.String()+"&id_prefix=1234&sort=created_at&limit=7&cursor="+cursor)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	in := svc.listIn
	if len(in.Statuses) != 2 || in.Statuses[0] != domain.JobPending || in.Statuses[1] != domain.JobFailed {
		t.Errorf("statuses = %v", in.Statuses)
	}
	if len(in.Queues) != 2 || in.Queues[0] != "a" || in.Queues[1] != "b" || in.Handler != "demo.echo" {
		t.Errorf("queues = %v, handler = %q", in.Queues, in.Handler)
	}
	if in.CreatedAfter == nil || !in.CreatedAfter.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) ||
		in.CreatedAfter.Location() != time.UTC {
		t.Errorf("created_after = %v", in.CreatedAfter)
	}
	if in.CreatedBefore == nil || !in.CreatedBefore.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("created_before = %v", in.CreatedBefore)
	}
	if in.WorkerID == nil || *in.WorkerID != worker || in.IDPrefix != "1234" {
		t.Errorf("worker = %v, id_prefix = %q", in.WorkerID, in.IDPrefix)
	}
	if in.Sort != domain.SortCreatedAtAsc || in.Limit != 7 {
		t.Errorf("sort = %q, limit = %d", in.Sort, in.Limit)
	}
	if in.After == nil || in.After.ID != sampleJobID ||
		!in.After.CreatedAt.Equal(time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)) {
		t.Errorf("after = %+v", in.After)
	}
}

func TestListJobs_ReturnsSummariesWithoutPayloadAndNextCursor(t *testing.T) {
	job := sampleJob()
	job.Payload = nil
	svc := &fakeJobs{listPage: domain.JobPage{
		Items: []domain.Job{job},
		Next:  &domain.JobCursor{CreatedAt: job.CreatedAt, ID: job.ID},
	}}

	rec := getList(jobsRouter(svc, 1024), "")

	var got struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0]["id"] != sampleJobID.String() || got.Items[0]["handler"] != "demo.echo" {
		t.Fatalf("items = %s", rec.Body)
	}
	if _, has := got.Items[0]["payload"]; has {
		t.Errorf("list item has a payload: %s", rec.Body)
	}
	if got.NextCursor == nil || *got.NextCursor == "" {
		t.Errorf("next_cursor = %v", got.NextCursor)
	}
}

func TestListJobs_EmptyLastPageHasEmptyItemsAndNullCursor(t *testing.T) {
	rec := getList(jobsRouter(&fakeJobs{}, 1024), "")

	if !strings.Contains(rec.Body.String(), `"items":[]`) || !strings.Contains(rec.Body.String(), `"next_cursor":null`) {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestListJobs_NextCursorCanBePassedBack(t *testing.T) {
	first := &fakeJobs{listPage: domain.JobPage{
		Next: &domain.JobCursor{CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC), ID: sampleJobID},
	}}
	var page struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(getList(jobsRouter(first, 1024), "").Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}

	second := &fakeJobs{}
	rec := getList(jobsRouter(second, 1024), "cursor="+page.NextCursor)

	if rec.Code != http.StatusOK || second.listIn.After == nil || second.listIn.After.ID != sampleJobID ||
		!second.listIn.After.CreatedAt.Equal(first.listPage.Next.CreatedAt) {
		t.Errorf("status = %d, after = %+v", rec.Code, second.listIn.After)
	}
}

func TestListJobs_TamperedCursorReturns400(t *testing.T) {
	svc := &fakeJobs{}

	rec := getList(jobsRouter(svc, 1024), "cursor=not-a-cursor")

	if rec.Code != http.StatusBadRequest || decodeError(t, rec).Error.Code != "bad_request" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestListJobs_UnparsableParamsReturn422WithParamName(t *testing.T) {
	tests := map[string]string{
		"limit=abc":               "limit",
		"limit=1&limit=2":         "limit",
		"created_after=yesterday": "created_after",
		"created_before=2026-13":  "created_before",
		"worker_id=nope":          "worker_id",
	}
	for query, param := range tests {
		t.Run(query, func(t *testing.T) {
			rec := getList(jobsRouter(&fakeJobs{}, 1024), query)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			e := decodeError(t, rec).Error
			if e.Code != "validation_failed" || e.Details[param] == nil {
				t.Errorf("error = %+v", e)
			}
		})
	}
}

func TestListJobs_ServiceValidationErrorsReturn422WithFields(t *testing.T) {
	svc := &fakeJobs{listErr: domain.ValidationErrors{"limit": "must be between 1 and 200", "status": "bad"}}

	rec := getList(jobsRouter(svc, 1024), "limit=500&status=weird")

	e := decodeError(t, rec).Error
	if rec.Code != http.StatusUnprocessableEntity || e.Code != "validation_failed" ||
		e.Details["limit"] == nil || e.Details["status"] == nil {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestListJobs_UnexpectedErrorReturns500WithoutLeak(t *testing.T) {
	svc := &fakeJobs{listErr: errors.New("pq: password=hunter2")}

	rec := getList(jobsRouter(svc, 1024), "")

	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}
