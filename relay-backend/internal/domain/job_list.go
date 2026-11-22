package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Page size bounds for job lists.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// JobSort is the order of a job list. Both orders break ties by id.
type JobSort string

// Job list orders.
const (
	SortCreatedAtAsc  JobSort = "created_at"
	SortCreatedAtDesc JobSort = "-created_at"
)

var idPrefixPattern = regexp.MustCompile(`^[0-9a-fA-F-]{1,36}$`)

// JobCursor is the position after the last job of a page.
type JobCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// JobFilter selects one page of jobs. Values of Statuses (and of Queues)
// match with OR; the different filters match with AND.
type JobFilter struct {
	Statuses      []JobStatus
	Queues        []string
	Handler       string
	CreatedAfter  *time.Time // inclusive
	CreatedBefore *time.Time // exclusive
	WorkerID      *uuid.UUID
	IDPrefix      string
	Sort          JobSort
	Limit         int
	After         *JobCursor
}

// JobPage is one page of jobs, without payloads. Next is nil on the last page.
type JobPage struct {
	Items []Job
	Next  *JobCursor
}

// Validate returns ValidationErrors listing every invalid field.
func (f JobFilter) Validate() error {
	errs := ValidationErrors{}
	for _, s := range f.Statuses {
		if !s.IsValid() {
			errs["status"] = "must be one of pending, running, completed, failed, cancelled"
			break
		}
	}
	for _, q := range f.Queues {
		if msg, ok := validateQueue(q); !ok {
			errs["queue"] = msg
			break
		}
	}
	if f.Handler != "" {
		if msg, ok := validateHandler(f.Handler); !ok {
			errs["handler"] = msg
		}
	}
	if f.IDPrefix != "" && !validIDPrefix(f.IDPrefix) {
		errs["id_prefix"] = "must be 1 to 32 hex digits, optionally with dashes"
	}
	if f.Sort != SortCreatedAtAsc && f.Sort != SortCreatedAtDesc {
		errs["sort"] = "must be created_at or -created_at"
	}
	if f.Limit < 1 || f.Limit > MaxListLimit {
		errs["limit"] = fmt.Sprintf("must be between 1 and %d", MaxListLimit)
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// IDRange returns the smallest and largest id that start with IDPrefix. ok is
// false when there is no prefix filter or the prefix is invalid.
func (f JobFilter) IDRange() (lo, hi uuid.UUID, ok bool) {
	if f.IDPrefix == "" || !validIDPrefix(f.IDPrefix) {
		return uuid.UUID{}, uuid.UUID{}, false
	}
	hex := strings.ToLower(strings.ReplaceAll(f.IDPrefix, "-", ""))
	lo, err := uuid.Parse(hex + strings.Repeat("0", 32-len(hex)))
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, false
	}
	hi, err = uuid.Parse(hex + strings.Repeat("f", 32-len(hex)))
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, false
	}
	return lo, hi, true
}

func validIDPrefix(p string) bool {
	if !idPrefixPattern.MatchString(p) {
		return false
	}
	digits := len(strings.ReplaceAll(p, "-", ""))
	return digits >= 1 && digits <= 32
}
