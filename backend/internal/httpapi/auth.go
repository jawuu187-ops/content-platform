package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// authHandler owns the credential endpoints. It contains no SQL and no password
// hashing: both live in the identity package, which is tested against a real
// database in CI.
type authHandler struct {
	svc     *identity.Service
	logger  *slog.Logger
	login   *limiter
	refresh *limiter
	read    *limiter
}

func newAuthHandler(svc *identity.Service, logger *slog.Logger) *authHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &authHandler{
		svc:     svc,
		logger:  logger,
		login:   newLimiter(10, time.Minute),  // 10 attempts per minute per IP
		refresh: newLimiter(60, time.Minute),  // rotation is frequent but bounded
		read:    newLimiter(120, time.Minute), // session listing
	}
}

type devicePayload struct {
	Fingerprint string `json:"fingerprint"`
	Platform    string `json:"platform"`
	Name        string `json:"name"`
}

func (d devicePayload) toDomain(r *http.Request) identity.DeviceInfo {
	return identity.DeviceInfo{
		Fingerprint: d.Fingerprint,
		Platform:    d.Platform,
		Name:        d.Name,
		UserAgent:   truncate(r.UserAgent(), 256),
		IP:          clientIP(r),
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		return ""
	}
	if host == "::1" {
		return "127.0.0.1"
	}
	return host
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

type registerRequest struct {
	Email    string        `json:"email"`
	Password string        `json:"password"`
	Device   devicePayload `json:"device"`
}

type loginRequest struct {
	Email    string        `json:"email"`
	Password string        `json:"password"`
	Device   devicePayload `json:"device"`
}

type refreshRequest struct {
	RefreshToken string        `json:"refresh_token"`
	Device       devicePayload `json:"device"`
}

type userDTO struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	Roles         []string  `json:"roles"`
	CreatedAt     time.Time `json:"created_at"`
}

type tokenDTO struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	TokenType        string    `json:"token_type"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type authResponse struct {
	User   userDTO  `json:"user"`
	Tokens tokenDTO `json:"tokens"`
}

func newAuthResponse(res *identity.Result, now time.Time) authResponse {
	roles := res.Principal.Roles
	if roles == nil {
		roles = []string{}
	}
	return authResponse{
		User: userDTO{
			ID:            res.User.ID,
			Email:         res.User.Email,
			EmailVerified: res.User.EmailVerified,
			Roles:         roles,
			CreatedAt:     res.User.CreatedAt,
		},
		Tokens: tokenDTO{
			AccessToken:      res.AccessToken,
			RefreshToken:     res.RefreshToken,
			TokenType:        "Bearer",
			AccessExpiresAt:  now.Add(res.AccessTTL).UTC(),
			RefreshExpiresAt: now.Add(res.RefreshTTL).UTC(),
		},
	}
}

// decodeJSON reads a bounded JSON body and rejects fields the schema does not
// know, so a typo in a client is reported instead of silently ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}

// writeAuthError maps domain errors to transport responses. The mapping is
// exhaustive on purpose: an unmapped error becomes a 500 rather than leaking.
func (h *authHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, identity.ErrWeakPassword):
		writeProblem(w, r, http.StatusBadRequest, "weak_password", err.Error())
	case errors.Is(err, identity.ErrEmailTaken):
		writeProblem(w, r, http.StatusConflict, "email_taken",
			"an account already exists for this email address")
	case errors.Is(err, identity.ErrInvalidCredentials):
		writeProblem(w, r, http.StatusUnauthorized, "invalid_credentials",
			"email or password is incorrect")
	case errors.Is(err, identity.ErrTokenReuse):
		writeProblem(w, r, http.StatusUnauthorized, "session_reuse",
			"this session was revoked because its token was reused; sign in again")
	case errors.Is(err, identity.ErrSessionExpired), errors.Is(err, identity.ErrSessionRevoked):
		writeProblem(w, r, http.StatusUnauthorized, "session_ended",
			"the session is no longer valid; sign in again")
	case errors.Is(err, identity.ErrAccountDisabled):
		writeProblem(w, r, http.StatusForbidden, "account_disabled",
			"this account cannot sign in")
	case errors.Is(err, identity.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "the session does not exist")
	default:
		h.logger.ErrorContext(r.Context(), "identity request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path), slog.Any("error", err))
		internalError(w, r)
	}
}

// noStore marks credential responses as uncacheable.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func (h *authHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	res, err := h.svc.Register(r.Context(), req.Email, req.Password, req.Device.toDomain(r),
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	w.Header().Set("Location", "/v1/auth/sessions/"+res.Principal.SessionID)
	writeJSON(w, http.StatusCreated, newAuthResponse(res, time.Now().UTC()))
}

func (h *authHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	res, err := h.svc.Login(r.Context(), req.Email, req.Password, req.Device.toDomain(r),
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, newAuthResponse(res, time.Now().UTC()))
}

func (h *authHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	res, err := h.svc.Refresh(r.Context(), req.RefreshToken, req.Device.toDomain(r),
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, newAuthResponse(res, time.Now().UTC()))
}

func (h *authHandler) handleLogout(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	if err := h.svc.Logout(r.Context(), p, RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) handleSessions(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	sessions, err := h.svc.Sessions(r.Context(), p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if sessions == nil {
		sessions = []identity.Session{}
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *authHandler) handleRevokeSession(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "session id is required")
		return
	}
	if err := h.svc.RevokeSession(r.Context(), p, id, RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) handleMe(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        p.UserID,
		"session_id":     p.SessionID,
		"email_verified": p.EmailVerified,
		"roles":          rolesOrEmpty(p.Roles),
	})
}

func rolesOrEmpty(roles []string) []string {
	if roles == nil {
		return []string{}
	}
	return roles
}

// requireAuth resolves the bearer token into a principal and rejects the request
// otherwise. It is the only way a handler receives an identity.
func (h *authHandler) requireAuth(next func(http.ResponseWriter, *http.Request, identity.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		scheme, token, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
			writeProblem(w, r, http.StatusUnauthorized, "missing_credentials",
				"an Authorization: Bearer token is required")
			return
		}
		principal, err := h.svc.Authenticate(r.Context(), token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="api", error="invalid_token"`)
			h.writeError(w, r, err)
			return
		}
		next(w, r, *principal)
	}
}

// registerRoutes mounts the auth surface on the mux. Keeping this in one place
// makes the public contract visible at a glance.
func (h *authHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/register", rateLimited(h.login, "register", h.handleRegister))
	mux.HandleFunc("POST /v1/auth/login", rateLimited(h.login, "login", h.handleLogin))
	mux.HandleFunc("POST /v1/auth/refresh", rateLimited(h.refresh, "refresh", h.handleRefresh))
	mux.HandleFunc("POST /v1/auth/logout", rateLimited(h.read, "logout", h.requireAuth(
		func(w http.ResponseWriter, r *http.Request, p identity.Principal) { h.handleLogout(w, r, p) })))
	mux.HandleFunc("GET /v1/auth/sessions", rateLimited(h.read, "sessions", h.requireAuth(
		func(w http.ResponseWriter, r *http.Request, p identity.Principal) { h.handleSessions(w, r, p) })))
	mux.HandleFunc("DELETE /v1/auth/sessions/{id}", rateLimited(h.read, "sessions", h.requireAuth(
		func(w http.ResponseWriter, r *http.Request, p identity.Principal) { h.handleRevokeSession(w, r, p) })))
	mux.HandleFunc("GET /v1/auth/me", rateLimited(h.read, "me", h.requireAuth(
		func(w http.ResponseWriter, r *http.Request, p identity.Principal) { h.handleMe(w, r, p) })))
}
