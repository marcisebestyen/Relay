// Command relay is the Relay CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/marcisebestyen/relay/relay-backend/internal/cli"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := cli.NewRootCmd(cli.BuildInfo{Version: version, Commit: commit, Date: date})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
