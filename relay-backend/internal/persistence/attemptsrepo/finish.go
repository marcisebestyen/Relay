package attemptsrepo

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// Finish sets a running attempt to a final status. result and errText may be
// nil (stored as NULL). It returns false when the attempt is not running.
func Finish(ctx context.Context, q db.Querier, id uuid.UUID, status domain.AttemptStatus, errText, errorKind *string, result []byte) (bool, error) {
	var res any
	if result != nil {
		res = result
	}
	tag, err := q.Exec(ctx, `
		UPDATE job_attempts SET
			status = $2,
			finished_at = now(),
			error = $3,
			error_kind = $4,
			result = $5
		WHERE id = $1 AND status = 'running'`,
		id, string(status), errText, errorKind, res)
	if err != nil {
		return false, fmt.Errorf("finish attempt %s: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}
