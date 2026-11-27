package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/finalize"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
	"github.com/marcisebestyen/relay/relay-backend/internal/workers/runtime"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

// newWorkerCmd builds `relay worker`. Its flags override the RELAY_WORKER_* env values.
func newWorkerCmd(cfg *config.Config, logger **slog.Logger, reg *relay.Registry, version string) *cobra.Command {
	var queues []string
	var concurrency int
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Run job workers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			wcfg := *cfg
			applyWorkerOverrides(&wcfg, queues, concurrency, cmd.Flags().Changed("queues"), cmd.Flags().Changed("concurrency"))
			if err := wcfg.Validate(config.RoleWorker); err != nil {
				return fmt.Errorf("invalid configuration:\n%w", err)
			}
			return runWorker(ctx, wcfg, reg, *logger, version)
		},
	}
	cmd.Flags().StringSliceVar(&queues, "queues", nil, "queues to run, comma separated (overrides RELAY_WORKER_QUEUES)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, "execution slots (overrides RELAY_WORKER_CONCURRENCY)")
	SetRole(cmd, config.RoleWorker)
	return cmd
}

// applyWorkerOverrides copies flag values onto cfg. A flag the user did not set keeps its env value.
func applyWorkerOverrides(cfg *config.Config, queues []string, concurrency int, queuesSet, concurrencySet bool) {
	if queuesSet {
		cfg.Worker.Queues = queues
	}
	if concurrencySet {
		cfg.Worker.Concurrency = concurrency
	}
}

func runWorker(ctx context.Context, cfg config.Config, reg *relay.Registry, logger *slog.Logger, version string) (err error) {
	if len(reg.IDs()) == 0 {
		return runtime.ErrNoHandlers
	}
	identity, err := workers.NewIdentity(version)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer closePool(ctx, pool, &err)

	rt, err := newRuntime(cfg, reg, logger, pool, identity)
	if err != nil {
		return err
	}
	return rt.Run(ctx)
}

// newRuntime wires the worker runtime to the claimer, finalizer and lifecycle on pool.
func newRuntime(cfg config.Config, reg *relay.Registry, logger *slog.Logger, pool *pgxpool.Pool, identity workers.Identity) (*runtime.Runtime, error) {
	claimer, err := claim.NewClaimer(pool, cfg.Lease.Duration)
	if err != nil {
		return nil, err
	}
	finalizer, err := finalize.New(pool, cfg.Limits.MaxResultBytes)
	if err != nil {
		return nil, err
	}
	return runtime.New(runtime.Config{
		WorkerID:          identity.ID,
		Queues:            cfg.Worker.Queues,
		Registry:          reg,
		Concurrency:       cfg.Worker.Concurrency,
		PollInterval:      cfg.Worker.PollInterval,
		HeartbeatInterval: cfg.Lease.HeartbeatInterval,
		ShutdownTimeout:   cfg.Shutdown.Timeout,
		Logger:            logger,
		Claimer:           claimer,
		Finalizer:         finalizer,
		Lifecycle:         runtime.NewLifecycle(pool, identity),
	})
}
