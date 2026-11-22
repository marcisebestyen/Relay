package domain_test

import (
	"errors"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

var allJobStatuses = []domain.JobStatus{
	domain.JobPending, domain.JobRunning, domain.JobCompleted,
	domain.JobFailed, domain.JobCancelled, domain.JobStatus("unknown"),
}

func TestTransitionMatrix(t *testing.T) {
	allowed := map[[2]domain.JobStatus]bool{
		{domain.JobPending, domain.JobRunning}:   true,
		{domain.JobPending, domain.JobCancelled}: true,
		{domain.JobRunning, domain.JobCompleted}: true,
		{domain.JobRunning, domain.JobPending}:   true,
		{domain.JobRunning, domain.JobFailed}:    true,
		{domain.JobRunning, domain.JobCancelled}: true,
		{domain.JobFailed, domain.JobPending}:    true,
	}
	for _, from := range allJobStatuses {
		for _, to := range allJobStatuses {
			want := allowed[[2]domain.JobStatus{from, to}]
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				if got := domain.CanTransition(from, to); got != want {
					t.Errorf("CanTransition = %v, want %v", got, want)
				}
				err := domain.Transition(from, to)
				if want && err != nil {
					t.Errorf("Transition error: %v", err)
				}
				if !want {
					var invalid domain.ErrInvalidTransition
					if !errors.As(err, &invalid) {
						t.Fatalf("want ErrInvalidTransition, got %v", err)
					}
					if invalid.From != from || invalid.To != to {
						t.Errorf("error carries %s->%s, want %s->%s", invalid.From, invalid.To, from, to)
					}
				}
			})
		}
	}
}

func TestJobStatusHelpers(t *testing.T) {
	tests := []struct {
		status                         domain.JobStatus
		terminal, cancellable, retried bool
	}{
		{domain.JobPending, false, true, false},
		{domain.JobRunning, false, true, false},
		{domain.JobCompleted, true, false, false},
		{domain.JobFailed, false, false, true},
		{domain.JobCancelled, true, false, false},
		{domain.JobStatus("unknown"), false, false, false},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := tc.status.IsTerminal(); got != tc.terminal {
				t.Errorf("IsTerminal = %v, want %v", got, tc.terminal)
			}
			if got := tc.status.IsCancellable(); got != tc.cancellable {
				t.Errorf("IsCancellable = %v, want %v", got, tc.cancellable)
			}
			if got := tc.status.IsRetryable(); got != tc.retried {
				t.Errorf("IsRetryable = %v, want %v", got, tc.retried)
			}
		})
	}
}

func TestErrInvalidTransitionMessage(t *testing.T) {
	err := domain.Transition(domain.JobCompleted, domain.JobRunning)
	want := "invalid job status transition: completed -> running"
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestAttemptStatusValues(t *testing.T) {
	want := map[domain.AttemptStatus]string{
		domain.AttemptRunning:   "running",
		domain.AttemptCompleted: "completed",
		domain.AttemptFailed:    "failed",
		domain.AttemptCancelled: "cancelled",
		domain.AttemptTimedOut:  "timed_out",
		domain.AttemptLost:      "lost",
	}
	for s, v := range want {
		if string(s) != v {
			t.Errorf("got %q, want %q", s, v)
		}
	}
}
