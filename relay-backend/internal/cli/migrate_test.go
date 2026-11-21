package cli_test

import (
	"strings"
	"testing"
)

func TestMigrateCommandsFailWithoutDatabaseURL(t *testing.T) {
	for _, sub := range []string{"up", "down", "status"} {
		t.Run(sub, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("RELAY_DATABASE_URL", "")
			_, _, err := run(t, "migrate", sub)
			if err == nil || !strings.Contains(err.Error(), "RELAY_DATABASE_URL is required") {
				t.Fatalf("want missing URL error, got %v", err)
			}
		})
	}
}

func TestMigrateUpUnreachableDatabaseHidesPassword(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "postgres://relay:hunter2-secret@127.0.0.1:1/relay?sslmode=disable")
	_, stderr, err := run(t, "migrate", "up")
	if err == nil {
		t.Fatal("expected connection error")
	}
	if strings.Contains(err.Error(), "hunter2-secret") || strings.Contains(stderr, "hunter2-secret") {
		t.Errorf("password leaked: err=%v stderr=%q", err, stderr)
	}
}

func TestMigrateRequiresSubcommand(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RELAY_DATABASE_URL", "postgres://x")
	if _, _, err := run(t, "migrate", "sideways"); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}
