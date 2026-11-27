package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/media"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Media and post surface.
//
// The delivery endpoint is the security boundary: it consumes a single-use
// token, re-runs the content authorization with the account's current roles and
// current verification state, and only then streams bytes. A token that arrives
// after a cancellation or a revocation therefore fails, not succeeds.

type mediaHandler struct {
	auth     *authHandler
	media    *media.Service
	creators *creator.Service
	logger   *slog.Logger
	upload   *limiter
	delivery *limiter
	feed     *limiter
}

func newMediaHandler(auth *authHandler, m *media.Service, c *creator.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *mediaHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &mediaHandler{
		auth:     auth,
		media:    m,
		creators: c,
		logger:   logger,
		upload:   newLimiter(60, time.Hour, shared),
		delivery: newLimiter(240, time.Minute, shared),
		feed:     newLimiter(240, time.Minute, shared),
	}
}

// optionalPrincipal resolves a bearer token when one is present and returns nil
// otherwise. Public reads use it so an anonymous visitor sees public content and
// an entitled visitor sees the same feed with the locks removed.
func (h *mediaHandler) optionalPrincipal(r *http.Request) *identity.Principal {
	header := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return nil
	}
	p, err := h.auth.svc.Authenticate(r.Context(), token)
	if err != nil {
		return nil
	}
	return p
}

func (h *mediaHandler) writeMediaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, media.ErrUnavailable):
		writeProblem(w, r, http.StatusServiceUnavailable, "media_unavailable",
			"media storage is not configured on this instance")
	case errors.Is(err, media.ErrTooLarge):
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "the file exceeds the size limit")
	case errors.Is(err, media.ErrUnsupportedType):
		writeProblem(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"only JPEG, PNG and MP4 files are accepted")
	case errors.Is(err, media.ErrInvalidFile):
		writeProblem(w, r, http.StatusUnprocessableEntity, "invalid_media", "the file content is not valid")
	case errors.Is(err, media.ErrTokenInvalid):
		writeProblem(w, r, http.StatusForbidden, "delivery_denied", "this delivery link is not valid")
	case errors.Is(err, creator.ErrNotFound), errors.Is(err, media.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "the requested item does not exist")
	case errors.Is(err, creator.ErrPaymentRequired):
		writeProblem(w, r, http.StatusPaymentRequired, "payment_required", "this content must be purchased first")
	case errors.Is(err, creator.ErrAgeRequired):
		writeProblem(w, r, http.StatusForbidden, "age_verification_required",
			"age verification is required before this content can be unlocked")
	case errors.Is(err, creator.ErrInvalidInput):
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		h.logger.ErrorContext(r.Context(), "media request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path), slog.Any("error", err))
		internalError(w, r)
	}
}

func (h *mediaHandler) handleUpload(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	if !h.media.Configured() {
		h.writeMediaError(w, r, media.ErrUnavailable)
		return
	}
	// The reader is bounded before validation so a client cannot spend our disk
	// or memory by promising a small upload and sending a large one.
	limit := media.DefaultLimits().VideoBytes
	r.Body = http.MaxBytesReader(w, r.Body, limit+1)
	obj, err := h.media.Ingest(r.Context(), p.UserID, r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.writeMediaError(w, r, media.ErrTooLarge)
			return
		}
		h.writeMediaError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"media": obj})
}

func (h *mediaHandler) handleCreatePost(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creator.PostInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	post, err := h.creators.CreatePost(r.Context(), p, body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"post": post})
}

func (h *mediaHandler) handleUpdatePost(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body creator.PostInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	post, err := h.creators.UpdatePost(r.Context(), p, r.PathValue("id"), body, RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"post": post})
}

func (h *mediaHandler) handleRemovePost(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	if err := h.creators.RemovePost(r.Context(), p, r.PathValue("id"), RequestIDFromContext(r.Context())); err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *mediaHandler) handleFeed(w http.ResponseWriter, r *http.Request) {
	viewer := h.optionalPrincipal(r)
	posts, err := h.creators.Posts(r.Context(), viewer, r.PathValue("handle"))
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "authenticated": viewer != nil})
}

func (h *mediaHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	viewer := h.optionalPrincipal(r)
	post, err := h.creators.PostsByID(r.Context(), viewer, r.PathValue("id"))
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"post": post})
}

type unlockBody struct {
	FanID    string `json:"fan_id"`
	TTLHours int    `json:"ttl_hours"`
}

func (h *mediaHandler) handleUnlock(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	unlock, err := h.creators.UnlockPost(r.Context(), p, r.PathValue("id"), RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"unlock": unlock})
}

func (h *mediaHandler) handleGrantUnlock(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body unlockBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	ttl := time.Duration(body.TTLHours) * time.Hour
	unlock, err := h.creators.GrantPostUnlock(r.Context(), p, r.PathValue("id"), body.FanID, ttl,
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"unlock": unlock})
}

type deliveryBody struct {
	PostID string `json:"post_id"`
}

// handleIssueDelivery authorizes the request and, if allowed, mints a short
// lived single-use link. The link is not an authorization: redemption checks
// again.
// handleContent streams media to an authenticated viewer and supports HTTP Range so
// video players can seek. Authorization is repeated on every request; unlike the
// single-use delivery token, this endpoint is a normal authenticated read.
func (h *mediaHandler) handleContent(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	postID := r.PathValue("postID")
	mediaID := r.PathValue("mediaID")
	has, err := h.creators.PostHasMedia(r.Context(), postID, mediaID)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	if !has {
		writeProblem(w, r, http.StatusNotFound, "not_found", "that media is not part of that post")
		return
	}
	decision, err := h.creators.AuthorizePost(r.Context(), &p, postID)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	if !decision.Allowed {
		noStore(w)
		writeJSON(w, http.StatusForbidden, map[string]any{
			"allowed": false, "reason": decision.Reason,
			"detail": "this account may not access that post",
		})
		return
	}
	obj, err := h.media.ObjectByID(r.Context(), mediaID)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	body, _, err := h.media.Open(r.Context(), obj.StorageKey)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	defer func() { _ = body.Close() }()
	seeker, ok := body.(io.ReadSeeker)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "internal_error",
			"this storage driver cannot serve range requests")
		return
	}
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Disposition", `inline; filename="media"`)
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeContent(w, r, "media", obj.CreatedAt, seeker)
}

func (h *mediaHandler) handleIssueDelivery(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body deliveryBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	mediaID := r.PathValue("id")
	has, err := h.creators.PostHasMedia(r.Context(), body.PostID, mediaID)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	if !has {
		// A media item is only reachable through a post that contains it.
		writeProblem(w, r, http.StatusNotFound, "not_found", "that media is not part of that post")
		return
	}
	decision, err := h.creators.AuthorizePost(r.Context(), &p, body.PostID)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	if !decision.Allowed {
		noStore(w)
		writeJSON(w, http.StatusForbidden, map[string]any{
			"allowed": false, "reason": decision.Reason,
			"detail": "this account may not access that post",
		})
		return
	}
	delivery, err := h.media.Issue(r.Context(), p.UserID, mediaID, body.PostID)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"delivery": delivery})
}

// handleDeliver redeems a token. Order matters: consume first (so a concurrent
// second redemption cannot succeed), then authorize, then stream. A denied
// authorization leaves the token consumed, which is the fail-closed direction.
func (h *mediaHandler) handleDeliver(w http.ResponseWriter, r *http.Request) {
	redeemed, err := h.media.Redeem(r.Context(), r.PathValue("token"))
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	principal, err := h.auth.svc.PrincipalByID(r.Context(), redeemed.ViewerID)
	if err != nil {
		h.writeMediaError(w, r, creator.ErrNotFound)
		return
	}
	decision, err := h.creators.AuthorizePost(r.Context(), &principal, redeemed.PostID)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	if !decision.Allowed {
		noStore(w)
		writeJSON(w, http.StatusForbidden, map[string]any{
			"allowed": false, "reason": decision.Reason,
			"detail": "access was withdrawn before this link was used",
		})
		return
	}
	body, size, err := h.media.Open(r.Context(), redeemed.Object.StorageKey)
	if err != nil {
		h.writeMediaError(w, r, err)
		return
	}
	defer func() { _ = body.Close() }()

	w.Header().Set("Content-Type", redeemed.Object.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", `inline; filename="media"`)
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, body); err != nil {
		h.logger.WarnContext(r.Context(), "media stream interrupted", slog.Any("error", err))
	}
}

func (h *mediaHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/creators/{handle}/posts", rateLimited(h.feed, "feed", h.handleFeed))
	mux.HandleFunc("GET /v1/posts/{id}", rateLimited(h.feed, "post", h.handlePost))
	mux.HandleFunc("GET /v1/media/delivery/{token}", rateLimited(h.delivery, "deliver", h.handleDeliver))

	creatorOnly := func(next func(http.ResponseWriter, *http.Request, identity.Principal)) http.HandlerFunc {
		return h.auth.requirePermission(identity.PermContentPublish, next)
	}
	mux.HandleFunc("POST /v1/media", rateLimited(h.upload, "upload", creatorOnly(h.handleUpload)))
	mux.HandleFunc("POST /v1/posts", rateLimited(h.delivery, "post_create", creatorOnly(h.handleCreatePost)))
	mux.HandleFunc("PATCH /v1/posts/{id}", rateLimited(h.delivery, "post_update", creatorOnly(h.handleUpdatePost)))
	mux.HandleFunc("DELETE /v1/posts/{id}", rateLimited(h.delivery, "post_delete", creatorOnly(h.handleRemovePost)))
	mux.HandleFunc("POST /v1/posts/{id}/unlock", rateLimited(h.delivery, "post_unlock",
		h.auth.requireAuth(h.handleUnlock)))
	mux.HandleFunc("POST /v1/posts/{id}/unlocks", rateLimited(h.delivery, "post_grant",
		h.auth.requirePermission(identity.PermAdminAll, h.handleGrantUnlock)))
	mux.HandleFunc("POST /v1/media/{id}/delivery", rateLimited(h.delivery, "delivery_issue",
		h.auth.requireAuth(h.handleIssueDelivery)))
	mux.HandleFunc("GET /v1/posts/{postID}/media/{mediaID}/content", rateLimited(h.delivery, "delivery_content",
		h.auth.requireAuth(h.handleContent)))
}
