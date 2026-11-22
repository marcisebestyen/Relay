package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/api/gen"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/eventsrepo"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

var errInvalidCursor = errors.New("cursor is not valid")

// ListJobAttempts handles GET /api/v1/jobs/{id}/attempts.
func (h *JobsHandler) ListJobAttempts(w http.ResponseWriter, r *http.Request, id gen.JobId, params gen.ListJobAttemptsParams) {
	limit, ok := pageLimit(w, params.Limit)
	if !ok {
		return
	}
	after, ok := pageCursor(w, params.Cursor, math.MaxInt32)
	if !ok {
		return
	}
	page, err := h.svc.ListAttempts(r.Context(), id, int(after), limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	list := gen.JobAttemptList{Items: make([]gen.JobAttempt, len(page.Items))}
	for i, a := range page.Items {
		list.Items[i] = toAttemptDTO(a)
	}
	if page.HasMore {
		list.NextCursor = nextCursor(int64(page.Items[len(page.Items)-1].AttemptNumber))
	}
	writeJSON(w, http.StatusOK, list)
}

// ListJobEvents handles GET /api/v1/jobs/{id}/events.
func (h *JobsHandler) ListJobEvents(w http.ResponseWriter, r *http.Request, id gen.JobId, params gen.ListJobEventsParams) {
	limit, ok := pageLimit(w, params.Limit)
	if !ok {
		return
	}
	after, ok := pageCursor(w, params.Cursor, math.MaxInt64)
	if !ok {
		return
	}
	page, err := h.svc.ListEvents(r.Context(), id, after, limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	list := gen.JobEventList{Items: make([]gen.JobEvent, len(page.Items))}
	for i, e := range page.Items {
		dto, err := toEventDTO(e)
		if err != nil {
			h.writeServiceError(w, r, err)
			return
		}
		list.Items[i] = dto
	}
	if page.HasMore {
		list.NextCursor = nextCursor(page.Items[len(page.Items)-1].ID)
	}
	writeJSON(w, http.StatusOK, list)
}

// pageLimit applies the default and checks the bounds. On failure it writes
// the 422 response and returns false.
func pageLimit(w http.ResponseWriter, limit *int) (int, bool) {
	if limit == nil {
		return defaultPageLimit, true
	}
	if *limit < 1 || *limit > maxPageLimit {
		writeValidationFailed(w, map[string]string{"limit": fmt.Sprintf("must be between 1 and %d", maxPageLimit)})
		return 0, false
	}
	return *limit, true
}

// pageCursor decodes the cursor, which must be at most upper. A missing cursor
// is 0, the start of the list. On failure it writes the 400 response and
// returns false.
func pageCursor(w http.ResponseWriter, cursor *string, upper int64) (int64, bool) {
	if cursor == nil {
		return 0, true
	}
	n, err := decodeCursor(*cursor)
	if err != nil || n > upper {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, errInvalidCursor.Error(), nil)
		return 0, false
	}
	return n, true
}

// Cursors are opaque to clients. Both lists page on one positive number.
func encodeCursor(n int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(n, 10)))
}

func decodeCursor(s string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, errInvalidCursor
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || n < 1 {
		return 0, errInvalidCursor
	}
	return n, nil
}

func nextCursor(n int64) *string {
	c := encodeCursor(n)
	return &c
}

// toAttemptDTO maps an attempt to the response shape. duration_ms is derived
// from the timestamps and stays null while the attempt runs.
func toAttemptDTO(a domain.JobAttempt) gen.JobAttempt {
	dto := gen.JobAttempt{
		Id:            a.ID,
		JobId:         a.JobID,
		AttemptNumber: a.AttemptNumber,
		WorkerId:      a.WorkerID,
		Status:        gen.AttemptStatus(a.Status),
		StartedAt:     a.StartedAt.UTC(),
		Error:         a.Error,
	}
	if a.FinishedAt != nil {
		finished := a.FinishedAt.UTC()
		dto.FinishedAt = &finished
		ms := finished.Sub(dto.StartedAt).Milliseconds()
		dto.DurationMs = &ms
	}
	if len(a.Result) > 0 {
		dto.Result = a.Result
	}
	return dto
}

func toEventDTO(e eventsrepo.Event) (gen.JobEvent, error) {
	dto := gen.JobEvent{
		Id:        e.ID,
		JobId:     e.JobID,
		AttemptId: (*uuid.UUID)(e.AttemptID),
		Type:      gen.JobEventType(e.Type),
		CreatedAt: e.CreatedAt.UTC(),
	}
	if len(e.Data) > 0 {
		var data map[string]any
		if err := json.Unmarshal(e.Data, &data); err != nil {
			return gen.JobEvent{}, fmt.Errorf("event %d data is not a JSON object: %w", e.ID, err)
		}
		dto.Data = &data
	}
	return dto, nil
}
