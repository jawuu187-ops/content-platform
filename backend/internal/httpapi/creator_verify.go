package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/creatorverify"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

type creatorVerifyHandler struct {
	auth  *authHandler
	svc   *creatorverify.Service
	log   *slog.Logger
	read  *limiter
	write *limiter
}

func newCreatorVerifyHandler(auth *authHandler, svc *creatorverify.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *creatorVerifyHandler {
	return &creatorVerifyHandler{
		auth: auth, svc: svc, log: logger,
		read:  newLimiter(120, 60*60, shared),
		write: newLimiter(20, 60*60, shared),
	}
}

func (h *creatorVerifyHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/creators/me/verification", rateLimited(h.read, "creator_verify",
		h.auth.requireAuth(h.handleLatest)))
	mux.HandleFunc("POST /v1/creators/me/verification", rateLimited(h.write, "creator_verify",
		h.auth.requireAuth(h.handleSubmit)))
	owner := func(next func(http.ResponseWriter, *http.Request, identity.Principal)) http.HandlerFunc {
		return h.auth.requirePermission(identity.PermVerifyReview, next)
	}
	mux.HandleFunc("GET /v1/owner/creator-verifications", rateLimited(h.read, "creator_verify_queue",
		owner(h.handleQueue)))
	mux.HandleFunc("GET /v1/owner/creator-verifications/{id}", rateLimited(h.read, "creator_verify_get",
		owner(h.handleGet)))
	mux.HandleFunc("POST /v1/owner/creator-verifications/{id}/approve",
		rateLimited(h.write, "creator_verify_decide", owner(h.handleApprove)))
	mux.HandleFunc("POST /v1/owner/creator-verifications/{id}/reject",
		rateLimited(h.write, "creator_verify_decide", owner(h.handleReject)))
}

func (h *creatorVerifyHandler) handleLatest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.svc.Latest(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"verification": req,
		"policy":       "Metadata-only review. No ID image or document is accepted or stored.",
	})
}

func (h *creatorVerifyHandler) handleSubmit(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creatorverify.SubmitInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	req, err := h.svc.Submit(r.Context(), p, body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{
		"verification": req,
		"notice":       "Submitted for owner review. No identity document was uploaded or stored.",
	})
}

func (h *creatorVerifyHandler) handleQueue(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	items, err := h.svc.Queue(r.Context(), 50)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"verifications": items})
}

func (h *creatorVerifyHandler) handleGet(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"verification": req})
}

type creatorVerifyDecision struct {
	Reason string `json:"reason"`
}

func (h *creatorVerifyHandler) handleApprove(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.svc.Decide(r.Context(), p, r.PathValue("id"), true, "",
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"verification": req})
}

func (h *creatorVerifyHandler) handleReject(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creatorVerifyDecision
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	req, err := h.svc.Decide(r.Context(), p, r.PathValue("id"), false, body.Reason,
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"verification": req})
}

func (h *creatorVerifyHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, creatorverify.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "the verification request does not exist")
	case errors.Is(err, creatorverify.ErrInvalid):
		writeProblem(w, r, http.StatusBadRequest, "invalid_verification", err.Error())
	case errors.Is(err, creatorverify.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "mfa_required",
			"a recent MFA step-up and verify.review permission are required")
	default:
		h.log.ErrorContext(r.Context(), "creator verification failed", "error", err)
		internalError(w, r)
	}
}
