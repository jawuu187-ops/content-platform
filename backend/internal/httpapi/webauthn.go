package httpapi

import (
	"errors"
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// Passkey endpoints. The bodies are the WebAuthn JSON model, so a browser
// navigator.credentials call or Android CredentialManager can be wired to these
// routes with field renaming only.

type pkRegisterStartBody struct {
	Label string `json:"label"`
}

type pkRegisterFinishBody struct {
	ChallengeID string                        `json:"challenge_id"`
	Label       string                        `json:"label"`
	Credential  identity.RegistrationResponse `json:"credential"`
}

type pkLoginStartBody struct {
	Email string `json:"email"`
}

type pkLoginFinishBody struct {
	ChallengeID string                     `json:"challenge_id"`
	Credential  identity.AssertionResponse `json:"credential"`
	Device      devicePayload              `json:"device"`
}

func (h *authHandler) handlePasskeyRegisterStart(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body pkRegisterStartBody
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &body); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
			return
		}
	}
	opts, err := h.svc.StartRegistration(r.Context(), p, body.Label)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, opts)
}

func (h *authHandler) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body pkRegisterFinishBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	cred, err := h.svc.FinishRegistration(r.Context(), p, body.ChallengeID, body.Credential,
		body.Label, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"credential": cred})
}

func (h *authHandler) handlePasskeyLoginStart(w http.ResponseWriter, r *http.Request) {
	var body pkLoginStartBody
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &body); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
			return
		}
	}
	opts, err := h.svc.StartLogin(r.Context(), body.Email)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, opts)
}

func (h *authHandler) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	var body pkLoginFinishBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	res, err := h.svc.FinishLogin(r.Context(), body.ChallengeID, body.Credential,
		body.Device.toDomain(r), RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, newAuthResponse(res, h.svc.Now()))
}

func (h *authHandler) handlePasskeyList(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	creds, err := h.svc.Credentials(r.Context(), p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"credentials": creds})
}

func (h *authHandler) handlePasskeyRevoke(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	id := r.PathValue("id")
	if err := h.svc.RevokeCredential(r.Context(), p, id, RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// registerPasskeyRoutes mounts the ceremony endpoints. Login start/finish are
// anonymous by design (the whole point is to authenticate), so they carry the
// stricter login rate limit.
func (h *authHandler) registerPasskeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/webauthn/register/start",
		rateLimited(h.read, "pk_register_start", h.requireAuth(h.handlePasskeyRegisterStart)))
	mux.HandleFunc("POST /v1/auth/webauthn/register/finish",
		rateLimited(h.login, "pk_register_finish", h.requireAuth(h.handlePasskeyRegisterFinish)))
	mux.HandleFunc("POST /v1/auth/webauthn/login/start",
		rateLimited(h.login, "pk_login_start", h.handlePasskeyLoginStart))
	mux.HandleFunc("POST /v1/auth/webauthn/login/finish",
		rateLimited(h.login, "pk_login_finish", h.handlePasskeyLoginFinish))
	mux.HandleFunc("GET /v1/auth/webauthn/credentials",
		rateLimited(h.read, "pk_list", h.requireAuth(h.handlePasskeyList)))
	mux.HandleFunc("DELETE /v1/auth/webauthn/credentials/{id}",
		rateLimited(h.read, "pk_revoke", h.requireAuth(h.handlePasskeyRevoke)))
}

// writePasskeyError maps ceremony failures. A wrong signature, a replayed
// challenge and an unknown credential all answer 401 with distinct codes: the
// client needs to tell "try again" from "your passkey is gone".
func (h *authHandler) writePasskeyError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, identity.ErrChallengeConsumed):
		writeProblem(w, r, http.StatusUnauthorized, "challenge_expired",
			"this ceremony expired or was already completed; start again")
		return true
	case errors.Is(err, identity.ErrCredentialCloned):
		writeProblem(w, r, http.StatusUnauthorized, "credential_counter_regressed",
			"the authenticator signature counter went backwards; the credential may be cloned")
		return true
	case errors.Is(err, identity.ErrInvalidAssertion):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_assertion",
			"the passkey response could not be verified")
		return true
	case errors.Is(err, identity.ErrUnsupportedAlgorithm):
		writeProblem(w, r, http.StatusBadRequest, "unsupported_algorithm",
			"this authenticator uses an algorithm the server does not verify")
		return true
	case errors.Is(err, identity.ErrWebAuthnUnavailable):
		writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
			"passkeys are not configured on this instance")
		return true
	}
	return false
}
