package api

import (
	"github.com/marcisebestyen/relay/relay-backend/internal/api/gen"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

// newJobInput maps the request body to the domain input. Times are made UTC.
func newJobInput(b createJobBody) domain.NewJobInput {
	in := domain.NewJobInput{
		Handler:        b.Handler,
		Payload:        b.Payload,
		Priority:       b.Priority,
		MaxAttempts:    b.MaxAttempts,
		TimeoutSeconds: b.TimeoutSeconds,
	}
	if b.Name != nil {
		in.Name = *b.Name
	}
	if b.Queue != nil {
		in.Queue = *b.Queue
	}
	if b.ScheduledAt != nil {
		t := b.ScheduledAt.UTC()
		in.ScheduledAt = &t
	}
	return in
}

// toJobDTO maps a domain job to the response shape. Times are UTC and an
// empty name is left out.
func toJobDTO(j domain.Job) gen.Job {
	dto := gen.Job{
		Id:             j.ID,
		Handler:        j.Handler,
		Payload:        j.Payload,
		Queue:          j.Queue,
		Status:         gen.JobStatus(j.Status),
		Priority:       j.Priority,
		ScheduledAt:    j.ScheduledAt.UTC(),
		MaxAttempts:    j.MaxAttempts,
		TimeoutSeconds: j.TimeoutSeconds,
		CreatedAt:      j.CreatedAt.UTC(),
		UpdatedAt:      j.UpdatedAt.UTC(),
		LastError:      j.LastError,
	}
	if j.Name != "" {
		name := j.Name
		dto.Name = &name
	}
	if j.CompletedAt != nil {
		t := j.CompletedAt.UTC()
		dto.CompletedAt = &t
	}
	return dto
}
