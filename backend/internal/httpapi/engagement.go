package httpapi

import (
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

type commentBody struct {
	Body string `json:"body"`
}

func (h *creatorHandler) handleLike(w http.ResponseWriter, r *http.Request, p identity.Principal, liked bool) {
	post, err := h.creators.SetLike(r.Context(), p, r.PathValue("id"), liked)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"post": post})
}

func (h *creatorHandler) handleBookmark(w http.ResponseWriter, r *http.Request, p identity.Principal, saved bool) {
	if err := h.creators.SetBookmark(r.Context(), p, r.PathValue("id"), saved); err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"bookmarked": saved})
}

func (h *creatorHandler) handleMyBookmarks(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	posts, err := h.creators.BookmarkedPosts(r.Context(), p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

func (h *creatorHandler) handleComments(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	comments, err := h.creators.Comments(r.Context(), p, r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"comments": comments})
}

func (h *creatorHandler) handleAddComment(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	var body commentBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return
	}
	comment, err := h.creators.AddComment(r.Context(), p, r.PathValue("id"), body.Body,
		RequestIDFromContext(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusCreated, map[string]any{"comment": comment})
}

func (h *creatorHandler) handleDeleteComment(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	if err := h.creators.DeleteComment(r.Context(), p, r.PathValue("id"),
		RequestIDFromContext(r.Context())); err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
