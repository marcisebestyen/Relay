package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// newAPICmd builds `relay api`. cfg and logger are filled before it runs.
func newAPICmd(cfg *config.Config, logger **slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Run the HTTP API server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runAPI(ctx, *cfg, *logger)
		},
	}
	SetRole(cmd, config.RoleAPI)
	return cmd
}

func runAPI(ctx context.Context, cfg config.Config, logger *slog.Logger) (err error) {
	pool, err := db.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer closePool(ctx, pool, &err)

	l, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr, err)
	}

	api.LogAuthStatus(logger, cfg.Auth.Token, cfg.HTTP.Addr)
	return api.Serve(ctx, l, apiHandler(pool, cfg, logger), cfg.Shutdown.Timeout, logger)
}

// apiHandler builds the HTTP handler for the job API on pool.
func apiHandler(pool *pgxpool.Pool, cfg config.Config, logger *slog.Logger) http.Handler {
	svc := jobs.NewService(pool, jobDefaults(cfg), nil, nil)
	return api.NewRouter(api.Deps{
		Logger:       logger,
		Pinger:       pool,
		MaxBodyBytes: cfg.Limits.MaxPayloadBytes,
		AuthToken:    cfg.Auth.Token,
		Routes:       api.NewJobsHandler(svc, logger).Register,
	})
}

// closePool closes pool and reports a close error only when err is nil.
func closePool(ctx context.Context, pool *pgxpool.Pool, err *error) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), poolCloseTimeout)
	defer cancel()
	if closeErr := db.Close(closeCtx, pool); *err == nil {
		*err = closeErr
	}
}

// Queue and priority have no env setting; the rest come from config.
const (
	defaultQueue    = "default"
	defaultPriority = 50
)

func jobDefaults(cfg config.Config) domain.Defaults {
	return domain.Defaults{
		Queue:          defaultQueue,
		Priority:       defaultPriority,
		MaxAttempts:    cfg.Retry.DefaultMaxAttempts,
		TimeoutSeconds: int(cfg.Timeouts.Default.Seconds()),
	}
}
