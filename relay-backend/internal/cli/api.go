package cli

import (
	"context"
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
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), poolCloseTimeout)
		defer cancel()
		if closeErr := db.Close(closeCtx, pool); err == nil {
			err = closeErr
		}
	}()

	l, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr, err)
	}

	handler := api.NewRouter(api.Deps{
		Logger:       logger,
		Pinger:       pool,
		MaxBodyBytes: cfg.Limits.MaxPayloadBytes,
	})
	return api.Serve(ctx, l, handler, cfg.Shutdown.Timeout, logger)
}
