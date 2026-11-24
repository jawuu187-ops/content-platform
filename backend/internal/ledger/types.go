// Package ledger is the financial system of record.
//
// Money moves only by posting balanced double-entry transactions. Rows are
// append-only and the database refuses UPDATE and DELETE, so a mistake is
// corrected by a compensating transaction rather than by editing history.
package ledger

import (
	"errors"
	"time"
)

// Sign convention: a debit is positive, a credit is negative. A transaction's
// entries must sum to zero, which the database also enforces at commit.
const (
	Debit  = int64(1)
	Credit = int64(-1)
)

// Account kinds.
const (
	KindAsset     = "asset"
	KindLiability = "liability"
	KindRevenue   = "revenue"
	KindExpense   = "expense"
)

// Owner kinds.
const (
	OwnerPlatform = "platform"
	OwnerUser     = "user"
	OwnerCreator  = "creator"
)

// Transaction kinds.
const (
	TxnSubscription = "subscription"
	TxnPPV          = "ppv"
	TxnTip          = "tip"
	TxnRefund       = "refund"
	TxnPayout       = "payout"
	TxnAdjustment   = "adjustment"
)

// Well-known account codes.
const (
	AccountProviderClearing = "platform:provider_clearing"
	AccountRevenue          = "platform:revenue"
	AccountRefundExpense    = "platform:refund_expense"
)

// Domain errors.
var (
	ErrUnavailable = errors.New("ledger unavailable")
	ErrInvalid     = errors.New("invalid ledger entry")
	ErrDuplicate   = errors.New("ledger transaction already posted")
)

// Account is one balance line.
type Account struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Kind      string    `json:"kind"`
	OwnerType string    `json:"owner_type"`
	OwnerID   string    `json:"owner_id,omitempty"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"created_at"`
}

// Entry is one leg of a transaction.
type Entry struct {
	AccountCode string
	AmountMinor int64
}

// Posting is a complete, balanced transaction.
type Posting struct {
	Kind      string
	Reference string
	Currency  string
	Memo      string
	Entries   []Entry
	// Simulated marks a posting that came from a test payment. It is stored on the
	// transaction row so balances and payouts can exclude it. False for real money.
	Simulated bool
}

// Balance is a summed account balance in minor units.
type Balance struct {
	Code        string `json:"code"`
	Currency    string `json:"currency"`
	AmountMinor int64  `json:"amount_minor"`
}
