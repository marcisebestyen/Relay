package workers

import "time"

// Status is a worker's liveness as the API and dashboard show it.
type Status string

// Liveness states. A stopped worker is always stopped, even if its last
// heartbeat is old.
const (
	StatusHealthy Status = "healthy"
	StatusStale   Status = "stale"
	StatusStopped Status = "stopped"
)

// Classify returns a worker's status at now. A worker is stopped when stoppedAt
// is set. Otherwise it is stale when its last heartbeat is more than staleAfter
// old. Exactly staleAfter old is still healthy.
func Classify(now, lastHeartbeatAt time.Time, stoppedAt *time.Time, staleAfter time.Duration) Status {
	if stoppedAt != nil {
		return StatusStopped
	}
	if now.Sub(lastHeartbeatAt) > staleAfter {
		return StatusStale
	}
	return StatusHealthy
}
