//go:build integration

package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestAuth_JobsRoutesWithRealDB(t *testing.T) {
	pool := pgtest.NewPool(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := api.NewRouter(api.Deps{
		Logger: logger, Pinger: pool, MaxBodyBytes: 4096, AuthToken: testToken,
		Routes: api.NewJobsHandler(jobs.NewService(pool, jobDefaults, nil, nil), logger).Register,
	})
	body := `{"handler":"demo.echo"}`

	assertUnauthorized(t, postJob(h, body))

	hdr := bearer(testToken)
	hdr["Content-Type"] = "application/json"
	rec := do(h, http.MethodPost, "/api/v1/jobs", hdr, strings.NewReader(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodGet, rec.Header().Get("Location"), bearer(testToken), nil); rec.Code != http.StatusOK {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodGet, "/ready", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("ready = %d", rec.Code)
	}
}
