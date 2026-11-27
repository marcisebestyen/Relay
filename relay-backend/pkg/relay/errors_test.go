package relay_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func TestNonRetryable_Nil_ReturnsNil(t *testing.T) {
	if got := relay.NonRetryable(nil); got != nil {
		t.Fatalf("NonRetryable(nil) = %v, want nil", got)
	}
}

func TestIsNonRetryable_WrappedNonRetryable_ReturnsTrue(t *testing.T) {
	base := errors.New("card declined")
	err := fmt.Errorf("charge: %w", relay.NonRetryable(base))

	if !relay.IsNonRetryable(err) {
		t.Fatal("wrapped non-retryable error not detected")
	}
}

func TestIsNonRetryable_DoubleWrapped_ReturnsTrue(t *testing.T) {
	err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", relay.NonRetryable(errors.New("bad"))))

	if !relay.IsNonRetryable(err) {
		t.Fatal("double-wrapped non-retryable error not detected")
	}
}

func TestIsNonRetryable_PlainError_ReturnsFalse(t *testing.T) {
	if relay.IsNonRetryable(errors.New("timeout")) {
		t.Fatal("plain error classified non-retryable")
	}
}

func TestIsNonRetryable_Nil_ReturnsFalse(t *testing.T) {
	if relay.IsNonRetryable(nil) {
		t.Fatal("nil classified non-retryable")
	}
}

func TestNonRetryable_PreservesOriginalError(t *testing.T) {
	base := errors.New("bad input")
	err := relay.NonRetryable(base)

	if !errors.Is(err, base) {
		t.Fatal("NonRetryable lost the original error chain")
	}
}

func TestNonRetryable_MessageMatchesOriginal(t *testing.T) {
	err := relay.NonRetryable(errors.New("bad input"))

	if err.Error() != "bad input" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "bad input")
	}
}
