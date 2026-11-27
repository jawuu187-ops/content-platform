package earnings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/money"
)

// Tax export.
//
// The export answers a different question from Summary: not "what did I earn",
// but "show me every movement of my money between these dates, in a form an
// accountant or a tax authority can read". It is therefore built from the ledger
// — the system of record — and it includes refunds and payout reservations, which
// reduce a creator's income and which a report that only counted sales would
// leave out.
//
// Two properties are deliberate:
//
//   - One currency per export. A tax figure is in one currency; a CSV that mixed
//     USD and EUR in one net column would be wrong in a way nobody notices.
//   - It refuses rather than truncates. A row limit that silently cut the tail of
//     a year is a tax return that is quietly short, so exceeding the limit is an
//     error asking for a narrower period.
//
// Simulated money is excluded by default and its exclusion is reported, exactly
// as Summary does (D-070): a test payment that vanished from the export without a
// trace would look like missing income.

var (
	// ErrInvalidExport covers a malformed range, an unsupported currency or an
	// empty creator id.
	ErrInvalidExport = errors.New("earnings: invalid export request")
	// ErrExportTooLarge means the period holds more rows than one export may
	// carry. The caller narrows the period instead of receiving a partial file.
	ErrExportTooLarge = errors.New("earnings: export period is too large")
)

const (
	// DefaultMaxExportRows bounds one export. A year of a busy creator is far
	// below this; a row cap still exists so one request cannot build a file in
	// memory without limit.
	DefaultMaxExportRows = 5000
	// MaxExportSpanDays bounds the period. 400 covers any calendar or fiscal year
	// including a leap day, and stops "from 2000" from becoming a full export.
	MaxExportSpanDays = 400
)

// ExportQuery is one export request. From is inclusive and To is exclusive, so a
// calendar year is [1 January, 1 January next year).
type ExportQuery struct {
	CreatorID string
	Currency  string
	From      time.Time
	To        time.Time
	// IncludeSimulated must be explicit. A tax export passes false.
	IncludeSimulated bool
}

// ExportRow is one ledger movement that touched the creator's payable account.
//
// GrossMinor is what a fan was charged and is only set on rows backed by a
// payment intent, which are the income rows. NetMinor is the creator's actual
// movement: positive for a sale or tip, negative for a refund or a payout
// reservation. The two are never derived from one another.
type ExportRow struct {
	OccurredAt time.Time
	Kind       string
	Purpose    string
	Reference  string
	Currency   string
	GrossMinor int64
	NetMinor   int64
	Simulated  bool
}

// Export is a complete, self-describing statement.
type Export struct {
	CreatorID        string
	Currency         string
	From             time.Time
	To               time.Time
	IncludeSimulated bool
	Rows             []ExportRow
	// GrossMinor totals the income rows; NetMinor totals every row, so refunds
	// and payouts are already subtracted.
	GrossMinor int64
	NetMinor   int64
	// ExcludedSimulated counts the simulated rows the period held when they were
	// left out, so the file states what it does not contain.
	ExcludedSimulated int
}

// Export builds the statement for one creator, one currency and one period.
func (s *Service) Export(ctx context.Context, q ExportQuery) (Export, error) {
	if q.CreatorID == "" {
		return Export{}, fmt.Errorf("%w: a creator is required", ErrInvalidExport)
	}
	if _, ok := money.AllowedCurrencies[money.Currency(q.Currency)]; !ok {
		return Export{}, fmt.Errorf("%w: currency must be one of USD, EUR, GBP", ErrInvalidExport)
	}
	if q.From.IsZero() || q.To.IsZero() || !q.To.After(q.From) {
		return Export{}, fmt.Errorf("%w: the period must have a start before its end", ErrInvalidExport)
	}
	if q.To.Sub(q.From) > MaxExportSpanDays*24*time.Hour {
		return Export{}, fmt.Errorf("%w: the period may span at most %d days", ErrInvalidExport, MaxExportSpanDays)
	}

	out := Export{
		CreatorID:        q.CreatorID,
		Currency:         q.Currency,
		From:             q.From.UTC(),
		To:               q.To.UTC(),
		IncludeSimulated: q.IncludeSimulated,
		Rows:             []ExportRow{},
	}

	// One row per ledger transaction that touched this creator's payable account.
	// The intent join is what supplies the gross figure, and it deliberately does
	// not match refund and payout references: those rows have a net movement but
	// no sale behind them.
	rows, err := s.pool.Query(ctx, `
		SELECT t.created_at, t.kind, COALESCE(i.purpose, ''), t.reference, t.currency,
		       COALESCE(i.amount_minor, 0)::bigint, COALESCE(-SUM(e.amount_minor), 0)::bigint,
		       t.simulated
		FROM ledger.transactions t
		JOIN ledger.entries e ON e.transaction_id = t.id
		JOIN ledger.accounts a ON a.id = e.account_id
		LEFT JOIN payments.intents i ON i.id::text = t.reference
		WHERE a.code = $1
		  AND t.currency = $2
		  AND t.created_at >= $3 AND t.created_at < $4
		  AND ($5 OR NOT t.simulated)
		GROUP BY t.id, t.created_at, t.kind, t.reference, t.currency, t.simulated, i.purpose, i.amount_minor
		ORDER BY t.created_at, t.id
		LIMIT $6`,
		"creator:"+q.CreatorID+":payable", q.Currency, out.From, out.To, q.IncludeSimulated,
		s.maxExportRows+1)
	if err != nil {
		return Export{}, fmt.Errorf("earnings: export rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r ExportRow
		if err := rows.Scan(&r.OccurredAt, &r.Kind, &r.Purpose, &r.Reference, &r.Currency,
			&r.GrossMinor, &r.NetMinor, &r.Simulated); err != nil {
			return Export{}, fmt.Errorf("earnings: scan export row: %w", err)
		}
		out.Rows = append(out.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return Export{}, fmt.Errorf("earnings: iterate export rows: %w", err)
	}
	if len(out.Rows) > s.maxExportRows {
		// The +1 in the query is how "too many" is detected without counting the
		// whole period twice.
		return Export{}, fmt.Errorf("%w: more than %d rows; narrow the period",
			ErrExportTooLarge, s.maxExportRows)
	}

	for _, r := range out.Rows {
		out.NetMinor += r.NetMinor
		if r.Purpose != "" {
			out.GrossMinor += r.GrossMinor
		}
	}

	if !q.IncludeSimulated {
		if err := s.pool.QueryRow(ctx, `
			SELECT COUNT(*)::int
			FROM ledger.transactions t
			JOIN ledger.entries e ON e.transaction_id = t.id
			JOIN ledger.accounts a ON a.id = e.account_id
			WHERE a.code = $1 AND t.currency = $2
			  AND t.created_at >= $3 AND t.created_at < $4
			  AND t.simulated`,
			"creator:"+q.CreatorID+":payable", q.Currency, out.From, out.To,
		).Scan(&out.ExcludedSimulated); err != nil {
			return Export{}, fmt.Errorf("earnings: count excluded test rows: %w", err)
		}
	}
	return out, nil
}
