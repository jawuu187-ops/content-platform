package httpapi

import (
	"errors"
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

type passwordResetRequest struct {
	Email string `json:"email"`
}

type passwordResetConfirm struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// handlePasswordResetRequest always answers 202. A caller cannot use it to learn
// whether an address is registered, and the response never contains a token.
func (h *authHandler) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	var body passwordResetRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	if err := h.svc.RequestPasswordReset(r.Context(), body.Email, RequestIDFromContext(r.Context())); err != nil {
		// The response stays generic, but operators need the failure in the log.
		h.logger.ErrorContext(r.Context(), "password reset request failed", "error", err)
	}
	noStore(w)
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func (h *authHandler) handlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var body passwordResetConfirm
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	err := h.svc.ConfirmPasswordReset(r.Context(), body.Token, body.NewPassword, RequestIDFromContext(r.Context()))
	if err != nil {
		switch {
		case errors.Is(err, identity.ErrInvalidResetToken):
			writeProblem(w, r, http.StatusBadRequest, "invalid_reset_token",
				"the reset code is invalid, expired or already used")
		case errors.Is(err, identity.ErrWeakPassword):
			writeProblem(w, r, http.StatusBadRequest, "weak_password", err.Error())
		default:
			h.writeError(w, r, err)
		}
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"password_reset":   true,
		"sessions_revoked": true,
	})
}
