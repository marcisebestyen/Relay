package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/marcisebestyen/relay/relay-backend/internal/cli"
	"github.com/marcisebestyen/relay/relay-backend/internal/config"
)

func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := cli.NewRootCmd(cli.BuildInfo{Version: "1.2.3", Commit: "abc123", Date: "2026-01-02"})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestVersionPrintsBuildInfo(t *testing.T) {
	stdout, _, err := run(t, "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"1.2.3", "abc123", "2026-01-02"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output %q missing %q", stdout, want)
		}
	}
}

func TestDebugLevelLogsJSONToStderr(t *testing.T) {
	stdout, stderr, err := run(t, "--log-level=debug", "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(stdout, `"level"`) {
		t.Errorf("logs leaked to stdout: %q", stdout)
	}
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(stderr), "\n")[0])
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("stderr is not JSON: %v (%q)", err, stderr)
	}
	if rec["level"] != "DEBUG" {
		t.Errorf("unexpected record: %v", rec)
	}
}

func TestInvalidLogLevelFails(t *testing.T) {
	_, _, err := run(t, "--log-level=nope", "version")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the bad value: %v", err)
	}
}

func runWithRole(t *testing.T, role config.Role, args ...string) (stderr string, err error) {
	t.Helper()
	var errOut bytes.Buffer
	cmd := cli.NewRootCmd(cli.BuildInfo{})
	probe := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
	cli.SetRole(probe, role)
	cmd.AddCommand(probe)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"probe"}, args...))
	err = cmd.Execute()
	return errOut.String(), err
}

func TestRoleCommandFailsWithoutDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "")
	_, err := runWithRole(t, config.RoleAPI)
	if err == nil || !strings.Contains(err.Error(), "RELAY_DATABASE_URL is required") {
		t.Fatalf("want missing URL error, got %v", err)
	}
}

func TestRoleCommandPassesWithDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "postgres://x")
	if _, err := runWithRole(t, config.RoleCombined); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRoleCommandReadsDotEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("RELAY_DATABASE_URL", "")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("RELAY_DATABASE_URL=postgres://from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runWithRole(t, config.RoleAPI); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVersionNeedsNoDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "")
	if _, _, err := run(t, "version"); err != nil {
		t.Fatalf("version must not need config: %v", err)
	}
}

func TestEnvLogLevelApplies(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_LOG_LEVEL", "debug")
	_, stderr, err := run(t, "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr, `"level":"DEBUG"`) {
		t.Errorf("RELAY_LOG_LEVEL=debug not applied: %q", stderr)
	}
}

func TestLogLevelFlagBeatsEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_LOG_LEVEL", "debug")
	_, stderr, err := run(t, "--log-level=error", "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr != "" {
		t.Errorf("flag should silence debug logs: %q", stderr)
	}
}

func TestInvalidEnvLogLevelFails(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_LOG_LEVEL", "loud")
	if _, _, err := run(t, "version"); err == nil {
		t.Fatal("expected error")
	}
}
