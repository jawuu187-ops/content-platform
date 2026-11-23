// Package agegate enforces the 18+ requirement for paid content.
//
// The package deliberately knows nothing about identity documents. A provider
// performs the check, we store the normalised outcome (verified / failed,
// assurance label, expiry) and a reference. Nothing in this package can read a
// document image, a selfie or a date of birth, because none of it is stored.
//
// The gate is enforced on the server, in the access decision, for every paid
// scope. A client cannot skip it, and a paid subscription does not bypass it.
package agegate

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Status is the effective verification state of an account.
type Status string

const (
	// StatusUnverified means no check has ever been attempted.
	StatusUnverified Status = "unverified"
	// StatusPending means a check was started and the provider has not answered.
	StatusPending Status = "pending"
	// StatusVerified means the account may see 18+ content until ExpiresAt.
	StatusVerified Status = "verified"
	// StatusFailed means the provider rejected the account. A new attempt is allowed.
	StatusFailed Status = "failed"
	// StatusExpired means a previous verification passed its validity window.
	StatusExpired Status = "expired"
	// StatusRevoked means a moderator or the provider withdrew the verification.
	StatusRevoked Status = "revoked"
)

// Domain errors. The HTTP layer maps these to status codes, so the wording here
// is internal and carries no user-facing detail.
var (
	ErrUnavailable       = errors.New("agegate: no verification provider is configured")
	ErrStartUnsupported  = errors.New("agegate: the configured provider cannot start a hosted check")
	ErrInvalidCallback   = errors.New("agegate: callback payload is not valid")
	ErrBadSignature      = errors.New("agegate: callback signature is not valid")
	ErrStaleCallback     = errors.New("agegate: callback timestamp is outside the accepted window")
	ErrUnknownReference  = errors.New("agegate: callback does not match any check")
	ErrAccountMismatch   = errors.New("agegate: callback refers to a different account")
	ErrRateLimited       = errors.New("agegate: too many verification attempts")
	ErrAlreadyVerified   = errors.New("agegate: the account is already verified")
	ErrNoLiveCheck       = errors.New("agegate: no live verification exists")
	ErrNotVerified       = errors.New("agegate: the account is not age verified")
	ErrInvalidInput      = errors.New("agegate: invalid input")
	ErrDuplicateCallback = errors.New("agegate: this provider event was already applied")
)

// Check is one verification attempt. It holds no identity data by construction.
type Check struct {
	ID          string     `json:"id"`
	UserID      string     `json:"-"`
	Status      Status     `json:"status"`
	Method      string     `json:"method"`
	Provider    string     `json:"provider,omitempty"`
	ProviderRef string     `json:"-"`
	Assurance   string     `json:"assurance,omitempty"`
	MinAge      int        `json:"min_age"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	Failure     string     `json:"failure_reason,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// State is the answer to "may this account see 18+ content right now".
type State struct {
	Status    Status     `json:"status"`
	Verified  bool       `json:"verified"`
	MinAge    int        `json:"min_age"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CanRetry  bool       `json:"can_retry"`
	ShowURL   string     `json:"check_url,omitempty"`
	Assurance string     `json:"assurance,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
}

// StartInput is what the platform asks a provider to open a check for.
type StartInput struct {
	AccountID string
	RequestID string
	ReturnURL string
}

// StartOutput is the provider's hosted session.
type StartOutput struct {
	Reference string
	CheckURL  string
	ExpiresAt time.Time
}

// Callback is a provider's normalised answer, already signature-verified.
type Callback struct {
	EventID    string
	Reference  string
	AccountID  string
	Status     Status
	Assurance  string
	MinAge     int
	VerifiedAt *time.Time
	ExpiresAt  *time.Time
	Reason     string
}

// Capabilities describes what a provider can do and where. Operations tooling
// shows it; the access decision never reads it.
type Capabilities struct {
	Name            string   `json:"name"`
	StartSupported  bool     `json:"start_supported"`
	CallbackSupport bool     `json:"callback_supported"`
	AssuranceLevels []string `json:"assurance_levels,omitempty"`
	Regions         []string `json:"regions,omitempty"`
	RetainsEvidence bool     `json:"retains_evidence"`
}

// Provider is the single seam for age verification vendors. Every method is
// about a result or a reference; no method accepts or returns identity evidence.
type Provider interface {
	Name() string
	Start(ctx context.Context, in StartInput) (StartOutput, error)
	ParseCallback(headers http.Header, body []byte, now time.Time) (Callback, error)
	Capabilities() Capabilities
}

// Gate is the narrow interface the content layer depends on. Keeping it to one
// boolean question stops the access path from growing vendor knowledge.
type Gate interface {
	AllowAdult(ctx context.Context, accountID string) (bool, error)
}
