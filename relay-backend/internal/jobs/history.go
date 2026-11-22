package jobs

import (
	"context"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/attemptsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/jobsrepo"
)

// Page is one page of a list. HasMore is true when more items follow it.
type Page[T any] struct {
	Items   []T
	HasMore bool
}

// ListAttempts returns up to limit attempts of the job after attempt number
// afterNumber, or db.ErrNotFound when the job does not exist.
func (s *Service) ListAttempts(ctx context.Context, jobID uuid.UUID, afterNumber, limit int) (Page[domain.JobAttempt], error) {
	if _, err := jobsrepo.GetByID(ctx, s.pool, jobID); err != nil {
		return Page[domain.JobAttempt]{}, err
	}
	items, err := attemptsrepo.ListByJob(ctx, s.pool, jobID, afterNumber, limit+1)
	if err != nil {
		return Page[domain.JobAttempt]{}, err
	}
	return trim(items, limit), nil
}

// ListEvents returns up to limit events of the job after event afterID, or
// db.ErrNotFound when the job does not exist.
func (s *Service) ListEvents(ctx context.Context, jobID uuid.UUID, afterID int64, limit int) (Page[eventsrepo.Event], error) {
	if _, err := jobsrepo.GetByID(ctx, s.pool, jobID); err != nil {
		return Page[eventsrepo.Event]{}, err
	}
	items, err := eventsrepo.ListByJob(ctx, s.pool, jobID, afterID, limit+1)
	if err != nil {
		return Page[eventsrepo.Event]{}, err
	}
	return trim(items, limit), nil
}

// trim cuts the extra row that was read only to learn whether more follow.
func trim[T any](items []T, limit int) Page[T] {
	if len(items) > limit {
		return Page[T]{Items: items[:limit], HasMore: true}
	}
	return Page[T]{Items: items}
}
