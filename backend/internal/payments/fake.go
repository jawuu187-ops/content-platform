package payments

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

// Fake is a development-only provider. It has no external endpoint and moves no
// money: a "checkout" is completed by sending a signed callback with the same
// scheme a real provider uses. That keeps the webhook, idempotency, ledger and
// revocation code paths under test without pretending to be a gateway.
//
// It must never be selectable in production; config refuses to boot with
// PAY_PROVIDER=fake unless APP_ENV is local or test.
type Fake struct {
	secret []byte
	now    func() time.Time
}

// NewFake builds the fake provider. The secret is required, as with a real
// provider: the signature path is exercised, not skipped.
func NewFake(secret []byte) (*Fake, error) {
	if len(secret) < 16 {
		return nil, fmt.Errorf("%w: webhook secret must be at least 16 bytes", ErrInvalidInput)
	}
	return &Fake{secret: secret, now: time.Now}, nil
}

// Name implements Provider.
func (f *Fake) Name() string { return "fake" }

// Capabilities implements Provider.
func (f *Fake) Capabilities() Capabilities {
	return Capabilities{HostedCheckout: false, Webhooks: true, Refunds: true}
}

// CreateIntent implements Provider. There is no hosted page, so the intent goes
// straight to pending and the caller completes it with a signed callback.
func (f *Fake) CreateIntent(_ context.Context, in Intent) (Intent, error) {
	in.ProviderRef = "fake_" + in.ID
	in.Status = StatusPending
	return in, nil
}

type fakePayload struct {
	EventID     string `json:"event_id"`
	Kind        string `json:"kind"`
	IntentID    string `json:"intent_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Reason      string `json:"reason"`
	OccurredAt  string `json:"occurred_at"`
}

// ParseEvent implements Provider using the same HMAC scheme as the age provider:
// `X-Payment-Signature: t=<unix>,v1=<hex HMAC-SHA256 over "t.body">`.
func (f *Fake) ParseEvent(_ context.Context, header http.Header, body []byte) (Event, error) {
	raw := header.Get("X-Payment-Signature")
	ts, sig, ok := parseSignatureHeader(raw)
	if !ok {
		return Event{}, ErrBadSignature
	}
	if delta := f.now().Sub(time.Unix(ts, 0)); delta > 5*time.Minute || delta < -5*time.Minute {
		return Event{}, fmt.Errorf("%w: timestamp outside the allowed window", ErrBadSignature)
	}
	mac := hmac.New(sha256.New, f.secret)
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	if !hmac.Equal([]byte(sig), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return Event{}, ErrBadSignature
	}

	var p fakePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, fmt.Errorf("%w: body is not JSON", ErrInvalidInput)
	}
	if p.EventID == "" || p.IntentID == "" {
		return Event{}, fmt.Errorf("%w: event_id and intent_id are required", ErrInvalidInput)
	}
	switch p.Kind {
	case EventSucceeded, EventFailed, EventRefunded:
	default:
		return Event{}, fmt.Errorf("%w: unknown event kind %q", ErrInvalidInput, p.Kind)
	}
	if p.AmountMinor <= 0 || len(p.Currency) != 3 {
		return Event{}, fmt.Errorf("%w: amount and currency are required", ErrInvalidInput)
	}
	occurred := f.now()
	if p.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, p.OccurredAt)
		if err != nil {
			return Event{}, fmt.Errorf("%w: occurred_at is not RFC3339", ErrInvalidInput)
		}
		occurred = t
	}
	return Event{
		ID:          p.EventID,
		Kind:        p.Kind,
		IntentID:    p.IntentID,
		AmountMinor: p.AmountMinor,
		Currency:    strings.ToUpper(p.Currency),
		OccurredAt:  occurred,
		Reason:      p.Reason,
	}, nil
}

// Refund implements Provider. The fake claims success so the platform-side
// reversal (ledger, entitlement revocation) can be tested.
func (f *Fake) Refund(_ context.Context, in Intent, amountMinor int64, _ string) (string, error) {
	if amountMinor <= 0 || amountMinor > in.AmountMinor {
		return "", fmt.Errorf("%w: refund amount is out of range", ErrInvalidInput)
	}
	return "fake_refund_" + in.ID, nil
}

// Sign builds the header a caller uses to complete a fake payment. It is exported
// so the smoke workflow and tests can produce a genuine signature instead of
// bypassing verification.
func (f *Fake) Sign(body []byte, at time.Time) string {
	mac := hmac.New(sha256.New, f.secret)
	ts := strconv.FormatInt(at.Unix(), 10)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func parseSignatureHeader(raw string) (int64, string, bool) {
	var ts int64
	var sig string
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, "", false
			}
			ts = n
		case "v1":
			sig = value
		}
	}
	if ts == 0 || sig == "" {
		return 0, "", false
	}
	return ts, sig, true
}

// Unavailable is the driver used when no provider is configured. It fails
// closed: no intent is created and no callback is accepted.
type Unavailable struct{}

// Name implements Provider.
func (Unavailable) Name() string { return "unconfigured" }

// Capabilities implements Provider.
func (Unavailable) Capabilities() Capabilities { return Capabilities{} }

// CreateIntent implements Provider.
func (Unavailable) CreateIntent(context.Context, Intent) (Intent, error) {
	return Intent{}, ErrUnavailable
}

// ParseEvent implements Provider.
func (Unavailable) ParseEvent(context.Context, http.Header, []byte) (Event, error) {
	return Event{}, ErrUnavailable
}

// Refund implements Provider.
func (Unavailable) Refund(context.Context, Intent, int64, string) (string, error) {
	return "", ErrUnavailable
}
