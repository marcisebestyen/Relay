package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers/runtime"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

// newStartCmd builds `relay start`, which runs the API and a worker in one process.
func newStartCmd(cfg *config.Config, logger **slog.Logger, reg *relay.Registry, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Run the HTTP API and job workers in one process",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if len(reg.IDs()) == 0 {
				return runtime.ErrNoHandlers
			}
			l, err := net.Listen("tcp", cfg.HTTP.Addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr, err)
			}
			return runStart(ctx, *cfg, reg, *logger, version, l)
		},
	}
	SetRole(cmd, config.RoleCombined)
	return cmd
}

// runStart serves the API on l and runs a worker, sharing one pool. When either
// stops, the other is stopped too.
func runStart(ctx context.Context, cfg config.Config, reg *relay.Registry, logger *slog.Logger, version string, l net.Listener) (err error) {
	identity, err := workers.NewIdentity(version)
	if err != nil {
		_ = l.Close()
		return err
	}
	pool, err := db.Open(ctx, cfg.Database)
	if err != nil {
		_ = l.Close()
		return err
	}
	defer closePool(ctx, pool, &err)

	rt, err := newRuntime(cfg, reg, logger, pool, identity)
	if err != nil {
		_ = l.Close()
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	api.LogAuthStatus(logger, cfg.Auth.Token, cfg.HTTP.Addr)
	errc := make(chan error, 2)
	go func() { errc <- api.Serve(runCtx, l, apiHandler(pool, cfg, logger), cfg.Shutdown.Timeout, logger) }()
	go func() { errc <- rt.Run(runCtx) }()

	var errs []error
	for range 2 {
		if e := <-errc; e != nil {
			errs = append(errs, e)
		}
		cancel()
	}
	return errors.Join(errs...)
}
