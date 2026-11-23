// Package payments turns a purchase into a settled payment and a ledger entry.
//
// The package is provider-independent on purpose. No gateway has been able to
// serve this business model at zero cost (see docs/vendors/payments-zero-cost.md),
// so the only shipped driver is a fake that exists to prove the flow end to end
// and is refused in production. A real gateway implements the same interface.
package payments

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Purpose is what a payment buys.
const (
	PurposeSubscription = "subscription"
	PurposePPV          = "ppv"
	PurposeTopup        = "topup"
)

// Intent states. A terminal state is never left again: refunds move a succeeded
// intent to refunded, and nothing moves back to pending.
const (
	StatusCreated    = "created"
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusCanceled   = "canceled"
	StatusRefunded   = "refunded"
)

// Event kinds a provider may report.
const (
	EventSucceeded = "payment.succeeded"
	EventFailed    = "payment.failed"
	EventRefunded  = "payment.refunded"
)

// Domain errors. The HTTP layer maps these to codes.
var (
	ErrUnavailable       = errors.New("payments unavailable")
	ErrInvalidInput      = errors.New("invalid payment request")
	ErrNotFound          = errors.New("payment not found")
	ErrForbidden         = errors.New("payment not allowed")
	ErrBadSignature      = errors.New("invalid webhook signature")
	ErrUnknownIntent     = errors.New("unknown payment intent")
	ErrAmountMismatch    = errors.New("payment amount does not match the intent")
	ErrInvalidState      = errors.New("payment is not in a state that allows this")
	ErrRefundUnsupported = errors.New("provider does not support refunds")
)

// Intent is one purchase attempt.
type Intent struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	Purpose        string     `json:"purpose"`
	CreatorID      string     `json:"creator_id,omitempty"`
	TierID         string     `json:"tier_id,omitempty"`
	PostID         string     `json:"post_id,omitempty"`
	AmountMinor    int64      `json:"amount_minor"`
	Currency       string     `json:"currency"`
	Provider       string     `json:"provider"`
	ProviderRef    string     `json:"provider_ref,omitempty"`
	Status         string     `json:"status"`
	CheckoutURL    string     `json:"checkout_url,omitempty"`
	FailureReason  string     `json:"failure_reason,omitempty"`
	IdempotencyKey string     `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	SettledAt      *time.Time `json:"settled_at,omitempty"`
}

// CreateInput is what a client may ask for. It never carries a price.
type CreateInput struct {
	Purpose        string `json:"purpose"`
	TierID         string `json:"tier_id"`
	PostID         string `json:"post_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// Event is a normalised provider callback.
type Event struct {
	ID          string
	Kind        string
	IntentID    string
	AmountMinor int64
	Currency    string
	OccurredAt  time.Time
	Reason      string
}

// Capabilities describes what a provider can do, so the API can be honest about
// it instead of failing at the last step.
type Capabilities struct {
	HostedCheckout bool `json:"hosted_checkout"`
	Webhooks       bool `json:"webhooks"`
	Refunds        bool `json:"refunds"`
}

// Provider is the payment seam. Implementations must verify their own webhook
// signatures: the service never trusts an unauthenticated callback.
type Provider interface {
	Name() string
	Capabilities() Capabilities
	// CreateIntent asks the provider to open a payment. It may return a checkout
	// URL, or an empty one when the provider has no hosted page.
	CreateIntent(ctx context.Context, in Intent) (Intent, error)
	// ParseEvent authenticates and normalises a callback.
	ParseEvent(ctx context.Context, header http.Header, body []byte) (Event, error)
	// Refund asks the provider to return money. Providers without refunds return
	// ErrRefundUnsupported, and the platform then refuses rather than pretending.
	Refund(ctx context.Context, in Intent, amountMinor int64, reason string) (string, error)
}

// Result reports what handling a webhook did.
type Result struct {
	Applied   bool   `json:"applied"`
	Duplicate bool   `json:"duplicate"`
	IntentID  string `json:"intent_id"`
	Status    string `json:"status"`
}
