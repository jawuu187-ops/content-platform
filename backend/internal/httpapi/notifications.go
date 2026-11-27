package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/notifications"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

type notificationHandler struct {
	auth  *authHandler
	svc   *notifications.Service
	read  *limiter
	write *limiter
}

func newNotificationHandler(auth *authHandler, svc *notifications.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *notificationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &notificationHandler{
		auth: auth, svc: svc,
		read:  newLimiter(240, 60*60, shared),
		write: newLimiter(120, 60*60, shared),
	}
}

func (h *notificationHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/notifications", rateLimited(h.read, "notifications",
		h.auth.requireAuth(h.handleList)))
	mux.HandleFunc("GET /v1/notifications/unread-count", rateLimited(h.read, "notifications",
		h.auth.requireAuth(h.handleUnread)))
	mux.HandleFunc("POST /v1/notifications/read-all", rateLimited(h.write, "notifications",
		h.auth.requireAuth(h.handleReadAll)))
	mux.HandleFunc("POST /v1/notifications/{id}/read", rateLimited(h.write, "notifications",
		h.auth.requireAuth(h.handleRead)))
	mux.HandleFunc("GET /v1/notifications/preferences", rateLimited(h.read, "notification_prefs",
		h.auth.requireAuth(h.handleGetPreferences)))
	mux.HandleFunc("PATCH /v1/notifications/preferences", rateLimited(h.write, "notification_prefs",
		h.auth.requireAuth(h.handleSavePreferences)))
	mux.HandleFunc("POST /v1/notifications/push-token", rateLimited(h.write, "push_token",
		h.auth.requireAuth(h.handlePushToken)))
	mux.HandleFunc("DELETE /v1/notifications/push-token", rateLimited(h.write, "push_token",
		h.auth.requireAuth(h.handleRevokePushToken)))
}

func (h *notificationHandler) handleList(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	items, unread, err := h.svc.List(r.Context(), p.UserID, limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"notifications": items, "unread_count": unread})
}

func (h *notificationHandler) handleUnread(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	n, err := h.svc.Unread(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"unread_count": n})
}

func (h *notificationHandler) handleRead(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	if err := h.svc.MarkRead(r.Context(), p.UserID, r.PathValue("id")); err != nil {
		h.writeError(w, r, err)
		return
	}
	n, _ := h.svc.Unread(r.Context(), p.UserID)
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"read": true, "unread_count": n})
}

func (h *notificationHandler) handleReadAll(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	changed, err := h.svc.MarkAllRead(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"marked": changed, "unread_count": 0})
}

func (h *notificationHandler) handleGetPreferences(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	prefs, err := h.svc.GetPreferences(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
}

type notificationPrefsBody struct {
	InApp      *bool `json:"in_app"`
	Messages   *bool `json:"messages"`
	Engagement *bool `json:"engagement"`
	Payments   *bool `json:"payments"`
	Marketing  *bool `json:"marketing"`
	Email      *bool `json:"email"`
	Push       *bool `json:"push"`
}

func (h *notificationHandler) handleSavePreferences(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body notificationPrefsBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	current, err := h.svc.GetPreferences(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if body.InApp != nil {
		current.InApp = *body.InApp
	}
	if body.Messages != nil {
		current.Messages = *body.Messages
	}
	if body.Engagement != nil {
		current.Engagement = *body.Engagement
	}
	if body.Payments != nil {
		current.Payments = *body.Payments
	}
	if body.Marketing != nil {
		current.Marketing = *body.Marketing
	}
	saved, err := h.svc.SavePreferences(r.Context(), p.UserID, current)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"preferences": saved,
		"notice":      "Email and push delivery are not configured; the in-app centre is active.",
	})
}

type pushTokenBody struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

func (h *notificationHandler) handlePushToken(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body pushTokenBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	if body.Platform == "" {
		body.Platform = "android"
	}
	if err := h.svc.RegisterPushToken(r.Context(), p.UserID, body.Token, body.Platform); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_push_token", err.Error())
		return
	}
	noStore(w)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"registered": true,
		"delivery":   "stored only; no push provider is configured",
	})
}

func (h *notificationHandler) handleRevokePushToken(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body pushTokenBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	if err := h.svc.RevokePushToken(r.Context(), p.UserID, body.Token); err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

func (h *notificationHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, notifications.ErrNotFound) {
		writeProblem(w, r, http.StatusNotFound, "not_found", "the notification does not exist")
		return
	}
	internalError(w, r)
}
