package relay

import (
	"fmt"
	"sort"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

// Registry maps handler IDs to handlers. Register at startup, before workers run.
// Register is not safe for concurrent use. Lookup and IDs are safe to call
// once registration is done.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register adds h under id. It fails when id breaks the handler ID rule,
// when h is nil, or when id is already registered. Callers should treat the
// error as fatal at startup.
func (r *Registry) Register(id string, h Handler) error {
	if err := domain.ValidateHandlerID(id); err != nil {
		return fmt.Errorf("register handler %q: %w", id, err)
	}
	if h == nil {
		return fmt.Errorf("register handler %q: handler is nil", id)
	}
	if _, exists := r.handlers[id]; exists {
		return fmt.Errorf("register handler %q: already registered", id)
	}
	r.handlers[id] = h
	return nil
}

// Lookup returns the handler for id, and false when none is registered.
func (r *Registry) Lookup(id string) (Handler, bool) {
	h, ok := r.handlers[id]
	return h, ok
}

// IDs returns all registered handler IDs in sorted order.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.handlers))
	for id := range r.handlers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
