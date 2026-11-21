package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/cli"
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
