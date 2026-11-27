package domain_test

import (
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

func TestValidateHandlerID_AcceptsValidIDs(t *testing.T) {
	valid := []string{"a", "demo.echo", "email-send_v2", "0x1", strings.Repeat("a", 128)}
	for _, id := range valid {
		if err := domain.ValidateHandlerID(id); err != nil {
			t.Errorf("ValidateHandlerID(%q) = %v, want nil", id, err)
		}
	}
}

func TestValidateHandlerID_RejectsInvalidIDs(t *testing.T) {
	invalid := []string{"", "Demo.echo", ".leading", "-leading", "has space", "a/b", strings.Repeat("a", 129)}
	for _, id := range invalid {
		if err := domain.ValidateHandlerID(id); err == nil {
			t.Errorf("ValidateHandlerID(%q) = nil, want error", id)
		}
	}
}
