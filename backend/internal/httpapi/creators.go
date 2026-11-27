package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Creator surface: profiles, tiers, subscriptions and the access decision that
// gates paid content. Public reads are anonymous; every write needs a session,
// and tier management needs the creator permission rather than "am I a creator",
// so the role matrix stays the single authority.

type creatorHandler struct {
	auth     *authHandler
	creators *creator.Service
	logger   *slog.Logger
	read     *limiter
	write    *limiter
}

func newCreatorHandler(auth *authHandler, svc *creator.Service, logger *slog.Logger, shared *ratelimit.Backend) *creatorHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &creatorHandler{
		auth:     auth,
		creators: svc,
		logger:   logger,
		read:     newLimiter(120, time.Minute, shared),
		write:    newLimiter(30, time.Minute, shared),
	}
}

type subscribeBody struct {
	TierID string `json:"tier_id"`
}

type listCreatorsBody struct{}

func (h *creatorHandler) handleListCreators(w http.ResponseWriter, r *http.Request) {
	limit := 24
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	profiles, err := h.creators.ListPublished(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"creators": profiles})
}

func (h *creatorHandler) handleMyProfile(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	profile, tiers, err := h.creators.MyProfile(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "tiers": tiers})
}

type grantEntitlementBody struct {
	FanID   string `json:"fan_id"`
	Scope   string `json:"scope"`
	TTLDays int    `json:"ttl_days"`
}

func (h *creatorHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, creator.ErrNotFound), errors.Is(err, creator.ErrNotPublished):
		writeProblem(w, r, http.StatusNotFound, "not_found", "the creator or resource does not exist")
	case errors.Is(err, creator.ErrHandleTaken):
		writeProblem(w, r, http.StatusConflict, "handle_taken", "that handle is already in use")
	case errors.Is(err, creator.ErrAlreadySubscribed):
		writeProblem(w, r, http.StatusConflict, "already_subscribed", "an active subscription already exists")
	case errors.Is(err, creator.ErrTierInactive):
		writeProblem(w, r, http.StatusConflict, "tier_inactive", "this tier is not accepting new subscribers")
	case errors.Is(err, creator.ErrSelfSubscription):
		writeProblem(w, r, http.StatusBadRequest, "self_subscription", "you cannot subscribe to your own profile")
	case errors.Is(err, creator.ErrPaymentRequired):
		writeProblem(w, r, http.StatusPaymentRequired, "payment_required", "a payment method is required to activate this subscription")
	case errors.Is(err, creator.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, creator.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "insufficient_permissions", "this account cannot perform that action")
	default:
		h.logger.ErrorContext(r.Context(), "creator request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path), slog.Any("error", err))
		internalError(w, r)
	}
}

func (h *creatorHandler) handleCreateProfile(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creator.ProfileInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	profile, err := h.creators.CreateProfile(r.Context(), p, body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	roles, err := h.auth.svc.BecomeCreator(r.Context(), p, RequestIDFromContext(r.Context()))
	if err != nil {
		// The profile exists; the role grant is retryable and reported so the
		// client does not believe it can publish yet.
		h.logger.ErrorContext(r.Context(), "creator role grant failed after profile creation",
			"user_id", p.UserID, slog.Any("error", err))
		writeJSON(w, http.StatusCreated, map[string]any{
			"profile": profile,
			"warning": "profile created but the creator role could not be granted; retry",
		})
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"profile": profile, "roles": roles})
}

func (h *creatorHandler) handlePatchProfile(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creator.ProfileInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	profile, err := h.creators.UpdateProfile(r.Context(), p, body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
}

func (h *creatorHandler) handlePublicProfile(w http.ResponseWriter, r *http.Request) {
	profile, tiers, err := h.creators.PublicProfile(r.Context(), r.PathValue("handle"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile": profile,
		"tiers":   tiers,
	})
}

func (h *creatorHandler) handlePublicTiers(w http.ResponseWriter, r *http.Request) {
	tiers, err := h.creators.TiersOf(r.Context(), r.PathValue("handle"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tiers": tiers})
}

func (h *creatorHandler) handleCreateTier(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creator.TierInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	tier, err := h.creators.CreateTier(r.Context(), p, body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"tier": tier})
}

func (h *creatorHandler) handlePatchTier(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creator.TierInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	tier, err := h.creators.UpdateTier(r.Context(), p, r.PathValue("id"), body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"tier": tier})
}

func (h *creatorHandler) handleDeleteTier(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	if err := h.creators.DeactivateTier(r.Context(), p, r.PathValue("id"), RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *creatorHandler) handleSubscribe(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body subscribeBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	sub, err := h.creators.Subscribe(r.Context(), p, r.PathValue("handle"), body.TierID, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	if sub.Status == creator.StatusPending {
		// Honest answer: no payment provider is wired yet, so nothing is charged
		// and nothing is unlocked.
		writeJSON(w, http.StatusAccepted, map[string]any{
			"subscription":     sub,
			"payment_required": true,
			"notice":           "the subscription is pending; it is activated after a successful payment",
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"subscription": sub})
}

func (h *creatorHandler) handleAccess(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = creator.ScopeSubscriber
	}
	decision, err := h.creators.CheckAccess(r.Context(), &p, r.PathValue("handle"), scope)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"access": decision})
}

func (h *creatorHandler) handleMySubscriptions(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	subs, err := h.creators.Subscriptions(r.Context(), p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
}

func (h *creatorHandler) handleCancel(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	sub, err := h.creators.CancelSubscription(r.Context(), p, r.PathValue("id"), RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"subscription": sub})
}

// handleGrantEntitlement is the admin path that gives a fan direct access, used
// by tooling and by the pay-per-view flow once it exists.
func (h *creatorHandler) handleGrantEntitlement(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body grantEntitlementBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	ttl := time.Duration(body.TTLDays) * 24 * time.Hour
	if err := h.creators.GrantEntitlement(r.Context(), p, r.PathValue("handle"), body.FanID, body.Scope, ttl,
		RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"granted": true})
}

func (h *creatorHandler) requireCreator(perm identity.Permission,
	next func(http.ResponseWriter, *http.Request, identity.Principal)) http.HandlerFunc {
	return h.auth.requirePermission(perm, next)
}

func (h *creatorHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/creators", rateLimited(h.read, "creator_list", h.handleListCreators))
	mux.HandleFunc("GET /v1/creators/me", rateLimited(h.read, "creator_me",
		h.auth.requireAuth(h.handleMyProfile)))
	mux.HandleFunc("POST /v1/creators", rateLimited(h.write, "creator_create", h.auth.requireAuth(h.handleCreateProfile)))
	mux.HandleFunc("PATCH /v1/creators/me", rateLimited(h.write, "creator_patch",
		h.requireCreator(identity.PermContentPublish, h.handlePatchProfile)))
	mux.HandleFunc("GET /v1/creators/{handle}", rateLimited(h.read, "creator_read", h.handlePublicProfile))
	mux.HandleFunc("GET /v1/creators/{handle}/tiers", rateLimited(h.read, "creator_tiers", h.handlePublicTiers))
	mux.HandleFunc("POST /v1/creators/{handle}/subscribe", rateLimited(h.write, "creator_subscribe",
		h.auth.requireAuth(h.handleSubscribe)))
	mux.HandleFunc("GET /v1/creators/{handle}/access", rateLimited(h.read, "creator_access",
		h.auth.requireAuth(h.handleAccess)))

	mux.HandleFunc("POST /v1/creators/me/tiers", rateLimited(h.write, "tier_create",
		h.requireCreator(identity.PermContentPublish, h.handleCreateTier)))
	mux.HandleFunc("PATCH /v1/creators/me/tiers/{id}", rateLimited(h.write, "tier_patch",
		h.requireCreator(identity.PermContentPublish, h.handlePatchTier)))
	mux.HandleFunc("DELETE /v1/creators/me/tiers/{id}", rateLimited(h.write, "tier_delete",
		h.requireCreator(identity.PermContentPublish, h.handleDeleteTier)))
	mux.HandleFunc("POST /v1/creators/{handle}/entitlements", rateLimited(h.write, "entitlement_grant",
		h.requireCreator(identity.PermAdminAll, h.handleGrantEntitlement)))

	mux.HandleFunc("GET /v1/me/subscriptions", rateLimited(h.read, "my_subs",
		h.auth.requireAuth(h.handleMySubscriptions)))
	mux.HandleFunc("POST /v1/subscriptions/{id}/cancel", rateLimited(h.write, "sub_cancel",
		h.auth.requireAuth(h.handleCancel)))

	h.registerStatsRoutes(mux)
}
