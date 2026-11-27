package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func registryWithHandler(t *testing.T) *relay.Registry {
	t.Helper()
	reg := relay.NewRegistry()
	err := reg.Register("demo.echo", relay.HandlerFunc(func(_ context.Context, p json.RawMessage) (json.RawMessage, error) {
		return p, nil
	}))
	if err != nil {
		t.Fatalf("register handler: %v", err)
	}
	return reg
}

func TestWorkerRefusesToStartWithoutHandlers(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "postgres://x")
	_, _, err := run(t, "worker")
	if err == nil || !strings.Contains(err.Error(), "no handlers registered") {
		t.Fatalf("want no-handlers error, got %v", err)
	}
}

func TestStartRefusesToStartWithoutHandlers(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "postgres://x")
	_, _, err := run(t, "start")
	if err == nil || !strings.Contains(err.Error(), "no handlers registered") {
		t.Fatalf("want no-handlers error, got %v", err)
	}
}

func TestWorkerFlagsOverrideEnvAndAreValidated(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "postgres://x")
	t.Setenv("RELAY_WORKER_CONCURRENCY", "4")
	cases := map[string]struct {
		args []string
		want string
	}{
		"zero concurrency flag": {[]string{"--concurrency=0"}, "RELAY_WORKER_CONCURRENCY must be >= 1"},
		"empty queues flag":     {[]string{"--queues="}, "RELAY_WORKER_QUEUES needs at least one queue"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := runWith(t, registryWithHandler(t), append([]string{"worker"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestWorkerEnvConcurrencyIsValidated(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "postgres://x")
	t.Setenv("RELAY_WORKER_CONCURRENCY", "0")
	_, _, err := runWith(t, registryWithHandler(t), "worker")
	if err == nil || !strings.Contains(err.Error(), "RELAY_WORKER_CONCURRENCY must be >= 1") {
		t.Fatalf("want env concurrency error, got %v", err)
	}
}

func TestWorkerAndStartAreListed(t *testing.T) {
	root := newRoot(t)
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
