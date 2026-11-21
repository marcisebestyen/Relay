// Package api builds the HTTP router, middleware and server lifecycle.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Stable error codes returned in the error envelope.
const (
	CodeBadRequest       = "bad_request"
	CodeValidationFailed = "validation_failed"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeConflict         = "conflict"
	CodeUnauthorized     = "unauthorized"
	CodeUnavailable      = "unavailable"
	CodeInternal         = "internal"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// WriteError writes the shared JSON error envelope. Nil details become {}.
func WriteError(w http.ResponseWriter, status int, code, msg string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	writeJSON(w, status, errorBody{Error: errorPayload{Code: code, Message: msg, Details: details}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("encode response", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
