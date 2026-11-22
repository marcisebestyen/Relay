package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/api/gen"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

// ListJobs handles GET /api/v1/jobs.
func (h *JobsHandler) ListJobs(w http.ResponseWriter, r *http.Request, params gen.ListJobsParams) {
	filter, err := listFilter(params)
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
		return
	}
	page, err := h.svc.List(r.Context(), filter)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := gen.JobList{Items: make([]gen.JobSummary, len(page.Items))}
	for i, j := range page.Items {
		out.Items[i] = toJobSummaryDTO(j)
	}
	if page.Next != nil {
		next := encodeCursor(*page.Next)
		out.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, out)
}

// listParamError turns a query parameter parsing failure into a 422 that
// names the parameter, like a field error of the create endpoint.
func listParamError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *gen.InvalidParamFormatError
	if errors.As(err, &invalid) {
		writeValidationFailed(w, map[string]string{invalid.ParamName: "is not a valid value"})
		return
	}
	paramError(w, r, err)
}

// listFilter maps query parameters to a filter and applies the defaults. Only
// the cursor is checked here; the domain validates the rest.
func listFilter(p gen.ListJobsParams) (domain.JobFilter, error) {
	f := domain.JobFilter{Sort: domain.SortCreatedAtDesc, Limit: domain.DefaultListLimit}
	if p.Status != nil {
		for _, s := range *p.Status {
			f.Statuses = append(f.Statuses, domain.JobStatus(s))
		}
	}
	if p.Queue != nil {
		f.Queues = *p.Queue
	}
	if p.Handler != nil {
		f.Handler = *p.Handler
	}
	f.CreatedAfter = utcPtr(p.CreatedAfter)
	f.CreatedBefore = utcPtr(p.CreatedBefore)
	if p.WorkerId != nil {
		id := *p.WorkerId
		f.WorkerID = &id
	}
	if p.IdPrefix != nil {
		f.IDPrefix = *p.IdPrefix
	}
	if p.Sort != nil {
		f.Sort = domain.JobSort(*p.Sort)
	}
	if p.Limit != nil {
		f.Limit = *p.Limit
	}
	if p.Cursor != nil {
		c, err := decodeCursor(*p.Cursor)
		if err != nil {
			return domain.JobFilter{}, err
		}
		f.After = &c
	}
	return f, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
