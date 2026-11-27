// Command relay is the Relay CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
	relaycli "github.com/marcisebestyen/relay/relay-backend/pkg/relay/cli"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// No handlers are built into this binary yet, so worker and start refuse to run.
	root := relaycli.NewRootCommand(relay.NewRegistry(), relaycli.BuildInfo{Version: version, Commit: commit, Date: date})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
