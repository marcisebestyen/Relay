// Package relay is the public API for writing and registering Relay job handlers.
package relay

import (
	"context"
	"encoding/json"
	"fmt"
)

// Handler runs one job. The payload is the job's JSON payload. The returned
// JSON is the job's result.
type Handler interface {
	Handle(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// HandlerFunc lets an ordinary function be used as a Handler.
type HandlerFunc func(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)

// Handle calls f.
func (f HandlerFunc) Handle(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	return f(ctx, payload)
}

// DecodeJSON decodes payload into a T. A bad payload returns an error that
// IsNonRetryable reports as true, because retrying will not fix bad input.
func DecodeJSON[T any](payload json.RawMessage) (T, error) {
	var zero T
	var v T
	if err := json.Unmarshal(payload, &v); err != nil {
		return zero, NonRetryable(fmt.Errorf("decode payload: %w", err))
	}
	return v, nil
}
