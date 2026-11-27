package httpapi

import (
	"net/http"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// Studio audience statistics.
//
// The creator id is the authenticated account, never a query parameter: there is
// no path by which one account reads another creator's audience. The numbers are
// counts of live paid memberships and paid unlocks (see creator.Service.Stats),
// so support grants and disposable fixtures are not mistaken for demand.
func (h *creatorHandler) registerStatsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/creator/stats", rateLimited(h.read, "creator_stats",
		h.auth.requirePermission(identity.PermFinanceRead, h.handleStats)))
}

func (h *creatorHandler) handleStats(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	stats, err := h.creators.Stats(r.Context(), p.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats})
}
