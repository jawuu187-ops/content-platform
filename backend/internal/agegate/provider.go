package agegate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader is the header a provider adapter (or its webhook relay) must
// sign. The format is `t=<unix seconds>,v1=<hex hmac-sha256 of "t.body">`.
const SignatureHeader = "X-Age-Signature"

// defaultTolerance bounds how old a signed callback may be. A captured request
// cannot be replayed later than this, and clock skew beyond it is a failure
// rather than a silent accept.
const defaultTolerance = 5 * time.Minute

// WebhookProvider verifies provider callbacks with a shared HMAC secret and
// normalises the documented payload into a Callback.
//
// It cannot start a hosted check: the hosted UI and session creation are
// vendor-specific and are added when a vendor is chosen (docs/vendors/
// age-verification.md tracks the shortlist). Until then Start returns
// ErrStartUnsupported, which the HTTP layer reports as 503 rather than
// pretending a verification flow exists.
type WebhookProvider struct {
	name      string
	secret    []byte
	tolerance time.Duration
	now       func() time.Time
}

// NewWebhookProvider builds a provider. The secret must be at least 16 bytes;
// a short secret is a configuration error, not something to paper over.
func NewWebhookProvider(name string, secret []byte) (*WebhookProvider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: provider name is required", ErrInvalidInput)
	}
	if len(secret) < 16 {
		return nil, fmt.Errorf("%w: callback secret must be at least 16 bytes", ErrInvalidInput)
	}
	return &WebhookProvider{name: name, secret: secret, tolerance: defaultTolerance, now: time.Now}, nil
}

// Name returns the configured provider identifier.
func (p *WebhookProvider) Name() string { return p.name }

// Start reports that hosted check creation is not integrated yet.
func (p *WebhookProvider) Start(context.Context, StartInput) (StartOutput, error) {
	return StartOutput{}, ErrStartUnsupported
}

// Capabilities describes the generic receiver honestly.
func (p *WebhookProvider) Capabilities() Capabilities {
	return Capabilities{
		Name:            p.name,
		StartSupported:  false,
		CallbackSupport: true,
		RetainsEvidence: true, // the vendor keeps the evidence; we do not
	}
}

// payload is the documented callback body. Unknown fields are ignored on
// purpose: a vendor adding a field must not break verification.
type payload struct {
	EventID    string `json:"event_id"`
	Reference  string `json:"reference"`
	AccountID  string `json:"account_id"`
	Status     string `json:"status"`
	Assurance  string `json:"assurance"`
	MinAge     int    `json:"min_age"`
	VerifiedAt string `json:"verified_at"`
	ExpiresAt  string `json:"expires_at"`
	Reason     string `json:"reason"`
}

// ParseCallback verifies the signature and timestamp before decoding anything,
// so an unsigned body is never interpreted.
func (p *WebhookProvider) ParseCallback(headers http.Header, body []byte, now time.Time) (Callback, error) {
	if err := p.verifySignature(headers, body, now); err != nil {
		return Callback{}, err
	}
	var in payload
	if err := json.Unmarshal(body, &in); err != nil {
		return Callback{}, fmt.Errorf("%w: body is not JSON", ErrInvalidCallback)
	}
	if strings.TrimSpace(in.EventID) == "" || strings.TrimSpace(in.Reference) == "" {
		return Callback{}, fmt.Errorf("%w: event_id and reference are required", ErrInvalidCallback)
	}
	if strings.TrimSpace(in.AccountID) == "" {
		return Callback{}, fmt.Errorf("%w: account_id is required", ErrInvalidCallback)
	}
	out := Callback{
		EventID:   in.EventID,
		Reference: in.Reference,
		AccountID: in.AccountID,
		Assurance: strings.TrimSpace(in.Assurance),
		MinAge:    in.MinAge,
		Reason:    strings.TrimSpace(in.Reason),
	}
	if out.MinAge == 0 {
		out.MinAge = 18
	}
	if out.MinAge < 18 || out.MinAge > 21 {
		return Callback{}, fmt.Errorf("%w: min_age must be between 18 and 21", ErrInvalidCallback)
	}
	switch Status(strings.ToLower(strings.TrimSpace(in.Status))) {
	case StatusVerified, StatusFailed, StatusExpired, StatusRevoked:
		out.Status = Status(strings.ToLower(strings.TrimSpace(in.Status)))
	default:
		return Callback{}, fmt.Errorf("%w: status must be verified, failed, expired or revoked", ErrInvalidCallback)
	}
	var err error
	if out.VerifiedAt, err = parseOptionalTime(in.VerifiedAt); err != nil {
		return Callback{}, fmt.Errorf("%w: verified_at must be RFC 3339", ErrInvalidCallback)
	}
	if out.ExpiresAt, err = parseOptionalTime(in.ExpiresAt); err != nil {
		return Callback{}, fmt.Errorf("%w: expires_at must be RFC 3339", ErrInvalidCallback)
	}
	if out.Status == StatusVerified {
		if out.VerifiedAt == nil {
			return Callback{}, fmt.Errorf("%w: a verified callback needs verified_at", ErrInvalidCallback)
		}
		if out.ExpiresAt != nil && !out.ExpiresAt.After(*out.VerifiedAt) {
			return Callback{}, fmt.Errorf("%w: expires_at must be after verified_at", ErrInvalidCallback)
		}
	}
	return out, nil
}

func parseOptionalTime(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	utc := t.UTC()
	return &utc, nil
}

func (p *WebhookProvider) verifySignature(headers http.Header, body []byte, now time.Time) error {
	raw := headers.Get(SignatureHeader)
	if raw == "" {
		return ErrBadSignature
	}
	var ts string
	var sig string
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			ts = value
		case "v1":
			sig = value
		}
	}
	if ts == "" || sig == "" {
		return ErrBadSignature
	}
	seconds, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrBadSignature
	}
	sent := time.Unix(seconds, 0).UTC()
	skew := now.Sub(sent)
	if skew < 0 {
		skew = -skew
	}
	if skew > p.tolerance {
		return ErrStaleCallback
	}
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(sig)
	if err != nil {
		return ErrBadSignature
	}
	if !hmac.Equal(want, got) {
		return ErrBadSignature
	}
	return nil
}

// Unavailable is the provider used when no vendor is configured. It refuses
// every operation instead of inventing a result.
type Unavailable struct{}

// NewUnavailable builds the fail-closed provider.
func NewUnavailable(string) *Unavailable { return &Unavailable{} }

// Name returns "unconfigured".
func (u *Unavailable) Name() string { return "unconfigured" }

// Start always fails: there is nothing to start.
func (u *Unavailable) Start(context.Context, StartInput) (StartOutput, error) {
	return StartOutput{}, ErrUnavailable
}

// ParseCallback always fails: an unsigned callback is never trusted.
func (u *Unavailable) ParseCallback(http.Header, []byte, time.Time) (Callback, error) {
	return Callback{}, ErrUnavailable
}

// Capabilities reports an unconfigured provider.
func (u *Unavailable) Capabilities() Capabilities {
	return Capabilities{Name: "unconfigured", StartSupported: false, CallbackSupport: false}
}
