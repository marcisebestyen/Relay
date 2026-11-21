// Package cli defines the relay command tree.
package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/observability/logging"
)

const (
	roleAnnotation = "relay.role"
	dotEnvPath     = ".env"
	logLevelFlag   = "log-level"
)

// SetRole marks cmd as needing validated config for role. Commands without a
// role (like version) skip validation.
func SetRole(cmd *cobra.Command, role config.Role) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[roleAnnotation] = string(role)
}

// loadConfig reads config once and validates it when cmd declares a role.
func loadConfig(cmd *cobra.Command) (config.Config, error) {
	cfg, err := config.LoadFromEnv(dotEnvPath)
	if err != nil {
		return cfg, err
	}
	if name, ok := cmd.Annotations[roleAnnotation]; ok {
		role, err := config.ParseRole(name)
		if err != nil {
			return cfg, err
		}
		if err := cfg.Validate(role); err != nil {
			return cfg, fmt.Errorf("invalid configuration:\n%w", err)
		}
	}
	return cfg, nil
}

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
	var cfg config.Config

	root := &cobra.Command{
		Use:           "relay",
		Short:         "Relay background job orchestration platform",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if cfg, err = loadConfig(cmd); err != nil {
				return err
			}
			if !cmd.Root().PersistentFlags().Changed(logLevelFlag) {
				logLevel = cfg.Telemetry.LogLevel
			}
			level, err := logging.ParseLevel(logLevel)
			if err != nil {
				return err
			}
			logger = logging.NewLogger(level, cmd.ErrOrStderr())
			return nil
		},
	}
	root.PersistentFlags().StringVar(&logLevel, logLevelFlag, "info", "log level: debug, info, warn, error (overrides RELAY_LOG_LEVEL)")

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

	root.AddCommand(newMigrateCmd(&cfg), newAPICmd(&cfg, &logger))

	return root
}
