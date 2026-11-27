package workers_test

import (
	"os"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
)

func TestNewIdentityFillsProcessFields(t *testing.T) {
	id, err := workers.NewIdentity("1.2.3")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname: %v", err)
	}
	if id.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Error("ID is the nil UUID")
	}
	if id.Hostname != host {
		t.Errorf("Hostname = %q, want %q", id.Hostname, host)
	}
	if id.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", id.PID, os.Getpid())
	}
	if id.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", id.Version)
	}
}

func TestNewIdentityGivesNewIDEachCall(t *testing.T) {
	a, err := workers.NewIdentity("dev")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	b, err := workers.NewIdentity("dev")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("two identities share ID %s", a.ID)
	}
}
