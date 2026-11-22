// Package domain holds Relay's pure job model: types, state machine and validation.
package domain

import "fmt"

// JobStatus is the lifecycle state of a job.
type JobStatus string

// Job lifecycle states.
const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// AttemptStatus is the state or outcome of one execution attempt.
type AttemptStatus string

// Attempt states and outcomes.
const (
	AttemptRunning   AttemptStatus = "running"
	AttemptCompleted AttemptStatus = "completed"
	AttemptFailed    AttemptStatus = "failed"
	AttemptCancelled AttemptStatus = "cancelled"
	AttemptTimedOut  AttemptStatus = "timed_out"
	AttemptLost      AttemptStatus = "lost"
)

var transitions = map[JobStatus]map[JobStatus]bool{
	JobPending: {JobRunning: true, JobCancelled: true},
	JobRunning: {JobCompleted: true, JobPending: true, JobFailed: true, JobCancelled: true},
	JobFailed:  {JobPending: true},
}

// CanTransition reports whether a job may move from one status to another.
func CanTransition(from, to JobStatus) bool {
	return transitions[from][to]
}

// ErrInvalidTransition is returned for a status change the state machine forbids.
type ErrInvalidTransition struct {
	From, To JobStatus
}

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid job status transition: %s -> %s", e.From, e.To)
}

// Transition returns nil if the change is allowed, else ErrInvalidTransition.
func Transition(from, to JobStatus) error {
	if !CanTransition(from, to) {
		return ErrInvalidTransition{From: from, To: to}
	}
	return nil
}

// IsTerminal reports whether no further change is possible. Failed is not
// terminal because a manual retry can move it back to pending.
func (s JobStatus) IsTerminal() bool {
	return s == JobCompleted || s == JobCancelled
}

// IsCancellable reports whether a cancel request is accepted.
func (s JobStatus) IsCancellable() bool {
	return s == JobPending || s == JobRunning
}

// IsRetryable reports whether a manual retry is accepted.
func (s JobStatus) IsRetryable() bool {
	return s == JobFailed
}
