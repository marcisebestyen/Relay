package relay_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func noopHandler() relay.Handler {
	return relay.HandlerFunc(func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	})
}

func TestRegistry_RegisterThenLookup_ReturnsHandler(t *testing.T) {
	reg := relay.NewRegistry()
	h := noopHandler()

	if err := reg.Register("demo.echo", h); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := reg.Lookup("demo.echo")
	if !ok {
		t.Fatal("Lookup found no handler, want one")
	}
	if got == nil {
		t.Fatal("Lookup returned nil handler")
	}
}

func TestRegistry_LookupUnknownID_ReturnsFalse(t *testing.T) {
	reg := relay.NewRegistry()

	if _, ok := reg.Lookup("missing"); ok {
		t.Fatal("Lookup(missing) found a handler, want none")
	}
}

func TestRegistry_RegisterDuplicateID_ReturnsError(t *testing.T) {
	reg := relay.NewRegistry()
	if err := reg.Register("demo.echo", noopHandler()); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	if err := reg.Register("demo.echo", noopHandler()); err == nil {
		t.Fatal("second Register with same ID returned nil, want error")
	}
}

func TestRegistry_RegisterInvalidID_ReturnsError(t *testing.T) {
	reg := relay.NewRegistry()

	for _, id := range []string{"", "Bad.ID", "has space"} {
		if err := reg.Register(id, noopHandler()); err == nil {
			t.Errorf("Register(%q) returned nil, want error", id)
		}
	}
}

func TestRegistry_RegisterNilHandler_ReturnsError(t *testing.T) {
	reg := relay.NewRegistry()

	if err := reg.Register("demo.echo", nil); err == nil {
		t.Fatal("Register with nil handler returned nil, want error")
	}
}

func TestRegistry_IDs_ReturnsSortedIDs(t *testing.T) {
	reg := relay.NewRegistry()
	for _, id := range []string{"zeta", "alpha", "mid.one"} {
		if err := reg.Register(id, noopHandler()); err != nil {
			t.Fatalf("Register(%q): %v", id, err)
		}
	}

	want := []string{"alpha", "mid.one", "zeta"}
	if got := reg.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs() = %v, want %v", got, want)
	}
}

func TestRegistry_IDsEmpty_ReturnsEmptySlice(t *testing.T) {
	reg := relay.NewRegistry()

	if got := reg.IDs(); len(got) != 0 {
		t.Fatalf("IDs() = %v, want empty", got)
	}
}

func TestRegistry_RegisterErrorIsNotNonRetryable(t *testing.T) {
	reg := relay.NewRegistry()

	err := reg.Register("", noopHandler())
	if err == nil || relay.IsNonRetryable(err) {
		t.Fatalf("registration error must not be classified non-retryable: %v", err)
	}
}
