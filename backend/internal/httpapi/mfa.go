package httpapi

import (
	"errors"
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// Email verification, multi-factor enrolment and the permission-gated admin
// surface live here. Handlers stay thin: every decision is made in the identity
// package and tested there against a real database.

type verifyConfirmBody struct {
	Code string `json:"code"`
}

type totpStartBody struct {
	Label string `json:"label"`
}

type mfaCodeBody struct {
	Code string `json:"code"`
}

type grantRoleBody struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// requirePermission wraps an authenticated handler with an authorization check.
// A denial is logged and answered with 403 + a stable code, never 404, so a
// client can tell "not allowed" from "does not exist".
func (h *authHandler) requirePermission(perm identity.Permission,
	next func(http.ResponseWriter, *http.Request, identity.Principal)) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request, p identity.Principal) {
		if !identity.HasPermission(p.Roles, perm) {
			h.logger.WarnContext(r.Context(), "authorization denied",
				"request_id", RequestIDFromContext(r.Context()),
				"user_id", p.UserID,
				"permission", string(perm),
				"roles", p.Roles,
				"path", r.URL.Path)
			writeProblem(w, r, http.StatusForbidden, "insufficient_permissions",
				"this account does not hold the permission required for this action")
			return
		}
		next(w, r, p)
	})
}

func (h *authHandler) handleEmailVerifyRequest(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	expires, err := h.svc.RequestEmailVerification(r.Context(), p, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     "sent",
		"expires_at": expires.UTC(),
	})
}

func (h *authHandler) handleEmailVerifyConfirm(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body verifyConfirmBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	if err := h.svc.ConfirmEmail(r.Context(), p, body.Code, RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"email_verified": true})
}

func (h *authHandler) handleMFAStatus(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	factors, err := h.svc.Factors(r.Context(), p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"factors": factors})
}

func (h *authHandler) handleTOTPStart(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body totpStartBody
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &body); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
			return
		}
	}
	secret, uri, err := h.svc.StartTOTP(r.Context(), p, body.Label, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{
		"kind":        "totp",
		"secret":      secret,
		"otpauth_uri": uri,
		"confirmed":   false,
		"instruction": "scan the URI, then confirm with a current code",
	})
}

func (h *authHandler) handleTOTPConfirm(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body mfaCodeBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	codes, err := h.svc.ConfirmTOTP(r.Context(), p, body.Code, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"confirmed":      true,
		"recovery_codes": codes,
		"notice":         "store these now; they are shown once and each works one time",
	})
}

func (h *authHandler) handleTOTPDisable(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body mfaCodeBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	if err := h.svc.DisableTOTP(r.Context(), p, body.Code, RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) handleMFAStepUp(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body mfaCodeBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	at, err := h.svc.StepUpMFA(r.Context(), p, body.Code, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"mfa_at": at.UTC()})
}

func (h *authHandler) handleGrantRole(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body grantRoleBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	roles, err := h.svc.GrantRole(r.Context(), p, body.UserID, body.Role, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{
		"user_id": body.UserID,
		"role":    body.Role,
		"roles":   roles,
	})
}

func (h *authHandler) handleUserRoles(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	userID := r.PathValue("id")
	if userID == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "user id is required")
		return
	}
	roles, err := h.svc.RolesOf(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":     userID,
		"roles":       roles,
		"permissions": identity.PermissionStrings(roles),
	})
}

// registerMFARoutes mounts verification, MFA and the permission-gated admin
// endpoints. Routes are grouped by the permission they require.
func (h *authHandler) registerMFARoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/email/verify/request",
		rateLimited(h.read, "email_verify", h.requireAuth(h.handleEmailVerifyRequest)))
	mux.HandleFunc("POST /v1/auth/email/verify/confirm",
		rateLimited(h.login, "email_confirm", h.requireAuth(h.handleEmailVerifyConfirm)))

	mux.HandleFunc("GET /v1/auth/mfa", rateLimited(h.read, "mfa", h.requireAuth(h.handleMFAStatus)))
	mux.HandleFunc("POST /v1/auth/mfa/totp/start", rateLimited(h.login, "mfa_start", h.requireAuth(h.handleTOTPStart)))
	mux.HandleFunc("POST /v1/auth/mfa/totp/confirm", rateLimited(h.login, "mfa_confirm", h.requireAuth(h.handleTOTPConfirm)))
	mux.HandleFunc("POST /v1/auth/mfa/totp/disable", rateLimited(h.login, "mfa_disable", h.requireAuth(h.handleTOTPDisable)))
	mux.HandleFunc("POST /v1/auth/mfa/step-up", rateLimited(h.login, "mfa_step_up", h.requireAuth(h.handleMFAStepUp)))

	mux.HandleFunc("POST /v1/admin/roles/grant",
		rateLimited(h.read, "roles_grant",
			h.requirePermission(identity.PermRolesGrant, h.handleGrantRole)))
	mux.HandleFunc("GET /v1/admin/users/{id}/roles",
		rateLimited(h.read, "users_roles",
			h.requirePermission(identity.PermUsersRead, h.handleUserRoles)))
}

// writeError additions for identity errors introduced with MFA and mail.
func (h *authHandler) writeMFAError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, identity.ErrMFARequired):
		w.Header().Set("WWW-Authenticate", `Bearer realm="api", error="mfa_required"`)
		writeProblem(w, r, http.StatusUnauthorized, "mfa_required",
			"this account has a second factor; resend the request with mfa_code")
		return true
	case errors.Is(err, identity.ErrInvalidMFACode):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_mfa_code",
			"the code is invalid, expired or already used")
		return true
	case errors.Is(err, identity.ErrMFAUnavailable), errors.Is(err, identity.ErrMailUnavailable):
		writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
			"this instance is not configured for the requested capability")
		return true
	}
	return h.writePasskeyError(w, r, err)
}
