// Package earnings reports what a creator has been paid, split by whether the money was
// real or simulated.
//
// This split is the whole point of the package. The platform runs a fake provider for
// development and demonstrations (D-062..D-065), so the same creator account can hold
// hundreds of successful simulated tips and subscriptions next to a real one. Any payout,
// balance or tax figure that summed the two would be wrong by an arbitrary amount, and the
// error would be invisible. Every query here is therefore grouped by `simulated` and every
// caller has to decide, explicitly, whether it wants test data.
//
// Gross is read from payment intents (what a fan was charged). Net is read from the ledger
// (what the creator is owed after the platform share). They are different questions and are
// never derived from one another.
package earnings

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Bucket is one purpose in one data class: real or simulated.
type Bucket struct {
	Purpose    string `json:"purpose"`
	Simulated  bool   `json:"simulated"`
	GrossMinor int64  `json:"gross_minor"`
	Count      int    `json:"count"`
}

// Summary is the creator-facing earnings picture.
type Summary struct {
	CreatorID string `json:"creator_id"`
	Currency  string `json:"currency"`
	// IncludeSimulated records what the caller asked for, so a report can never be
	// mistaken for the other one.
	IncludeSimulated bool     `json:"include_simulated"`
	Buckets          []Bucket `json:"buckets"`
	// Net figures come from the ledger, split so a payout or tax query takes one side.
	// NetSimulatedMinor is always reported, even when simulated buckets are excluded,
	// so a UI can say "test data exists and is not included".
	NetLiveMinor      int64 `json:"net_live_minor"`
	NetSimulatedMinor int64 `json:"net_simulated_minor"`
	LiveCount         int   `json:"live_count"`
	SimulatedCount    int   `json:"simulated_count"`
}

type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Summary totals a creator's settled sales and tips.
//
// includeSimulated=false is the correct default for anything that will be paid out or
// reported to a tax authority; true is for the creator's own dashboard, where the test
// numbers are labelled as test numbers.
func (s *Service) Summary(ctx context.Context, creatorID string, includeSimulated bool) (Summary, error) {
	if creatorID == "" {
		return Summary{}, fmt.Errorf("earnings: creator id is required")
	}
	out := Summary{CreatorID: creatorID, IncludeSimulated: includeSimulated, Buckets: []Bucket{}}

	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(currency)::text, 'USD')
		FROM payments.intents WHERE creator_id = $1::uuid`, creatorID).Scan(&out.Currency); err != nil {
		return Summary{}, fmt.Errorf("earnings: read currency: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT simulated, purpose, COALESCE(SUM(amount_minor), 0)::bigint, COUNT(*)::int
		FROM payments.intents
		WHERE creator_id = $1::uuid AND status = 'succeeded'
		  AND purpose IN ('subscription', 'ppv', 'tip')
		GROUP BY simulated, purpose
		ORDER BY simulated, purpose`, creatorID)
	if err != nil {
		return Summary{}, fmt.Errorf("earnings: gross by purpose: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var bucket Bucket
		if err := rows.Scan(&bucket.Simulated, &bucket.Purpose, &bucket.GrossMinor, &bucket.Count); err != nil {
			return Summary{}, fmt.Errorf("earnings: scan bucket: %w", err)
		}
		if bucket.Simulated {
			out.SimulatedCount += bucket.Count
			if !includeSimulated {
				continue
			}
		} else {
			out.LiveCount += bucket.Count
		}
		out.Buckets = append(out.Buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("earnings: iterate buckets: %w", err)
	}

	// The payable account holds the creator's net: credits for sales, debits for refunds
	// and payouts. Summing it by data class keeps real money and test money apart even
	// when they share one account code.
	netRows, err := s.pool.Query(ctx, `
		SELECT t.simulated, COALESCE(SUM(-e.amount_minor), 0)::bigint
		FROM ledger.entries e
		JOIN ledger.transactions t ON t.id = e.transaction_id
		JOIN ledger.accounts a ON a.id = e.account_id
		WHERE a.code = $1
		GROUP BY t.simulated`, "creator:"+creatorID+":payable")
	if err != nil {
		return Summary{}, fmt.Errorf("earnings: net by data class: %w", err)
	}
	defer netRows.Close()
	for netRows.Next() {
		var simulated bool
		var net int64
		if err := netRows.Scan(&simulated, &net); err != nil {
			return Summary{}, fmt.Errorf("earnings: scan net: %w", err)
		}
		if simulated {
			out.NetSimulatedMinor = net
		} else {
			out.NetLiveMinor = net
		}
	}
	if err := netRows.Err(); err != nil {
		return Summary{}, fmt.Errorf("earnings: iterate net: %w", err)
	}
	return out, nil
}
