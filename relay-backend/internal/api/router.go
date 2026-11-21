package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const readyTimeout = 2 * time.Second

// Router is the route registration surface handed to feature packages.
type Router = chi.Router

// Pinger checks that a dependency is reachable. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps holds what the router needs.
type Deps struct {
	Logger       *slog.Logger
	Pinger       Pinger
	MaxBodyBytes int64
	// Routes registers extra routes (used by later tickets). May be nil.
	Routes func(r Router)
}

// NewRouter builds the HTTP handler with middleware, health routes and
// envelope-shaped 404/405 responses.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, accessLog(d.Logger), recoverer(d.Logger), bodyLimit(d.MaxBodyBytes))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, CodeNotFound, "route not found", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed", nil)
	})

	r.Get("/health", health)
	r.Get("/ready", ready(d.Logger, d.Pinger))

	if d.Routes != nil {
		d.Routes(r)
	}
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func ready(logger *slog.Logger, db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			logger.WarnContext(r.Context(), "readiness check failed",
				"request_id", RequestIDFromContext(r.Context()), "dependency", "database", "error", err)
			WriteError(w, http.StatusServiceUnavailable, CodeUnavailable, "service not ready",
				map[string]any{"failed": []string{"database"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
