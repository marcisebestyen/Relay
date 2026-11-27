package claim

import (
	"testing"
	"time"
)

func TestNewClaimerRejectsNonPositiveLease(t *testing.T) {
	if _, err := NewClaimer(nil, 0); err == nil {
		t.Fatal("NewClaimer with zero lease: want error, got nil")
	}
	if _, err := NewClaimer(nil, -time.Second); err == nil {
		t.Fatal("NewClaimer with negative lease: want error, got nil")
	}
}

func TestNewClaimerRejectsNilPool(t *testing.T) {
	if _, err := NewClaimer(nil, time.Second); err == nil {
		t.Fatal("NewClaimer with nil pool: want error, got nil")
	}
}

func TestDeadlineIsNilWithoutTimeout(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := deadlineFor(start, 0); got != nil {
		t.Fatalf("deadlineFor(start, 0) = %v, want nil", got)
	}
}

func TestDeadlineAddsTimeoutToStart(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := deadlineFor(start, 90)
	want := start.Add(90 * time.Second)
	if got == nil || !got.Equal(want) {
		t.Fatalf("deadlineFor(start, 90) = %v, want %v", got, want)
	}
}
