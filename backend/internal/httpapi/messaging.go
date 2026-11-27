package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/media"
	"github.com/jawuu187-ops/content-platform/backend/internal/messaging"
	"github.com/jawuu187-ops/content-platform/backend/internal/payments"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Direct messages.
//
// Every route requires a session; there is no anonymous read of a thread. The service
// re-checks entitlement on send, so a stale client cannot keep writing after a
// subscription lapses. Bodies are plain text, bounded by the service, and the
// database refuses edits and deletes.

type dmHandler struct {
	auth   *authHandler
	dm     *messaging.Service
	media  *media.Service
	pay    *payments.Service
	logger *slog.Logger
	write  *limiter
	read   *limiter
}

func newDMHandler(auth *authHandler, dm *messaging.Service, m *media.Service, pay *payments.Service,
	logger *slog.Logger, shared *ratelimit.Backend) *dmHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &dmHandler{
		auth:   auth,
		dm:     dm,
		media:  m,
		pay:    pay,
		logger: logger,
		write:  newLimiter(60, time.Hour, shared),
		read:   newLimiter(240, time.Minute, shared),
	}
}

func (h *dmHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/dm/conversations", rateLimited(h.write, "dm_open",
		h.auth.requireAuth(h.handleOpen)))
	mux.HandleFunc("GET /v1/dm/conversations", rateLimited(h.read, "dm_list",
		h.auth.requireAuth(h.handleList)))
	mux.HandleFunc("GET /v1/dm/conversations/{id}/messages", rateLimited(h.read, "dm_thread",
		h.auth.requireAuth(h.handleThread)))
	mux.HandleFunc("POST /v1/dm/conversations/{id}/messages", rateLimited(h.write, "dm_send",
		h.auth.requireAuth(h.handleSend)))
	mux.HandleFunc("POST /v1/dm/conversations/{id}/attachments", rateLimited(h.write, "dm_send",
		h.auth.requireAuth(h.handleAttachment)))
	mux.HandleFunc("GET /v1/dm/messages/{id}/media", rateLimited(h.read, "dm_media",
		h.auth.requireAuth(h.handleMessageMedia)))
	mux.HandleFunc("POST /v1/dm/broadcast", rateLimited(h.write, "dm_broadcast",
		h.auth.requireAuth(h.handleBroadcast)))
	mux.HandleFunc("POST /v1/dm/requests", rateLimited(h.write, "dm_request",
		h.auth.requireAuth(h.handleRequest)))
	mux.HandleFunc("GET /v1/dm/requests", rateLimited(h.read, "dm_requests",
		h.auth.requireAuth(h.handleRequests)))
	mux.HandleFunc("POST /v1/dm/conversations/{id}/accept", rateLimited(h.write, "dm_request_decide",
		h.auth.requireAuth(h.handleAcceptRequest)))
	mux.HandleFunc("POST /v1/dm/conversations/{id}/reject", rateLimited(h.write, "dm_request_decide",
		h.auth.requireAuth(h.handleRejectRequest)))
	mux.HandleFunc("POST /v1/dm/conversations/{id}/block", rateLimited(h.write, "dm_request_decide",
		h.auth.requireAuth(h.handleBlockRequest)))
	mux.HandleFunc("POST /v1/dm/messages/{id}/unlock", rateLimited(h.write, "dm_unlock",
		h.auth.requireAuth(h.handleUnlockMessage)))
	mux.HandleFunc("GET /v1/dm/settings", rateLimited(h.read, "dm_settings",
		h.auth.requireAuth(h.handleGetMessagingSettings)))
	mux.HandleFunc("PATCH /v1/dm/settings", rateLimited(h.write, "dm_settings",
		h.auth.requireAuth(h.handleSetMessagingSettings)))
}

func (h *dmHandler) writeDMError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, messaging.ErrInvalidInput), errors.Is(err, messaging.ErrBodyTooLong):
		writeProblem(w, r, http.StatusBadRequest, "invalid_message", err.Error())
	case errors.Is(err, messaging.ErrSelf):
		writeProblem(w, r, http.StatusUnprocessableEntity, "not_allowed", "you cannot message yourself")
	case errors.Is(err, messaging.ErrNotEntitled):
		// 403, not 404: the creator exists and the thread may exist too. The caller is
		// simply not entitled to write until they subscribe or unlock.
		writeProblem(w, r, http.StatusForbidden, "subscription_required",
			"a live subscription or unlock is required to message this creator")
	case errors.Is(err, messaging.ErrRequestPolicy):
		writeProblem(w, r, http.StatusForbidden, "request_not_allowed",
			"this creator is not accepting message requests")
	case errors.Is(err, messaging.ErrRequestPending):
		writeProblem(w, r, http.StatusConflict, "request_pending",
			"this message request is waiting for the creator")
	case errors.Is(err, messaging.ErrAlreadyUnlocked):
		writeProblem(w, r, http.StatusConflict, "already_unlocked",
			"this message is already unlocked")
		writeProblem(w, r, http.StatusBadRequest, "invalid_message", err.Error())
	case errors.Is(err, messaging.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "not_a_participant", "this conversation is not yours")
	case errors.Is(err, messaging.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "conversation not found")
	default:
		h.logger.ErrorContext(r.Context(), "dm request failed", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "something went wrong")
	}
}

func (h *dmHandler) handleOpen(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		CreatorID string `json:"creator_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	conv, err := h.dm.Open(r.Context(), p, in.CreatorID)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"conversation": conv})
}

func (h *dmHandler) handleList(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	convs, err := h.dm.List(r.Context(), p)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": convs})
}

func (h *dmHandler) handleThread(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	msgs, err := h.dm.Thread(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (h *dmHandler) handleSend(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	m, err := h.dm.Send(r.Context(), p, r.PathValue("id"), in.Body)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": m})
}

func (h *dmHandler) handleAttachment(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		Body    string `json:"body"`
		MediaID string `json:"media_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	m, err := h.dm.SendWithMedia(r.Context(), p, r.PathValue("id"), in.Body, in.MediaID)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": m})
}

func (h *dmHandler) handleMessageMedia(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	mediaID, err := h.dm.MessageMedia(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	obj, err := h.media.ObjectByID(r.Context(), mediaID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dm attachment object failed", "error", err)
		writeProblem(w, r, http.StatusNotFound, "not_found", "attachment not found")
		return
	}
	body, _, err := h.media.Open(r.Context(), obj.StorageKey)
	if err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "media_unavailable", "attachment storage is unavailable")
		return
	}
	defer func() { _ = body.Close() }()
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, body)
}

func (h *dmHandler) handleBroadcast(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	sent, err := h.dm.Broadcast(r.Context(), p, in.Body)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sent": sent})
}

func (h *dmHandler) handleRequest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		CreatorID string `json:"creator_id"`
		Body      string `json:"body"`
		MediaID   string `json:"media_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	conv, msg, err := h.dm.Request(r.Context(), p, in.CreatorID, in.Body, in.MediaID)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"conversation": conv, "message": msg})
}

func (h *dmHandler) handleRequests(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	all, err := h.dm.List(r.Context(), p)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	out := make([]messaging.Conversation, 0)
	for _, c := range all {
		if c.Status == "requested" && c.CreatorID == p.UserID {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func (h *dmHandler) handleAcceptRequest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	conv, err := h.dm.AcceptRequest(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": conv})
}

func (h *dmHandler) handleRejectRequest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	conv, err := h.dm.RejectRequest(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": conv})
}

func (h *dmHandler) handleBlockRequest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	conv, err := h.dm.BlockRequest(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": conv})
}

func (h *dmHandler) handleUnlockMessage(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	if h.pay == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "payment_unavailable", "payments are not configured")
		return
	}
	intent, err := h.pay.CreateIntent(r.Context(), p, payments.CreateInput{
		Purpose: payments.PurposeMessage, MessageID: r.PathValue("id"),
		IdempotencyKey: in.IdempotencyKey,
	}, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	if err := h.pay.SimulateSettle(r.Context(), intent.ID); err != nil {
		h.writeDMError(w, r, err)
		return
	}
	msg, err := h.dm.MessageByID(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": msg, "payment": intent})
}

func (h *dmHandler) handleGetMessagingSettings(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	s, err := h.dm.Settings(r.Context(), p)
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s})
}

func (h *dmHandler) handleSetMessagingSettings(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var in messaging.Settings
	if err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	s, err := h.dm.UpdateSettings(r.Context(), p, in, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeDMError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s})
}
