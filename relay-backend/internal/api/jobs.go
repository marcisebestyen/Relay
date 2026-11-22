package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/api/gen"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

// JobService is what the job endpoints need from the job use cases.
type JobService interface {
	Create(ctx context.Context, in domain.NewJobInput) (domain.Job, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Job, error)
	List(ctx context.Context, f domain.JobFilter) (domain.JobPage, error)
}

// JobsHandler serves the job endpoints. The other generated methods stay
// unimplemented until their tickets land and are not routed.
type JobsHandler struct {
	gen.Unimplemented
	svc    JobService
	logger *slog.Logger
}

// NewJobsHandler builds a JobsHandler.
func NewJobsHandler(svc JobService, logger *slog.Logger) *JobsHandler {
	return &JobsHandler{svc: svc, logger: logger}
}

// Register adds the create, get and list routes to r. It wires the generated
// wrapper by hand so routes owned elsewhere (the spec route) stay untouched.
func (h *JobsHandler) Register(r Router) {
	w := gen.ServerInterfaceWrapper{Handler: h, ErrorHandlerFunc: paramError}
	r.Post("/api/v1/jobs", w.CreateJob)
	lw := gen.ServerInterfaceWrapper{Handler: h, ErrorHandlerFunc: listParamError}
	r.Get("/api/v1/jobs", lw.ListJobs)
	r.Get("/api/v1/jobs/{id}", w.GetJob)
}

// paramError turns a generated parameter parsing failure into the envelope.
func paramError(w http.ResponseWriter, _ *http.Request, err error) {
	WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
}

// createJobBody keeps the payload as raw bytes so big numbers and key order
// reach the database unchanged.
type createJobBody struct {
	gen.CreateJobRequest
	Payload json.RawMessage `json:"payload"`
}

// CreateJob handles POST /api/v1/jobs.
func (h *JobsHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var body createJobBody
	if !decodeBody(w, r, &body) {
		return
	}
	job, err := h.svc.Create(r.Context(), newJobInput(body))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/jobs/"+job.ID.String())
	writeJSON(w, http.StatusCreated, toJobDTO(job))
}

// GetJob handles GET /api/v1/jobs/{id}.
func (h *JobsHandler) GetJob(w http.ResponseWriter, r *http.Request, id gen.JobId) {
	job, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toJobDTO(job))
}

func (h *JobsHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var verrs domain.ValidationErrors
	switch {
	case errors.As(err, &verrs):
		writeValidationFailed(w, verrs)
	case errors.Is(err, db.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "job not found", nil)
	default:
		h.logger.ErrorContext(r.Context(), "job request failed",
			"request_id", RequestIDFromContext(r.Context()), "error", err)
		WriteError(w, http.StatusInternalServerError, CodeInternal, "internal server error", nil)
	}
}

func writeValidationFailed(w http.ResponseWriter, fields map[string]string) {
	details := make(map[string]any, len(fields))
	for k, v := range fields {
		details[k] = v
	}
	WriteError(w, http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed", details)
}

// decodeBody reads exactly one JSON object into dst. On failure it writes the
// error response and returns false.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		// A second value after the object is a malformed body.
		if err = dec.Decode(&struct{}{}); errors.Is(err, io.EOF) {
			return true
		} else if err == nil {
			err = errors.New("unexpected data after JSON body")
		}
	}
	writeDecodeError(w, err)
	return false
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var (
		tooLarge *http.MaxBytesError
		typeErr  *json.UnmarshalTypeError
		timeErr  *time.ParseError
	)
	switch {
	case errors.As(err, &tooLarge):
		WriteError(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			"request body is too large", map[string]any{"max_bytes": tooLarge.Limit})
	case errors.As(err, &typeErr) && typeErr.Field != "":
		writeValidationFailed(w, map[string]string{typeErr.Field: "must be of type " + typeErr.Type.String()})
	case errors.As(err, &timeErr):
		writeValidationFailed(w, map[string]string{"scheduled_at": "must be an RFC 3339 timestamp"})
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		WriteError(w, http.StatusBadRequest, CodeBadRequest, err.Error(), nil)
	default:
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "request body must be a single valid JSON object", nil)
	}
}
