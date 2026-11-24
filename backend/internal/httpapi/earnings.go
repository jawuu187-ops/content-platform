package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/earnings"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// Creator earnings.
//
// The report is split by data class. `include_simulated=false` (the default) is what a
// payout, balance or tax export must use: it leaves every fake-provider row out. The
// simulated totals are still reported, so the dashboard can say that test data exists and
// was not counted, instead of quietly showing a smaller number than the creator expects.

type earningsHandler struct {
	auth     *authHandler
	earnings *earnings.Service
	logger   *slog.Logger
	read     *limiter
}

func newEarningsHandler(auth *authHandler, svc *earnings.Service, logger *slog.Logger,
	shared *ratelimit.Backend) *earningsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &earningsHandler{
		auth:     auth,
		earnings: svc,
		logger:   logger,
		read:     newLimiter(120, time.Minute, shared),
	}
}

func (h *earningsHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/creator/earnings", rateLimited(h.read, "earnings_read",
		h.auth.requirePermission(identity.PermFinanceRead, h.handleSummary)))
}

func (h *earningsHandler) handleSummary(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	includeSimulated := r.URL.Query().Get("include_simulated") == "true"
	summary, err := h.earnings.Summary(r.Context(), p.UserID, includeSimulated)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "earnings summary failed",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
		internalError(w, r)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"earnings": summary})
}
