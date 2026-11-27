package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/payouts"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Creator payouts.
//
// A creator asks for money the ledger already owes them; only `payouts.approve`
// decides. The permission check is in the router, and the service refuses a
// creator deciding their own request even if a future role grant would allow it.
// Nothing here can mark a payout paid: no rail is configured, and the platform
// does not claim money left when it did not.

type payoutHandler struct {
	auth    *authHandler
	payouts *payouts.Service
	logger  *slog.Logger
	// Requesting money is rare and deliberate; reading is frequent; deciding is
	// rare and consequential.
	request *limiter
	read    *limiter
	decide  *limiter
}

func newPayoutHandler(auth *authHandler, svc *payouts.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *payoutHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &payoutHandler{
		auth:    auth,
		payouts: svc,
		logger:  logger,
		request: newLimiter(10, time.Hour, shared),
		read:    newLimiter(120, time.Minute, shared),
		decide:  newLimiter(60, time.Hour, shared),
	}
}

func (h *payoutHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/creator/payouts", rateLimited(h.read, "payout_list",
		h.auth.requirePermission(identity.PermPayoutRequest, h.handleMine)))
	mux.HandleFunc("POST /v1/creator/payouts", rateLimited(h.request, "payout_request",
		h.auth.requirePermission(identity.PermPayoutRequest, h.handleRequest)))
	mux.HandleFunc("GET /v1/creator/payouts/{id}", rateLimited(h.read, "payout_read",
		h.auth.requirePermission(identity.PermPayoutRequest, h.handleDetail)))
	mux.HandleFunc("POST /v1/creator/payouts/{id}/cancel", rateLimited(h.request, "payout_cancel",
		h.auth.requirePermission(identity.PermPayoutRequest, h.handleCancel)))

	mux.HandleFunc("GET /v1/admin/payouts", rateLimited(h.read, "payout_queue",
		h.auth.requirePermission(identity.PermPayoutApprove, h.handleQueue)))
	mux.HandleFunc("GET /v1/admin/payouts/{id}", rateLimited(h.read, "payout_admin_read",
		h.auth.requirePermission(identity.PermPayoutApprove, h.handleAdminDetail)))
	mux.HandleFunc("POST /v1/admin/payouts/{id}/approve", rateLimited(h.decide, "payout_approve",
		h.auth.requirePermission(identity.PermPayoutApprove, h.handleApprove)))
	mux.HandleFunc("POST /v1/admin/payouts/{id}/reject", rateLimited(h.decide, "payout_reject",
		h.auth.requirePermission(identity.PermPayoutApprove, h.handleReject)))
}

// handleMine returns the caller's requests together with the balance they are
// measured against, so the screen needs one request rather than three.
func (h *payoutHandler) handleMine(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "USD"
	}
	list, err := h.payouts.List(r.Context(), p.UserID)
	if err != nil {
		h.internal(w, r, "list payouts", err)
		return
	}
	balance, err := h.payouts.Balance(r.Context(), p.UserID, currency)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payouts": list, "balance": balance})
}

func (h *payoutHandler) handleRequest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in payouts.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	req, err := h.payouts.Request(r.Context(), p, in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"payout": req})
}

func (h *payoutHandler) handleCancel(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.payouts.Cancel(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payout": req})
}

func (h *payoutHandler) handleDetail(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.payouts.Detail(r.Context(), p, r.PathValue("id"), false)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payout": req})
}

func (h *payoutHandler) handleQueue(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	list, err := h.payouts.Queue(r.Context(), r.URL.Query().Get("status"), 0)
	if err != nil {
		h.internal(w, r, "read the payout queue", err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payouts": list})
}

func (h *payoutHandler) handleAdminDetail(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	req, err := h.payouts.Detail(r.Context(), p, r.PathValue("id"), true)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payout": req})
}

// decisionNote is the body both decisions accept. The note is optional on an
// approval and mandatory on a refusal; the service, not the handler, decides.
type decisionNote struct {
	Note string `json:"note"`
}

func (h *payoutHandler) handleApprove(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in decisionNote
	// An empty body is a legal approval, so an absent body is not an error; a
	// malformed one still is.
	if err := decodeJSON(w, r, &in); err != nil && !errors.Is(err, io.EOF) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	req, err := h.payouts.Approve(r.Context(), p, r.PathValue("id"), in.Note, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payout": req})
}

func (h *payoutHandler) handleReject(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in decisionNote
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	req, err := h.payouts.Reject(r.Context(), p, r.PathValue("id"), in.Note, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payout": req})
}

// writeError maps every domain error to one stable problem code. The mapping is
// exhaustive on purpose: an unmapped error becomes a 500 rather than leaking.
func (h *payoutHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, payouts.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_payout", err.Error())
	case errors.Is(err, payouts.ErrInsufficientBalance):
		writeProblem(w, r, http.StatusConflict, "insufficient_balance", err.Error())
	case errors.Is(err, payouts.ErrConflict):
		writeProblem(w, r, http.StatusConflict, "payout_conflict", err.Error())
	case errors.Is(err, payouts.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, payouts.ErrNotFound):
		notFound(w, r)
	default:
		h.internal(w, r, "payout request failed", err)
	}
}

func (h *payoutHandler) internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	h.logger.ErrorContext(r.Context(), what+" failed",
		slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
	internalError(w, r)
}
