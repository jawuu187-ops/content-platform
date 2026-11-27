package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/owneraccess"
)

type ownerHandler struct {
	auth   *authHandler
	svc    *owneraccess.Service
	logger *slog.Logger
}

func newOwnerHandler(auth *authHandler, svc *owneraccess.Service, logger *slog.Logger) *ownerHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ownerHandler{auth: auth, svc: svc, logger: logger}
}

func (h *ownerHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/owner/activate",
		rateLimited(h.auth.login, "owner_activate", h.auth.requireAuth(h.handleActivate)))
}

func (h *ownerHandler) handleActivate(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body struct {
		Secret string `json:"secret"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	err := h.svc.Activate(r.Context(), p, body.Secret, RequestIDFromContext(r.Context()))
	switch {
	case err == nil:
		noStore(w)
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, owneraccess.ErrDisabled):
		writeProblem(w, r, http.StatusServiceUnavailable, "owner_activation_unavailable",
			"owner activation is not configured on this deployment")
	case errors.Is(err, owneraccess.ErrMFARequired):
		writeProblem(w, r, http.StatusForbidden, "mfa_required",
			"a recent second-factor step-up is required before owner activation")
	case errors.Is(err, owneraccess.ErrDenied):
		writeProblem(w, r, http.StatusForbidden, "forbidden",
			"this account cannot activate owner access with the supplied value")
	case errors.Is(err, owneraccess.ErrAlreadyActivated):
		writeProblem(w, r, http.StatusConflict, "already_activated",
			"owner activation was already consumed")
	case errors.Is(err, owneraccess.ErrOwnerExists):
		writeProblem(w, r, http.StatusConflict, "owner_exists",
			"this deployment already has an owner")
	default:
		h.logger.Error("owner activation failed", "error", err, "request_id", RequestIDFromContext(r.Context()))
		writeProblem(w, r, http.StatusInternalServerError, "internal_error",
			"owner activation could not be completed")
	}
}
