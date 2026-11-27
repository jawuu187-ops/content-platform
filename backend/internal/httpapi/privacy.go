package httpapi

import (
	"errors"
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
	"github.com/jawuu187-ops/content-platform/backend/internal/privacy"
)

type privacyHandler struct {
	auth  *authHandler
	svc   *privacy.Service
	read  *limiter
	write *limiter
}

func newPrivacyHandler(auth *authHandler, svc *privacy.Service, shared *ratelimit.Backend) *privacyHandler {
	return &privacyHandler{auth: auth, svc: svc,
		read: newLimiter(60, 60*60, shared), write: newLimiter(20, 60*60, shared)}
}

func (h *privacyHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/export", rateLimited(h.read, "export", h.auth.requireAuth(h.handleExport)))
	mux.HandleFunc("GET /v1/me/consents", rateLimited(h.read, "consents", h.auth.requireAuth(h.handleConsents)))
	mux.HandleFunc("PATCH /v1/me/consents", rateLimited(h.write, "consents", h.auth.requireAuth(h.handleSetConsent)))
	mux.HandleFunc("GET /v1/me/deletion", rateLimited(h.read, "deletion", h.auth.requireAuth(h.handleDeletion)))
	mux.HandleFunc("POST /v1/me/deletion", rateLimited(h.write, "deletion", h.auth.requireAuth(h.handleRequestDeletion)))
	mux.HandleFunc("DELETE /v1/me/deletion", rateLimited(h.write, "deletion", h.auth.requireAuth(h.handleCancelDeletion)))
}

func (h *privacyHandler) handleExport(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	body, err := h.svc.Export(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	w.Header().Set("Content-Disposition", `attachment; filename="account-export.json"`)
	writeJSON(w, http.StatusOK, body)
}

func (h *privacyHandler) handleConsents(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	items, err := h.svc.Consents(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"consents": items})
}

func (h *privacyHandler) handleSetConsent(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body struct {
		Kind    string `json:"kind"`
		Granted *bool  `json:"granted"`
	}
	if err := decodeJSON(w, r, &body); err != nil || body.Granted == nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "kind and granted are required")
		return
	}
	items, err := h.svc.SaveConsent(r.Context(), p.UserID, body.Kind, *body.Granted)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"consents": items})
}

func (h *privacyHandler) handleDeletion(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	item, err := h.svc.LatestDeletion(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"deletion": item})
}

func (h *privacyHandler) handleRequestDeletion(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	if body.Confirm != "DELETE" {
		writeProblem(w, r, http.StatusBadRequest, "confirmation_required", `type DELETE to confirm`)
		return
	}
	item, err := h.svc.RequestDeletion(r.Context(), p, body.Password, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"deletion": item,
		"notice":   "Deletion is scheduled after a seven-day grace period. Ledger and payment records are retained and anonymized.",
	})
}

func (h *privacyHandler) handleCancelDeletion(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	item, err := h.svc.CancelDeletion(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"deletion": item})
}

func (h *privacyHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidCredentials):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_credentials", "password is incorrect")
	case errors.Is(err, privacy.ErrInvalid):
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, privacy.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "no scheduled deletion")
	default:
		internalError(w, r)
	}
}
