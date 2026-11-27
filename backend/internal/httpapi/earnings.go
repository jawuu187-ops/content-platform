package httpapi

import (
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
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
	// export is slower and rarer than a dashboard read: it scans a period and
	// writes a file.
	export *limiter
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
		export:   newLimiter(10, time.Hour, shared),
	}
}

func (h *earningsHandler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/creator/earnings", rateLimited(h.read, "earnings_read",
		h.auth.requirePermission(identity.PermFinanceRead, h.handleSummary)))
	mux.HandleFunc("GET /v1/creator/earnings/export", rateLimited(h.export, "earnings_export",
		h.auth.requirePermission(identity.PermFinanceRead, h.handleExport)))
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

// handleExport returns the creator's ledger movements for a period as CSV.
//
// The file is built from the ledger and carries refunds and payout reservations
// as negative lines, so the net column is the creator's actual movement and not
// only their sales. Dates are inclusive at both ends and interpreted as UTC days:
// a calendar year is from=2026-01-01 to=2026-12-31. One currency per file,
// because a tax figure is in one currency.
func (h *earningsHandler) handleExport(w http.ResponseWriter, r *http.Request, p identity.Principal) {
	q, err := parseExportQuery(r, p.UserID)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_export", err.Error())
		return
	}
	exp, err := h.earnings.Export(r.Context(), q)
	switch {
	case errors.Is(err, earnings.ErrInvalidExport):
		writeProblem(w, r, http.StatusBadRequest, "invalid_export", err.Error())
		return
	case errors.Is(err, earnings.ErrExportTooLarge):
		// Refusing is the point: a silently truncated year would be a tax return
		// that is quietly short.
		writeProblem(w, r, http.StatusBadRequest, "export_too_large",
			"this period holds too many movements for one export; request a narrower period")
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "earnings export failed",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
		internalError(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="earnings-%s.csv"`,
		exp.From.Format("2006-01-02")+"_"+exp.To.AddDate(0, 0, -1).Format("2006-01-02")))
	w.Header().Set("X-Export-Currency", exp.Currency)
	w.Header().Set("X-Export-Rows", strconv.Itoa(len(exp.Rows)))
	w.Header().Set("X-Export-Gross-Minor", strconv.FormatInt(exp.GrossMinor, 10))
	w.Header().Set("X-Export-Net-Minor", strconv.FormatInt(exp.NetMinor, 10))
	w.Header().Set("X-Export-Excluded-Simulated", strconv.Itoa(exp.ExcludedSimulated))
	noStore(w)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"occurred_at", "kind", "purpose", "reference", "currency",
		"gross_minor", "net_minor", "data_class",
	})
	for _, row := range exp.Rows {
		dataClass := "real"
		if row.Simulated {
			dataClass = "test"
		}
		_ = cw.Write([]string{
			row.OccurredAt.UTC().Format(time.RFC3339),
			row.Kind,
			row.Purpose,
			row.Reference,
			row.Currency,
			strconv.FormatInt(row.GrossMinor, 10),
			strconv.FormatInt(row.NetMinor, 10),
			dataClass,
		})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		// The status line is already sent, so the only honest report is the log.
		h.logger.ErrorContext(r.Context(), "write earnings export",
			slog.String("request_id", RequestIDFromContext(r.Context())), slog.Any("error", err))
	}
}

// parseExportQuery reads from/to as UTC days. The end date is inclusive, so it is
// advanced by one day to become the exclusive bound the service uses.
func parseExportQuery(r *http.Request, creatorID string) (earnings.ExportQuery, error) {
	now := time.Now().UTC()
	q := earnings.ExportQuery{
		CreatorID: creatorID,
		Currency:  r.URL.Query().Get("currency"),
		From:      time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC),
		To:        now,
	}
	if q.Currency == "" {
		q.Currency = "USD"
	}
	if v := r.URL.Query().Get("from"); v != "" {
		from, err := time.Parse("2006-01-02", v)
		if err != nil {
			return q, fmt.Errorf("%w: from must be a date like 2026-01-01", earnings.ErrInvalidExport)
		}
		q.From = from
	}
	if v := r.URL.Query().Get("to"); v != "" {
		to, err := time.Parse("2006-01-02", v)
		if err != nil {
			return q, fmt.Errorf("%w: to must be a date like 2026-12-31", earnings.ErrInvalidExport)
		}
		q.To = to.AddDate(0, 0, 1)
	}
	q.IncludeSimulated = r.URL.Query().Get("include_simulated") == "true"
	return q, nil
}
