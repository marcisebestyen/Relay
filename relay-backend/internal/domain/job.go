package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Job is one unit of work submitted to Relay.
type Job struct {
	ID             uuid.UUID
	Name           string
	Handler        string
	Payload        json.RawMessage
	Queue          string
	Status         JobStatus
	Priority       int
	ScheduledAt    time.Time
	MaxAttempts    int
	TimeoutSeconds int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
	LastError      *string
}

// JobAttempt records one execution of a job.
type JobAttempt struct {
	ID            uuid.UUID
	JobID         uuid.UUID
	AttemptNumber int
	WorkerID      uuid.UUID
	Status        AttemptStatus
	StartedAt     time.Time
	FinishedAt    *time.Time
	Error         *string
	Result        json.RawMessage
}

// NewJobInput is the caller's request. Nil pointers and empty strings mean "use the default".
type NewJobInput struct {
	Name           string
	Handler        string
	Queue          string
	Payload        json.RawMessage
	Priority       *int
	MaxAttempts    *int
	TimeoutSeconds *int
	ScheduledAt    *time.Time
}

// Defaults are the values used for fields the caller left out.
type Defaults struct {
	Queue          string
	Priority       int
	MaxAttempts    int
	TimeoutSeconds int
}

// NewJob builds a pending job with defaults applied. On bad input it returns a
// zero Job and ValidationErrors listing every invalid field.
func NewJob(id uuid.UUID, in NewJobInput, d Defaults, now time.Time) (Job, error) {
	job := Job{
		ID:             id,
		Name:           in.Name,
		Handler:        in.Handler,
		Payload:        in.Payload,
		Queue:          strings.ToLower(strings.TrimSpace(in.Queue)),
		Status:         JobPending,
		Priority:       valueOr(in.Priority, d.Priority),
		ScheduledAt:    now,
		MaxAttempts:    valueOr(in.MaxAttempts, d.MaxAttempts),
		TimeoutSeconds: valueOr(in.TimeoutSeconds, d.TimeoutSeconds),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if job.Queue == "" {
		job.Queue = strings.ToLower(strings.TrimSpace(d.Queue))
	}
	if in.ScheduledAt != nil {
		job.ScheduledAt = *in.ScheduledAt
	}

	errs := ValidationErrors{}
	check := func(field string, msg string, ok bool) {
		if !ok {
			errs[field] = msg
		}
	}
	msg, ok := validateName(job.Name)
	check("name", msg, ok)
	msg, ok = validateHandler(job.Handler)
	check("handler", msg, ok)
	msg, ok = validateQueue(job.Queue)
	check("queue", msg, ok)
	msg, ok = validatePriority(job.Priority)
	check("priority", msg, ok)
	msg, ok = validateMaxAttempts(job.MaxAttempts)
	check("max_attempts", msg, ok)
	msg, ok = validateTimeoutSeconds(job.TimeoutSeconds)
	check("timeout_seconds", msg, ok)
	if len(errs) > 0 {
		return Job{}, errs
	}
	return job, nil
}

func valueOr(v *int, fallback int) int {
	if v != nil {
		return *v
	}
	return fallback
}
