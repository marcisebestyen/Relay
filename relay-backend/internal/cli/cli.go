// Package cli defines the relay command tree.
package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/observability/logging"
)

// BuildInfo is set at build time via -ldflags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// NewRootCmd builds the `relay` root command.
func NewRootCmd(info BuildInfo) *cobra.Command {
	var logLevel string
	var logger *slog.Logger

	root := &cobra.Command{
		Use:           "relay",
		Short:         "Relay background job orchestration platform",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			level, err := logging.ParseLevel(logLevel)
			if err != nil {
				return err
			}
			logger = logging.NewLogger(level, cmd.ErrOrStderr())
			return nil
		},
	}
	root.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn, error")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version, commit and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger.Debug("printing version", "version", info.Version)
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "relay %s (commit %s, built %s)\n", info.Version, info.Commit, info.Date)
			return err
		},
	})

	return root
}
