package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/moderation"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Safety reports and the moderation queue.
//
// Anyone with a session can report a thing they can see. Only a caller holding
// safety.review can read the queue or change a report's status; the check happens in the
// router, not in the UI, and the domain service enforces visibility of the reported object
// so this endpoint cannot be used to enumerate private posts or messages.

type moderationHandler struct {
	auth    *authHandler
	reports *moderation.Service
	logger  *slog.Logger
	submit  *limiter
	read    *limiter
	decide  *limiter
}

func newModerationHandler(auth *authHandler, svc *moderation.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *moderationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &moderationHandler{
		auth:    auth,
		reports: svc,
		logger:  logger,
		// Reporting is rare and deliberate; reading is frequent; deciding is rare.
		submit: newLimiter(10, time.Hour, shared),
		read:   newLimiter(120, time.Minute, shared),
		decide: newLimiter(60, time.Hour, shared),
	}
}

func (h *moderationHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/reports", rateLimited(h.submit, "report_submit",
		h.auth.requireAuth(h.handleSubmit)))
	mux.HandleFunc("GET /v1/reports/mine", rateLimited(h.read, "report_mine",
		h.auth.requireAuth(h.handleMine)))
	mux.HandleFunc("GET /v1/reports/{id}", rateLimited(h.read, "report_read",
		h.auth.requireAuth(h.handleDetail)))

	mux.HandleFunc("GET /v1/moderation/reports", rateLimited(h.read, "moderation_queue",
		h.auth.requirePermission(identity.PermSafetyReview, h.handleQueue)))
	mux.HandleFunc("POST /v1/moderation/reports/{id}/transitions", rateLimited(h.decide, "moderation_decide",
		h.auth.requirePermission(identity.PermSafetyReview, h.handleTransition)))
	mux.HandleFunc("POST /v1/copyright/claims", rateLimited(h.submit, "copyright_submit",
		h.auth.requireAuth(h.handleCopyrightSubmit)))
	mux.HandleFunc("GET /v1/copyright/claims/mine", rateLimited(h.read, "copyright_mine",
		h.auth.requireAuth(h.handleCopyrightMine)))
	mux.HandleFunc("POST /v1/copyright/claims/{id}/counter", rateLimited(h.decide, "copyright_counter",
		h.auth.requireAuth(h.handleCopyrightCounter)))
	mux.HandleFunc("GET /v1/moderation/copyright", rateLimited(h.read, "copyright_queue",
		h.auth.requirePermission(identity.PermSafetyReview, h.handleCopyrightQueue)))
	mux.HandleFunc("POST /v1/moderation/copyright/{id}/decision", rateLimited(h.decide, "copyright_decide",
		h.auth.requirePermission(identity.PermSafetyReview, h.handleCopyrightDecision)))
}

func (h *moderationHandler) handleSubmit(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in moderation.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	rep, err := h.reports.Submit(r.Context(), p, in)
	switch {
	case errors.Is(err, moderation.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_report", err.Error())
		return
	case errors.Is(err, moderation.ErrNotFound):
		// Deliberately the same answer for "does not exist" and "not yours": the
		// endpoint must not confirm that a private message id exists.
		writeProblem(w, r, http.StatusNotFound, "not_found", "nothing to report was found")
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "report submission failed",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
		internalError(w, r)
		return
	}
	noStore(w)
	status := http.StatusCreated
	if rep.Duplicate {
		// The complaint already exists; the caller learns the id and that nothing
		// new was created, which keeps a retry from piling up rows.
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"report": rep})
}

func (h *moderationHandler) handleMine(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	list, err := h.reports.Mine(r.Context(), p)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list own reports failed",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
		internalError(w, r)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"reports": list})
}

func (h *moderationHandler) handleDetail(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	anyReport := identity.HasPermission(p.Roles, identity.PermSafetyReview)
	rep, err := h.reports.Detail(r.Context(), p, r.PathValue("id"), anyReport)
	switch {
	case errors.Is(err, moderation.ErrNotFound):
		notFound(w, r)
		return
	case errors.Is(err, moderation.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "forbidden", "this report is not yours")
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "read report failed",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
		internalError(w, r)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"report": rep})
}

func (h *moderationHandler) handleQueue(w http.ResponseWriter, r *http.Request, _ identity.Principal) {
	status := r.URL.Query().Get("status")
	limit := 0
	list, err := h.reports.Queue(r.Context(), status, limit)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "moderation queue failed",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
		internalError(w, r)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"reports": list})
}

func (h *moderationHandler) handleTransition(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	rep, err := h.reports.Transition(r.Context(), p, r.PathValue("id"), body.Action, body.Note)
	switch {
	case errors.Is(err, moderation.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_transition", err.Error())
		return
	case errors.Is(err, moderation.ErrConflict):
		writeProblem(w, r, http.StatusConflict, "invalid_transition", err.Error())
		return
	case errors.Is(err, moderation.ErrNotFound):
		notFound(w, r)
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "report transition failed",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
		internalError(w, r)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"report": rep})
}

func (h *moderationHandler) handleCopyrightSubmit(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in moderation.CopyrightInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	claim, err := h.reports.SubmitCopyright(r.Context(), p, in)
	if err != nil {
		h.writeModerationError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"claim": claim})
}

func (h *moderationHandler) handleCopyrightMine(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	claims, err := h.reports.CopyrightMine(r.Context(), p)
	if err != nil {
		h.writeModerationError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"claims": claims})
}

func (h *moderationHandler) handleCopyrightCounter(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		Notice string `json:"notice"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	claim, err := h.reports.CounterNotice(r.Context(), p, r.PathValue("id"), in.Notice)
	if err != nil {
		h.writeModerationError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"claim": claim})
}

func (h *moderationHandler) handleCopyrightQueue(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	claims, err := h.reports.CopyrightQueue(r.Context())
	if err != nil {
		h.writeModerationError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"claims": claims})
}

func (h *moderationHandler) handleCopyrightDecision(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		Takedown bool   `json:"takedown"`
		Note     string `json:"note"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	claim, err := h.reports.DecideCopyright(r.Context(), p, r.PathValue("id"), in.Takedown, in.Note)
	if err != nil {
		h.writeModerationError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"claim": claim})
}

func (h *moderationHandler) writeModerationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, moderation.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_report", err.Error())
	case errors.Is(err, moderation.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "the claim or target does not exist")
	case errors.Is(err, moderation.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "not_allowed", "this account cannot perform that action")
	case errors.Is(err, moderation.ErrConflict):
		writeProblem(w, r, http.StatusConflict, "conflict", "that decision is no longer available")
	default:
		h.logger.ErrorContext(r.Context(), "moderation request failed", "error", err)
		internalError(w, r)
	}
}
