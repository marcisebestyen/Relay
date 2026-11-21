//go:build integration

package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/testutil/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

func TestMigrateUpStatusDownAgainstDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", pgtest.NewDatabase(t))

	stdout, _, err := run(t, "migrate", "status")
	if err != nil || !strings.Contains(stdout, "pending") {
		t.Fatalf("status before up: out=%q err=%v", stdout, err)
	}

	stdout, _, err = run(t, "migrate", "up")
	if err != nil || !strings.Contains(stdout, "00001_baseline.sql") {
		t.Fatalf("up: out=%q err=%v", stdout, err)
	}

	stdout, _, err = run(t, "migrate", "status")
	if err != nil || !strings.Contains(stdout, "applied") || strings.Contains(stdout, "pending") {
		t.Fatalf("status after up: out=%q err=%v", stdout, err)
	}

	stdout, _, err = run(t, "migrate", "up")
	if err != nil || !strings.Contains(stdout, "no pending migrations") {
		t.Fatalf("second up: out=%q err=%v", stdout, err)
	}

	stdout, _, err = run(t, "migrate", "down")
	if err != nil || !strings.Contains(stdout, "00001_baseline.sql") {
		t.Fatalf("down: out=%q err=%v", stdout, err)
	}
}
