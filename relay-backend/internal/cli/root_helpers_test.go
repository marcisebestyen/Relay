package cli_test

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/cli"
	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func newRoot(t *testing.T) *cobra.Command {
	t.Helper()
	return cli.NewRootCmd(cli.BuildInfo{}, relay.NewRegistry())
}
