package cli_test

import (
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
	relaycli "github.com/marcisebestyen/relay/relay-backend/pkg/relay/cli"
)

func TestNewRootCommandBuildsRelayCommandTree(t *testing.T) {
	root := relaycli.NewRootCommand(relay.NewRegistry(), relaycli.BuildInfo{Version: "1.0.0"})
	if root.Use != "relay" {
		t.Errorf("root use = %q, want relay", root.Use)
	}
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"worker", "start", "api", "migrate", "version"} {
		if !names[want] {
			t.Errorf("missing subcommand %q", want)
		}
	}
}
