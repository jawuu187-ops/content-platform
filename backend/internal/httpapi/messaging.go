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
	logger *slog.Logger
	write  *limiter
	read   *limiter
}

func newDMHandler(auth *authHandler, dm *messaging.Service, m *media.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *dmHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &dmHandler{
		auth:   auth,
		dm:     dm,
		media:  m,
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
