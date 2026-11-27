package relay_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func TestHandlerFunc_Handle_CallsFunction(t *testing.T) {
	in := json.RawMessage(`{"n":1}`)
	out := json.RawMessage(`{"ok":true}`)
	var gotPayload json.RawMessage

	h := relay.HandlerFunc(func(_ context.Context, p json.RawMessage) (json.RawMessage, error) {
		gotPayload = p
		return out, nil
	})

	got, err := h.Handle(context.Background(), in)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if string(got) != string(out) {
		t.Errorf("result = %s, want %s", got, out)
	}
	if string(gotPayload) != string(in) {
		t.Errorf("payload = %s, want %s", gotPayload, in)
	}
}

type echoInput struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestDecodeJSON_ValidPayload_ReturnsValue(t *testing.T) {
	got, err := relay.DecodeJSON[echoInput](json.RawMessage(`{"name":"x","n":2}`))
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if got.Name != "x" || got.N != 2 {
		t.Fatalf("got %+v, want {Name:x N:2}", got)
	}
}

func TestDecodeJSON_BadPayload_ReturnsNonRetryableError(t *testing.T) {
	cases := map[string]json.RawMessage{
		"malformed":  json.RawMessage(`{"name":`),
		"wrong type": json.RawMessage(`{"n":"two"}`),
		"empty":      json.RawMessage(``),
		"nil":        nil,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := relay.DecodeJSON[echoInput](payload)
			if err == nil {
				t.Fatal("DecodeJSON returned nil error, want error")
			}
			if !relay.IsNonRetryable(err) {
				t.Fatalf("bad payload error not classified non-retryable: %v", err)
			}
		})
	}
}

func TestDecodeJSON_BadPayload_KeepsUnderlyingCause(t *testing.T) {
	_, err := relay.DecodeJSON[echoInput](json.RawMessage(`{"n":"two"}`))

	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("want *json.UnmarshalTypeError in chain, got %v", err)
	}
}
