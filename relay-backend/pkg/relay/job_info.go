package relay

import (
	"context"

	"github.com/google/uuid"
)

// JobInfo says which job attempt a handler is running.
type JobInfo struct {
	JobID         uuid.UUID
	AttemptNumber int
	Queue         string
	WorkerID      uuid.UUID
}

type jobInfoKey struct{}

// WithJobInfo returns a copy of ctx that carries info. The worker runtime
// calls it before each handler runs.
func WithJobInfo(ctx context.Context, info JobInfo) context.Context {
	return context.WithValue(ctx, jobInfoKey{}, info)
}

// JobInfoFromContext returns the job info a handler was started with, and
// false when ctx has none.
func JobInfoFromContext(ctx context.Context) (JobInfo, bool) {
	info, ok := ctx.Value(jobInfoKey{}).(JobInfo)
	return info, ok
}
