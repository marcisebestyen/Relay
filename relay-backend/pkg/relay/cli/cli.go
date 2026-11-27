// Package cli builds the relay command tree for an application. An application
// registers its own handlers and gets a binary that can run them.
package cli

import (
	"github.com/spf13/cobra"

	internalcli "github.com/marcisebestyen/relay/relay-backend/internal/cli"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

// BuildInfo is set at build time via -ldflags.
type BuildInfo = internalcli.BuildInfo

// NewRootCommand builds the `relay` root command. registry holds the handlers
// this binary runs.
func NewRootCommand(registry *relay.Registry, info BuildInfo) *cobra.Command {
	return internalcli.NewRootCmd(info, registry)
}
