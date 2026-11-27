package workers_test

import (
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/workers"
)

func TestClassifyHealthyWhenHeartbeatIsFresh(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := workers.Classify(now, now, nil, 30*time.Second)
	if got != workers.StatusHealthy {
		t.Fatalf("status = %q, want %q", got, workers.StatusHealthy)
	}
}

func TestClassifyHealthyAtExactlyStaleAfter(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	last := now.Add(-30 * time.Second)
	got := workers.Classify(now, last, nil, 30*time.Second)
	if got != workers.StatusHealthy {
		t.Fatalf("status = %q, want %q", got, workers.StatusHealthy)
	}
}

func TestClassifyStaleJustAfterStaleAfter(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	last := now.Add(-30*time.Second - time.Nanosecond)
	got := workers.Classify(now, last, nil, 30*time.Second)
	if got != workers.StatusStale {
		t.Fatalf("status = %q, want %q", got, workers.StatusStale)
	}
}

func TestClassifyStoppedWhenStoppedAtIsSet(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	stopped := now.Add(-time.Second)
	got := workers.Classify(now, now, &stopped, 30*time.Second)
	if got != workers.StatusStopped {
		t.Fatalf("status = %q, want %q", got, workers.StatusStopped)
	}
}

func TestClassifyStoppedWinsOverStale(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	stopped := now.Add(-time.Minute)
	last := now.Add(-time.Hour)
	got := workers.Classify(now, last, &stopped, 30*time.Second)
	if got != workers.StatusStopped {
		t.Fatalf("status = %q, want %q", got, workers.StatusStopped)
	}
}
