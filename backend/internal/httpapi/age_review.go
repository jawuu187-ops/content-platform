package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/agereview"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// ageReviewHandler exposes the user's manual-review request and the owner-only
// review queue. UI visibility is never the authorization: every owner route asks
// for verify.review, and the service additionally requires the owner role and a
// recent session-bound MFA proof.
type ageReviewHandler struct {
	auth   *authHandler
	svc    *agereview.Service
	logger *slog.Logger
}

func newAgeReviewHandler(auth *authHandler, svc *agereview.Service, logger *slog.Logger) *ageReviewHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ageReviewHandler{auth: auth, svc: svc, logger: logger}
}

func (h *ageReviewHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/verify/age/review", rateLimited(h.auth.read, "age_review_self",
		h.auth.requireAuth(h.handleLatest)))
	mux.HandleFunc("POST /v1/verify/age/review", rateLimited(h.auth.login, "age_review_request",
		h.auth.requireAuth(h.handleRequest)))

	owner := func(next func(http.ResponseWriter, *http.Request, identity.Principal)) http.HandlerFunc {
		return h.auth.requirePermission(identity.PermVerifyReview, next)
	}
	mux.HandleFunc("GET /v1/owner/verify/reviews", rateLimited(h.auth.read, "age_review_queue", owner(h.handleQueue)))
	mux.HandleFunc("GET /v1/owner/verify/reviews/{id}", rateLimited(h.auth.read, "age_review_get", owner(h.handleGet)))
	mux.HandleFunc("POST /v1/owner/verify/reviews/{id}/approve",
		rateLimited(h.auth.login, "age_review_approve", owner(h.handleApprove)))
	mux.HandleFunc("POST /v1/owner/verify/reviews/{id}/reject",
		rateLimited(h.auth.login, "age_review_reject", owner(h.handleReject)))
	mux.HandleFunc("POST /v1/owner/verify/users/{id}/revoke",
		rateLimited(h.auth.login, "age_review_revoke", owner(h.handleRevoke)))
}

func (h *ageReviewHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agereview.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "forbidden",
			"only the platform owner may review age verification")
	case errors.Is(err, agereview.ErrMFARequired):
		writeProblem(w, r, http.StatusForbidden, "mfa_required",
			"a recent second-factor step-up is required for this review action")
	case errors.Is(err, agereview.ErrSelfReview):
		writeProblem(w, r, http.StatusForbidden, "self_review_forbidden",
			"you cannot review or revoke your own age verification")
	case errors.Is(err, agereview.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "review_not_found", "that review request does not exist")
	case errors.Is(err, agereview.ErrNotPending):
		writeProblem(w, r, http.StatusConflict, "review_not_pending", "that review request is already decided")
	case errors.Is(err, agereview.ErrAlreadyPending):
		writeProblem(w, r, http.StatusConflict, "review_pending", "a manual review is already waiting for a decision")
	case errors.Is(err, agereview.ErrAlreadyVerified):
		writeProblem(w, r, http.StatusConflict, "already_verified", "this account is already age verified")
	case errors.Is(err, agereview.ErrNoVerification):
		writeProblem(w, r, http.StatusConflict, "not_verified", "this account has no live verification to revoke")
	case errors.Is(err, agereview.ErrInvalidReason):
		writeProblem(w, r, http.StatusBadRequest, "invalid_reason",
			"choose one of the listed non-sensitive decision reasons")
	case errors.Is(err, agereview.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		h.logger.ErrorContext(r.Context(), "age review request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path), slog.Any("error", err))
		internalError(w, r)
	}
}

func actorOf(p identity.Principal) agereview.Actor {
	return agereview.Actor{
		UserID:    p.UserID,
		SessionID: p.SessionID,
		Roles:     p.Roles,
		MFAAt:     p.MFAAt,
	}
}

func (h *ageReviewHandler) handleLatest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.svc.LatestForUser(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"review": req})
}

func (h *ageReviewHandler) handleRequest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.svc.RequestManual(r.Context(), p.UserID, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"review": req})
}

func (h *ageReviewHandler) handleQueue(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	items, err := h.svc.Queue(r.Context(), actorOf(p))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"reviews": items})
}

func (h *ageReviewHandler) handleGet(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.svc.Get(r.Context(), actorOf(p), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"review": req})
}

func (h *ageReviewHandler) handleApprove(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.svc.Approve(r.Context(), actorOf(p), r.PathValue("id"),
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"review": req})
}

func (h *ageReviewHandler) handleReject(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	req, err := h.svc.Reject(r.Context(), actorOf(p), r.PathValue("id"), body.ReasonCode,
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"review": req})
}

func (h *ageReviewHandler) handleRevoke(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	n, err := h.svc.Revoke(r.Context(), actorOf(p), r.PathValue("id"), body.ReasonCode,
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"revoked": n})
}
