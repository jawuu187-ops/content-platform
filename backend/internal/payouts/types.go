// Package payouts turns a creator's ledger balance into a withdrawal they can
// ask for, and a finance operator can approve or refuse.
//
// The ledger is the only source of truth for what a creator is owed. A request
// does not create money and an approval does not guess at it: the service reads
// the creator's payable balance inside the same transaction that reserves the
// amount, so two approvals can never spend the same balance, and the reservation
// is a balanced ledger transaction (debit the creator payable, credit the payout
// clearing account) like every other movement of money on this platform.
//
// Simulated money is never payable. Every balance read here passes
// includeSimulated=false, which is the split D-070 requires: the fake provider
// writes real ledger rows, so a payout that summed both would leave the platform
// short by whatever the demonstrations earned.
package payouts

import (
	"errors"
	"time"
)

// Domain errors. The HTTP layer maps each to one stable problem code.
var (
	ErrInvalidInput = errors.New("payouts: invalid input")
	ErrNotFound     = errors.New("payouts: not found")
	ErrForbidden    = errors.New("payouts: not allowed")
	// ErrConflict means the request is not in a state the asked-for transition
	// starts from (already decided, or already cancelled).
	ErrConflict = errors.New("payouts: status transition not allowed")
	// ErrInsufficientBalance means the creator's live payable balance is below
	// the requested amount. It is separate from ErrInvalidInput because the
	// request is well formed; the money is simply not there.
	ErrInsufficientBalance = errors.New("payouts: insufficient balance")
)

// Request statuses, mirrored by a CHECK constraint.
const (
	StatusRequested = "requested"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusCancelled = "cancelled"
	StatusPaid      = "paid"
	StatusFailed    = "failed"
)

// Actions recorded in the trail. They are verbs, because the same target status
// can be reached from more than one place and the difference is what matters.
const (
	ActionRequest = "request"
	ActionApprove = "approve"
	ActionReject  = "reject"
	ActionCancel  = "cancel"
	ActionSettle  = "settle"
)

// Payout methods. This is a label on the request, not stored coordinates: bank
// or wallet details need a tokenisation and retention story before this platform
// keeps them.
const (
	MethodManual = "manual"
	MethodBank   = "bank"
	MethodWallet = "wallet"
)

const (
	// MinAmountMinor and MaxAmountMinor are repeated by a database CHECK. The
	// floor keeps a payout from costing more in operator time than it moves.
	MinAmountMinor = 1_000
	MaxAmountMinor = 10_000_000

	MaxNoteLength         = 500
	MaxDecisionNoteLength = 2000
	// MinDecisionNoteLength is why a refusal is refused: a creator who is told
	// "no" is owed a reason a support ticket can quote.
	MinDecisionNoteLength = 3

	MaxListSize = 100
)

// Methods lists the accepted method labels.
func Methods() []string { return []string{MethodManual, MethodBank, MethodWallet} }

// Input is a creator's request. It carries no creator id: the caller is the
// creator, so there is no field with which to ask for someone else's money.
type Input struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`
	Note        string `json:"note"`
}

// Request is one withdrawal and its decision.
type Request struct {
	ID            string `json:"id"`
	CreatorID     string `json:"creator_id"`
	AmountMinor   int64  `json:"amount_minor"`
	Currency      string `json:"currency"`
	Method        string `json:"method"`
	Status        string `json:"status"`
	RequestedBy   string `json:"requested_by"`
	DecidedBy     string `json:"decided_by,omitempty"`
	Note          string `json:"note,omitempty"`
	DecisionNote  string `json:"decision_note,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`

	DecidedAt *time.Time `json:"decided_at,omitempty"`
	PaidAt    *time.Time `json:"paid_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// Events is filled by Detail and left empty by the list calls, so a queue
	// render is one query rather than one per row.
	Events []Event `json:"events,omitempty"`
}

// Event is one line of a request's trail.
type Event struct {
	ID         int64     `json:"id"`
	Action     string    `json:"action"`
	FromStatus string    `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	Note       string    `json:"note,omitempty"`
	ActorID    string    `json:"actor_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Balance is what a creator may ask for, in the currency they were paid in.
type Balance struct {
	CreatorID string `json:"creator_id"`
	Currency  string `json:"currency"`
	// AvailableMinor is the live payable balance: credits for settled sales and
	// tips, debits for refunds and approved payouts, test money excluded.
	AvailableMinor int64 `json:"available_minor"`
	// PendingMinor is money already asked for but not yet reserved, so it is
	// still inside AvailableMinor. It is reported for the same reason simulated
	// totals are: a number that silently excludes something reads as missing
	// money. What a creator may ask for now is AvailableMinor - PendingMinor.
	PendingMinor int64 `json:"pending_minor"`
	// MinimumMinor is the smallest request the server accepts, so the UI can
	// show the floor before someone types an amount.
	MinimumMinor int64 `json:"minimum_minor"`
}
