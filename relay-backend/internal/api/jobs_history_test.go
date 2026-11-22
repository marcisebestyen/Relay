package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
)

type fakeHistory struct {
	attempts     jobs.Page[domain.JobAttempt]
	events       jobs.Page[eventsrepo.Event]
	err          error
	gotID        uuid.UUID
	gotAfter     int64
	gotLimit     int
	listCalled   bool
	attemptAfter int
}

func (f *fakeHistory) ListAttempts(_ context.Context, id uuid.UUID, after, limit int) (jobs.Page[domain.JobAttempt], error) {
	f.listCalled, f.gotID, f.attemptAfter, f.gotLimit = true, id, after, limit
	return f.attempts, f.err
}

func (f *fakeHistory) ListEvents(_ context.Context, id uuid.UUID, after int64, limit int) (jobs.Page[eventsrepo.Event], error) {
	f.listCalled, f.gotID, f.gotAfter, f.gotLimit = true, id, after, limit
	return f.events, f.err
}

func historyRouter(h *fakeHistory) http.Handler {
	return jobsRouter(&fakeJobs{fakeHistory: h}, 1024)
}

func getPath(h http.Handler, path string) *httptest.ResponseRecorder {
	return do(h, http.MethodGet, path, nil, nil)
}

func encodeCursor(n int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(n, 10)))
}

var (
	attemptsPath = "/api/v1/jobs/" + sampleJobID.String() + "/attempts"
	eventsPath   = "/api/v1/jobs/" + sampleJobID.String() + "/events"
	t0           = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
)

func sampleAttempts() []domain.JobAttempt {
	end := t0.Add(1500 * time.Millisecond)
	msg := "boom"
	return []domain.JobAttempt{
		{ID: uuid.New(), JobID: sampleJobID, AttemptNumber: 1, WorkerID: uuid.New(), Status: domain.AttemptFailed,
			StartedAt: t0, FinishedAt: &end, Error: &msg, Result: json.RawMessage(`{"a":1}`)},
		{ID: uuid.New(), JobID: sampleJobID, AttemptNumber: 2, WorkerID: uuid.New(), Status: domain.AttemptRunning, StartedAt: t0},
	}
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body: %v (%s)", err, rec.Body)
	}
	return m
}

func TestListAttempts_MapsFieldsAndDerivesDuration(t *testing.T) {
	fake := &fakeHistory{attempts: jobs.Page[domain.JobAttempt]{Items: sampleAttempts()}}
	rec := getPath(historyRouter(fake), attemptsPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	items := decodeMap(t, rec)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %s", rec.Body)
	}
	done, running := items[0].(map[string]any), items[1].(map[string]any)
	if done["attempt_number"] != float64(1) || done["status"] != "failed" || done["duration_ms"] != float64(1500) ||
		done["error"] != "boom" || done["started_at"] != "2026-03-04T05:06:07Z" ||
		done["finished_at"] != "2026-03-04T05:06:08.5Z" || done["job_id"] != sampleJobID.String() {
		t.Errorf("finished attempt = %v", done)
	}
	if done["result"].(map[string]any)["a"] != float64(1) {
		t.Errorf("result = %v", done["result"])
	}
	if running["duration_ms"] != nil || running["finished_at"] != nil || running["error"] != nil || running["result"] != nil {
		t.Errorf("running attempt = %v", running)
	}
	if decodeMap(t, rec)["next_cursor"] != nil {
		t.Errorf("next_cursor should be null on the last page: %s", rec.Body)
	}
}

func TestListAttempts_UsesDefaultLimitAndNoCursor(t *testing.T) {
	fake := &fakeHistory{}
	getPath(historyRouter(fake), attemptsPath)
	if fake.gotID != sampleJobID || fake.gotLimit != 50 || fake.attemptAfter != 0 {
		t.Errorf("service got id=%v limit=%d after=%d", fake.gotID, fake.gotLimit, fake.attemptAfter)
	}
}

func TestListAttempts_EmptyIsEmptyArray(t *testing.T) {
	rec := getPath(historyRouter(&fakeHistory{}), attemptsPath)
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestListAttempts_NextCursorRoundTrips(t *testing.T) {
	as := sampleAttempts()
	fake := &fakeHistory{attempts: jobs.Page[domain.JobAttempt]{Items: as, HasMore: true}}
	h := historyRouter(fake)
	rec := getPath(h, attemptsPath+"?limit=2")
	next, _ := decodeMap(t, rec)["next_cursor"].(string)
	if next == "" {
		t.Fatalf("no next_cursor: %s", rec.Body)
	}
	if fake.gotLimit != 2 {
		t.Errorf("limit = %d", fake.gotLimit)
	}

	getPath(h, attemptsPath+"?cursor="+next)
	if fake.attemptAfter != 2 {
		t.Errorf("after = %d, want the last attempt number 2", fake.attemptAfter)
	}
}

func TestListEvents_MapsFields(t *testing.T) {
	attempt := uuid.New()
	fake := &fakeHistory{events: jobs.Page[eventsrepo.Event]{Items: []eventsrepo.Event{
		{ID: 7, JobID: sampleJobID, AttemptID: &attempt, Type: "attempt.started", Data: json.RawMessage(`{"worker":"w1"}`), CreatedAt: t0},
		{ID: 8, JobID: sampleJobID, Type: "job.created", CreatedAt: t0},
	}}}
	rec := getPath(historyRouter(fake), eventsPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	items := decodeMap(t, rec)["items"].([]any)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	if first["id"] != float64(7) || first["type"] != "attempt.started" || first["attempt_id"] != attempt.String() ||
		first["created_at"] != "2026-03-04T05:06:07Z" || first["data"].(map[string]any)["worker"] != "w1" {
		t.Errorf("first = %v", first)
	}
	if second["attempt_id"] != nil || second["data"] != nil {
		t.Errorf("second = %v", second)
	}
}

func TestListEvents_PassesCursorAndLimitAndReturnsNextCursor(t *testing.T) {
	fake := &fakeHistory{events: jobs.Page[eventsrepo.Event]{
		Items:   []eventsrepo.Event{{ID: 41, JobID: sampleJobID, Type: "job.created", CreatedAt: t0}},
		HasMore: true,
	}}
	rec := getPath(historyRouter(fake), eventsPath+"?limit=5&cursor="+encodeCursor(40))
	if fake.gotAfter != 40 || fake.gotLimit != 5 {
		t.Errorf("service got after=%d limit=%d", fake.gotAfter, fake.gotLimit)
	}
	if got := decodeMap(t, rec)["next_cursor"]; got != encodeCursor(41) {
		t.Errorf("next_cursor = %v, want %s", got, encodeCursor(41))
	}
}

func TestList_InvalidLimitReturns422(t *testing.T) {
	for _, path := range []string{attemptsPath, eventsPath} {
		for _, limit := range []string{"0", "-1", "201"} {
			t.Run(path[strings.LastIndex(path, "/")+1:]+"/limit="+limit, func(t *testing.T) {
				fake := &fakeHistory{}
				rec := getPath(historyRouter(fake), path+"?limit="+limit)
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
				}
				e := decodeError(t, rec).Error
				if e.Code != "validation_failed" || e.Details["limit"] == nil {
					t.Errorf("error = %+v", e)
				}
				if fake.listCalled {
					t.Error("service must not be called")
				}
			})
		}
	}
}

func TestList_LimitBoundsAreAccepted(t *testing.T) {
	for _, limit := range []string{"1", "200"} {
		fake := &fakeHistory{}
		rec := getPath(historyRouter(fake), attemptsPath+"?limit="+limit)
		if rec.Code != http.StatusOK || strconv.Itoa(fake.gotLimit) != limit {
			t.Errorf("limit %s: status %d, service limit %d", limit, rec.Code, fake.gotLimit)
		}
	}
}

func TestList_NonNumericLimitReturns400(t *testing.T) {
	rec := getPath(historyRouter(&fakeHistory{}), eventsPath+"?limit=abc")
	if rec.Code != http.StatusBadRequest || decodeError(t, rec).Error.Code != "bad_request" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestList_InvalidCursorReturns400(t *testing.T) {
	cursors := map[string]string{
		"not base64":    "!!!",
		"not a number":  base64.RawURLEncoding.EncodeToString([]byte("abc")),
		"negative":      encodeCursor(-1),
		"zero":          encodeCursor(0),
		"overflowing":   base64.RawURLEncoding.EncodeToString([]byte("99999999999999999999")),
		"padded base64": "MQ==",
	}
	for name, cur := range cursors {
		for _, path := range []string{attemptsPath, eventsPath} {
			t.Run(name, func(t *testing.T) {
				fake := &fakeHistory{}
				rec := getPath(historyRouter(fake), path+"?cursor="+cur)
				if rec.Code != http.StatusBadRequest || decodeError(t, rec).Error.Code != "bad_request" {
					t.Fatalf("got %d %s", rec.Code, rec.Body)
				}
				if fake.listCalled {
					t.Error("service must not be called")
				}
			})
		}
	}
}

func TestList_AttemptCursorTooBigForIntReturns400(t *testing.T) {
	rec := getPath(historyRouter(&fakeHistory{}), attemptsPath+"?cursor="+encodeCursor(1<<40))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestList_UnknownJobReturns404(t *testing.T) {
	for _, path := range []string{attemptsPath, eventsPath} {
		rec := getPath(historyRouter(&fakeHistory{err: db.ErrNotFound}), path)
		if rec.Code != http.StatusNotFound || decodeError(t, rec).Error.Code != "not_found" {
			t.Errorf("%s: got %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestList_UnexpectedErrorReturns500WithoutLeak(t *testing.T) {
	for _, path := range []string{attemptsPath, eventsPath} {
		rec := getPath(historyRouter(&fakeHistory{err: errors.New("pq: password=hunter2")}), path)
		if rec.Code != http.StatusInternalServerError || decodeError(t, rec).Error.Code != "internal" ||
			strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("%s: got %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestList_InvalidJobIDReturns400(t *testing.T) {
	for _, suffix := range []string{"attempts", "events"} {
		rec := getPath(historyRouter(&fakeHistory{}), "/api/v1/jobs/nope/"+suffix)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", suffix, rec.Code)
		}
	}
}

func TestListEvents_NonObjectDataReturns500(t *testing.T) {
	fake := &fakeHistory{events: jobs.Page[eventsrepo.Event]{Items: []eventsrepo.Event{
		{ID: 1, JobID: sampleJobID, Type: "x", Data: json.RawMessage(`[1]`), CreatedAt: t0},
	}}}
	rec := getPath(historyRouter(fake), eventsPath)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
}
