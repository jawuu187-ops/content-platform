package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/payments"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Payment surface.
//
// Two rules shape it: a client can never state a price (only what it wants to
// buy), and a callback is never trusted until the provider's signature over the
// raw body has been verified by the provider implementation itself.

type paymentHandler struct {
	auth     *authHandler
	payments *payments.Service
	logger   *slog.Logger
	create   *limiter
	webhook  *limiter
	tip      *limiter
}

func newPaymentHandler(auth *authHandler, svc *payments.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *paymentHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &paymentHandler{
		auth:     auth,
		payments: svc,
		logger:   logger,
		create:   newLimiter(30, time.Hour, shared),
		webhook:  newLimiter(300, time.Minute, shared),
		tip:      newLimiter(20, time.Hour, shared),
	}
}

func (h *paymentHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, payments.ErrUnavailable):
		writeProblem(w, r, http.StatusServiceUnavailable, "payment_unavailable",
			"no payment provider is configured on this instance")
	case errors.Is(err, payments.ErrBadSignature):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_signature", "the callback signature is not valid")
	case errors.Is(err, payments.ErrAmountMismatch):
		writeProblem(w, r, http.StatusConflict, "amount_mismatch",
			"the callback amount does not match the payment intent")
	case errors.Is(err, payments.ErrInvalidState):
		writeProblem(w, r, http.StatusConflict, "invalid_payment_state",
			"the payment is not in a state that allows this")
	case errors.Is(err, payments.ErrRefundUnsupported):
		writeProblem(w, r, http.StatusNotImplemented, "refund_unsupported",
			"the configured provider does not support refunds")
	case errors.Is(err, payments.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "payment_forbidden", "this payment action is not allowed")
	case errors.Is(err, payments.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "the payment does not exist")
	case errors.Is(err, payments.ErrInvalidInput), errors.Is(err, creator.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, creator.ErrTierInactive):
		writeProblem(w, r, http.StatusConflict, "tier_inactive", "that tier is not on sale")
	case errors.Is(err, creator.ErrSelfSubscription):
		writeProblem(w, r, http.StatusForbidden, "self_purchase", "you cannot buy your own content")
	case errors.Is(err, creator.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "the requested item does not exist")
	default:
		h.logger.ErrorContext(r.Context(), "payment request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path), slog.Any("error", err))
		internalError(w, r)
	}
}

func (h *paymentHandler) handleCreate(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body payments.CreateInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	intent, err := h.payments.CreateIntent(r.Context(), p, body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"payment": intent})
}

// handleTip creates a tip intent. It is a separate route because a tip is the one payment
// whose amount comes from the client: the request names a recipient, an amount in minor
// units and at most one short line. The service bounds all three.
func (h *paymentHandler) handleTip(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body struct {
		CreatorID      string `json:"creator_id"`
		AmountMinor    int64  `json:"amount_minor"`
		Note           string `json:"note"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	intent, err := h.payments.CreateIntent(r.Context(), p, payments.CreateInput{
		Purpose:        payments.PurposeTip,
		CreatorID:      body.CreatorID,
		AmountMinor:    body.AmountMinor,
		Note:           body.Note,
		IdempotencyKey: body.IdempotencyKey,
	}, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"tip": intent})
}

// handleSimulate completes a fake-provider intent for its owner. A real intent
// cannot pass: the service checks the provider's `simulated` stamp.
func (h *paymentHandler) handleSimulate(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	intent, err := h.payments.SimulateSettleFor(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payment": intent})
}

func (h *paymentHandler) handleGet(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	intent, err := h.payments.Get(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payment": intent})
}

func (h *paymentHandler) handleList(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	list, err := h.payments.ListMine(r.Context(), p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payments": list})
}

// handleWebhook is unauthenticated by design: the provider signs the body, and
// the signature is the authentication. The body is capped so a hostile caller
// cannot spend memory before the signature check rejects it.
func (h *paymentHandler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "the callback body could not be read")
		return
	}
	result, err := h.payments.HandleEvent(r.Context(), r.Header, body)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	status := http.StatusOK
	if result.Duplicate {
		status = http.StatusAccepted
	}
	writeJSON(w, status, map[string]any{
		"applied": result.Applied, "duplicate": result.Duplicate,
		"intent_id": result.IntentID, "status": result.Status,
	})
}

type refundBody struct {
	AmountMinor int64  `json:"amount_minor"`
	Reason      string `json:"reason"`
}

func (h *paymentHandler) handleRefund(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body refundBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	intent, err := h.payments.Refund(r.Context(), p, r.PathValue("id"), body.AmountMinor, body.Reason,
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"payment": intent})
}

func (h *paymentHandler) handleTrialBalance(w http.ResponseWriter, r *http.Request, _ identity.Principal) {
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "USD"
	}
	net, volume, err := h.payments.TrialBalance(r.Context(), currency)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	balances, err := h.payments.Balances(r.Context(), currency)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"currency": currency, "net_minor": net, "volume_minor": volume,
		"balanced": net == 0, "accounts": balances,
	})
}

func (h *paymentHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/payments/webhook",
		rateLimited(h.webhook, "payment_webhook", h.handleWebhook))
	mux.HandleFunc("POST /v1/payments/intents",
		rateLimited(h.create, "payment_create", h.auth.requireAuth(h.handleCreate)))
	mux.HandleFunc("POST /v1/tips",
		rateLimited(h.tip, "tip_create", h.auth.requireAuth(h.handleTip)))
	mux.HandleFunc("POST /v1/payments/{id}/simulate",
		rateLimited(h.create, "payment_simulate", h.auth.requireAuth(h.handleSimulate)))
	mux.HandleFunc("GET /v1/payments/{id}",
		rateLimited(h.create, "payment_read", h.auth.requireAuth(h.handleGet)))
	mux.HandleFunc("GET /v1/me/payments",
		rateLimited(h.create, "payment_list", h.auth.requireAuth(h.handleList)))
	mux.HandleFunc("POST /v1/admin/payments/{id}/refund",
		rateLimited(h.create, "payment_refund", h.auth.requirePermission(identity.PermAdminAll, h.handleRefund)))
	mux.HandleFunc("GET /v1/admin/ledger/trial-balance",
		rateLimited(h.create, "ledger_read", h.auth.requirePermission(identity.PermAdminAll, h.handleTrialBalance)))
}
