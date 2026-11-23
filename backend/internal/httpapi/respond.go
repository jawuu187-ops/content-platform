// Package httpapi wires the public HTTP surface: routing, middleware, error
// shapes and the operational endpoints. Business modules register their routes
// here through the Router type, which keeps transport concerns out of domain
// packages.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// problem is an RFC 9457 problem document. Every error response the platform
// emits uses this shape so clients can react to `code` instead of parsing prose.
type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already written; all we can do is record it.
		slog.Default().Error("httpapi: encode response", slog.Any("error", err))
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	p := problem{
		Type:      "https://errors.invalid/" + code,
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Code:      code,
		RequestID: RequestIDFromContext(r.Context()),
	}
	writeJSON(w, status, p)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusNotFound, "not_found", "the requested resource does not exist")
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this resource")
}

func internalError(w http.ResponseWriter, r *http.Request) {
	// Never leak internal error text to clients; the request id ties the client
	// report to the server-side log line with the full cause.
	writeProblem(w, r, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
}
