package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/migrate"
	"github.com/marcisebestyen/relay/relay-backend/migrations"
)

const poolCloseTimeout = 10 * time.Second

// newMigrateCmd builds `relay migrate`. cfg is filled before any subcommand runs.
func newMigrateCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage database migrations",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		migrateSubcommand(cfg, "up", "Apply all pending migrations", runUp),
		migrateSubcommand(cfg, "down", "Roll back the most recent migration", runDown),
		migrateSubcommand(cfg, "status", "List migrations and whether they are applied", runStatus),
	)
	return cmd
}

type migrateAction func(ctx context.Context, m *migrate.Migrator, out io.Writer) error

func migrateSubcommand(cfg *config.Config, use, short string, action migrateAction) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			ctx := cmd.Context()
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

			m, err := migrate.New(pool, migrations.FS)
			if err != nil {
				return err
			}
			return action(ctx, m, cmd.OutOrStdout())
		},
	}
	// Migrations only need the database URL; the API role checks just that plus HTTP defaults.
	SetRole(cmd, config.RoleAPI)
	return cmd
}

func runUp(ctx context.Context, m *migrate.Migrator, out io.Writer) error {
	applied, err := m.Up(ctx)
	for _, r := range applied {
		fmt.Fprintf(out, "applied %s\n", r.Source)
	}
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Fprintln(out, "no pending migrations")
	}
	return nil
}

func runDown(ctx context.Context, m *migrate.Migrator, out io.Writer) error {
	res, err := m.Down(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "rolled back %s\n", res.Source)
	return nil
}

func runStatus(ctx context.Context, m *migrate.Migrator, out io.Writer) error {
	list, err := m.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range list {
		state := "pending"
		if s.Applied {
			state = "applied " + s.AppliedAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(out, "%05d  %-30s  %s\n", s.Version, s.Source, state)
	}
	return nil
}
