package jobsrepo

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// FinishOwned moves a running job to status completed or failed and clears
// ownership. It only matches while claimToken is still the job's token. It
// returns false when no row matched, so the caller's claim is stale.
func FinishOwned(ctx context.Context, q db.Querier, id, claimToken uuid.UUID, status domain.JobStatus, lastError *string) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE jobs SET
			status = $3,
			last_error = $4,
			completed_at = CASE WHEN $3 = 'completed' THEN now() ELSE completed_at END,
			worker_id = NULL,
			lease_expires_at = NULL,
			claim_token = NULL,
			updated_at = now()
		WHERE id = $1 AND status = 'running' AND claim_token = $2`,
		id, claimToken, string(status), lastError)
	if err != nil {
		return false, fmt.Errorf("finish job %s: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}
