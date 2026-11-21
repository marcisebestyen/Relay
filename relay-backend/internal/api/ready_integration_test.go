//go:build integration

package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func realRouter(p api.Pinger) http.Handler {
	return api.NewRouter(api.Deps{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Pinger: p, MaxBodyBytes: 1024})
}

func TestReadyWithRealDatabase(t *testing.T) {
	pool := pgtest.OpenPool(t, pgtest.NewDatabase(t))
	rec := do(realRouter(pool), http.MethodGet, "/ready", nil, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}

func TestReadyReturns503WhenPoolClosed(t *testing.T) {
	pool := pgtest.OpenPool(t, pgtest.NewDatabase(t))
	pool.Close()
	rec := do(realRouter(pool), http.MethodGet, "/ready", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}
