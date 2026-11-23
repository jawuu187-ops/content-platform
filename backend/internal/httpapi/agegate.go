package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/agegate"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Age verification surface.
//
// The provider callback is the only unauthenticated endpoint here, and it is
// authenticated by an HMAC signature instead of a session. Everything that
// reads or changes verification state for an account needs a session, and
// revocation needs the admin permission.

const maxCallbackBytes = 64 << 10

type ageGateHandler struct {
	auth      *authHandler
	svc       *agegate.Service
	logger    *slog.Logger
	start     *limiter
	callbacks *limiter
}

func newAgeGateHandler(auth *authHandler, svc *agegate.Service, logger *slog.Logger, shared *ratelimit.Backend) *ageGateHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ageGateHandler{
		auth:      auth,
		svc:       svc,
		logger:    logger,
		start:     newLimiter(10, time.Hour, shared),
		callbacks: newLimiter(300, time.Minute, shared),
	}
}

func (h *ageGateHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agegate.ErrUnavailable):
		writeProblem(w, r, http.StatusServiceUnavailable, "verification_unavailable",
			"age verification is not configured on this instance")
	case errors.Is(err, agegate.ErrStartUnsupported):
		writeProblem(w, r, http.StatusServiceUnavailable, "verification_not_integrated",
			"no verification provider is integrated yet; verification cannot be started")
	case errors.Is(err, agegate.ErrBadSignature), errors.Is(err, agegate.ErrStaleCallback):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_signature", "the callback signature is not valid")
	case errors.Is(err, agegate.ErrInvalidCallback):
		writeProblem(w, r, http.StatusBadRequest, "invalid_callback", "the callback payload is not valid")
	case errors.Is(err, agegate.ErrUnknownReference):
		writeProblem(w, r, http.StatusNotFound, "unknown_reference", "no check matches that reference")
	case errors.Is(err, agegate.ErrAccountMismatch):
		writeProblem(w, r, http.StatusConflict, "account_mismatch", "the callback refers to a different account")
	case errors.Is(err, agegate.ErrRateLimited):
		writeProblem(w, r, http.StatusTooManyRequests, "verification_rate_limited",
			"too many verification attempts; try again later")
	case errors.Is(err, agegate.ErrAlreadyVerified):
		writeProblem(w, r, http.StatusConflict, "already_verified", "this account is already verified")
	case errors.Is(err, agegate.ErrNoLiveCheck):
		writeProblem(w, r, http.StatusNotFound, "not_verified", "this account has no live verification")
	case errors.Is(err, agegate.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		h.logger.ErrorContext(r.Context(), "age verification request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path), slog.Any("error", err))
		internalError(w, r)
	}
}

func (h *ageGateHandler) handleStatus(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	state, err := h.svc.Status(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"verification": state})
}

func (h *ageGateHandler) handleStart(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	state, err := h.svc.Start(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"verification": state})
}

// handleCallback receives a signed provider event. The body is read with a
// hard limit before signature verification so a large upload costs nothing.
func (h *ageGateHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBytes+1))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_callback", "the callback body could not be read")
		return
	}
	if len(body) > maxCallbackBytes {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "callback_too_large", "the callback body is too large")
		return
	}
	check, err := h.svc.HandleCallback(r.Context(), r.Header, body)
	switch {
	case errors.Is(err, agegate.ErrDuplicateCallback):
		// Idempotent by design: the provider may retry, the state is already right.
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "already_applied"})
		return
	case err != nil:
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "applied",
		"check":  check,
	})
}

func (h *ageGateHandler) handleAdminStatus(w http.ResponseWriter, r *http.Request, _ identity.Principal) {
	userID := r.PathValue("id")
	if userID == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "user id is required")
		return
	}
	state, err := h.svc.Status(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"verification": state})
}

type revokeBody struct {
	Reason string `json:"reason"`
}

func (h *ageGateHandler) handleRevoke(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body revokeBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	n, err := h.svc.Revoke(r.Context(), p.UserID, r.PathValue("id"), body.Reason)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if n == 0 {
		writeProblem(w, r, http.StatusConflict, "not_verified", "this account has no live verification to revoke")
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

func (h *ageGateHandler) handleCapabilities(w http.ResponseWriter, r *http.Request, _ identity.Principal) {
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"provider": h.svc.Capabilities()})
}

func (h *ageGateHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/verify/age/callback", rateLimited(h.callbacks, "age_callback", h.handleCallback))

	mux.HandleFunc("GET /v1/verify/age", rateLimited(h.start, "age_status",
		h.auth.requireAuth(h.handleStatus)))
	mux.HandleFunc("POST /v1/verify/age/start", rateLimited(h.start, "age_start",
		h.auth.requireAuth(h.handleStart)))

	mux.HandleFunc("GET /v1/admin/verify/age/{id}", rateLimited(h.start, "age_admin_status",
		h.auth.requirePermission(identity.PermAdminAll, h.handleAdminStatus)))
	mux.HandleFunc("POST /v1/admin/verify/age/{id}/revoke", rateLimited(h.start, "age_admin_revoke",
		h.auth.requirePermission(identity.PermAdminAll, h.handleRevoke)))
	mux.HandleFunc("GET /v1/admin/verify/capabilities", rateLimited(h.start, "age_capabilities",
		h.auth.requirePermission(identity.PermAdminAll, h.handleCapabilities)))
}
