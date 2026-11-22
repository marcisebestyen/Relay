package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

type errorBody struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func newRouter(t *testing.T, pinger api.Pinger, extra func(http.Handler) http.Handler) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := api.NewRouter(api.Deps{Logger: logger, Pinger: pinger, MaxBodyBytes: 16})
	if extra != nil {
		h = extra(h)
	}
	return h, &logs
}

func do(h http.Handler, method, path string, hdr map[string]string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	api.WriteError(rec, http.StatusBadRequest, api.CodeBadRequest, "nope", map[string]any{"field": "x"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Error.Code != "bad_request" || b.Error.Message != "nope" || b.Error.Details["field"] != "x" {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestWriteErrorOmitsNilDetailsAsEmptyObject(t *testing.T) {
	rec := httptest.NewRecorder()
	api.WriteError(rec, http.StatusNotFound, api.CodeNotFound, "gone", nil)
	if !strings.Contains(rec.Body.String(), `"details":{}`) {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestHealthOK(t *testing.T) {
	h, _ := newRouter(t, fakePinger{err: errors.New("db down")}, nil)
	rec := do(h, http.MethodGet, "/health", nil, nil)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestReadyOK(t *testing.T) {
	h, _ := newRouter(t, fakePinger{}, nil)
	rec := do(h, http.MethodGet, "/ready", nil, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestReadyReportsFailedDependency(t *testing.T) {
	h, _ := newRouter(t, fakePinger{err: errors.New("secret conn string")}, nil)
	rec := do(h, http.MethodGet, "/ready", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Error.Code != "unavailable" {
		t.Errorf("code = %q", b.Error.Code)
	}
	failed, _ := b.Error.Details["failed"].([]any)
	if len(failed) != 1 || failed[0] != "database" {
		t.Errorf("details = %v", b.Error.Details)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("leaked cause: %s", rec.Body)
	}
}

func TestUnknownRouteReturnsEnvelope404(t *testing.T) {
	h, _ := newRouter(t, fakePinger{}, nil)
	rec := do(h, http.MethodGet, "/nope", nil, nil)
	var b errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusNotFound || b.Error.Code != "not_found" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestWrongMethodReturnsEnvelope405(t *testing.T) {
	h, _ := newRouter(t, fakePinger{}, nil)
	rec := do(h, http.MethodPost, "/health", nil, nil)
	var b errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusMethodNotAllowed || b.Error.Code != "method_not_allowed" {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestRequestIDGeneratedWhenMissing(t *testing.T) {
	h, _ := newRouter(t, fakePinger{}, nil)
	a := do(h, http.MethodGet, "/health", nil, nil).Header().Get("X-Request-ID")
	b := do(h, http.MethodGet, "/health", nil, nil).Header().Get("X-Request-ID")
	if a == "" || a == b {
		t.Errorf("ids not unique/non-empty: %q %q", a, b)
	}
}

func TestRequestIDPropagatedWhenValid(t *testing.T) {
	h, logs := newRouter(t, fakePinger{}, nil)
	rec := do(h, http.MethodGet, "/health", map[string]string{"X-Request-ID": "abc-123"}, nil)
	if got := rec.Header().Get("X-Request-ID"); got != "abc-123" {
		t.Errorf("id = %q", got)
	}
	if !strings.Contains(logs.String(), `"request_id":"abc-123"`) {
		t.Errorf("log missing id: %s", logs)
	}
}

func TestRequestIDReplacedWhenUnsafe(t *testing.T) {
	h, _ := newRouter(t, fakePinger{}, nil)
	rec := do(h, http.MethodGet, "/health", map[string]string{"X-Request-ID": "bad id\x01"}, nil)
	if got := rec.Header().Get("X-Request-ID"); got == "" || strings.ContainsAny(got, " \x01") {
		t.Errorf("id = %q", got)
	}
}

func TestRequestIDOnErrorResponses(t *testing.T) {
	h, _ := newRouter(t, fakePinger{}, nil)
	if do(h, http.MethodGet, "/nope", nil, nil).Header().Get("X-Request-ID") == "" {
		t.Error("404 missing request id")
	}
}

func TestAccessLogFields(t *testing.T) {
	h, logs := newRouter(t, fakePinger{}, nil)
	do(h, http.MethodGet, "/health", nil, nil)

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &rec); err != nil {
		t.Fatalf("log not json: %v (%s)", err, logs)
	}
	for _, k := range []string{"request_id", "method", "path", "status", "duration_ms"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("log missing %q: %v", k, rec)
		}
	}
	if rec["method"] != "GET" || rec["path"] != "/health" || rec["status"] != float64(200) {
		t.Errorf("log = %v", rec)
	}
}

func TestPanicReturns500WithoutLeak(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := api.NewRouter(api.Deps{
		Logger: logger, Pinger: fakePinger{}, MaxBodyBytes: 16,
		Routes: func(r api.Router) {
			r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("kaboom-secret") })
		},
	})
	rec := do(h, http.MethodGet, "/boom", nil, nil)

	var b errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusInternalServerError || b.Error.Code != "internal" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "kaboom") || strings.Contains(rec.Body.String(), "goroutine") {
		t.Errorf("leaked: %s", rec.Body)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("missing request id")
	}
	if !strings.Contains(logs.String(), "kaboom-secret") || !strings.Contains(logs.String(), `"status":500`) {
		t.Errorf("panic/status not logged: %s", logs.String())
	}
}

func TestBodyLimitEnforced(t *testing.T) {
	var readErr error
	h := api.NewRouter(api.Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Pinger: fakePinger{}, MaxBodyBytes: 4,
		Routes: func(r api.Router) {
			r.Post("/echo", func(_ http.ResponseWriter, req *http.Request) {
				_, readErr = io.ReadAll(req.Body)
			})
		},
	})
	do(h, http.MethodPost, "/echo", nil, strings.NewReader("too long body"))
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Errorf("read err = %v", readErr)
	}
}

func TestOpenAPISpec_IsServed(t *testing.T) {
	h, _ := newRouter(t, fakePinger{}, nil)
	rec := do(h, http.MethodGet, "/api/v1/openapi.yaml", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "openapi: 3.") {
		t.Errorf("body does not start with an openapi version: %.40q", rec.Body.String())
	}
}
