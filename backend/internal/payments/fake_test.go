package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testSecret = "pay-webhook-secret-0123456789"

func signedBody(t *testing.T, f *Fake, payload map[string]any, at time.Time) (http.Header, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	h := http.Header{}
	h.Set("X-Payment-Signature", f.Sign(body, at))
	return h, body
}

func TestFakeProviderVerifiesSignatureAndWindow(t *testing.T) {
	f, err := NewFake([]byte(testSecret))
	if err != nil {
		t.Fatalf("new fake: %v", err)
	}
	payload := map[string]any{
		"event_id": "evt_1", "kind": EventSucceeded, "intent_id": "11111111-1111-4111-8111-111111111111",
		"amount_minor": 1200, "currency": "usd",
	}
	header, body := signedBody(t, f, payload, time.Now())
	event, err := f.ParseEvent(context.Background(), header, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event.ID != "evt_1" || event.Currency != "USD" || event.AmountMinor != 1200 {
		t.Fatalf("unexpected event: %+v", event)
	}

	// Tampered body with the original signature.
	if _, err := f.ParseEvent(context.Background(), header, append(body, ' ')); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered body: want ErrBadSignature, got %v", err)
	}
	// Replayed outside the 5-minute window.
	staleHeader, staleBody := signedBody(t, f, payload, time.Now().Add(-10*time.Minute))
	if _, err := f.ParseEvent(context.Background(), staleHeader, staleBody); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("stale signature: want ErrBadSignature, got %v", err)
	}
	// Missing header.
	if _, err := f.ParseEvent(context.Background(), http.Header{}, body); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("missing signature: want ErrBadSignature, got %v", err)
	}
	// Unknown kind and missing identifiers.
	badKind, badKindBody := signedBody(t, f, map[string]any{
		"event_id": "evt_2", "kind": "payment.magic", "intent_id": "x", "amount_minor": 1, "currency": "USD",
	}, time.Now())
	if _, err := f.ParseEvent(context.Background(), badKind, badKindBody); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown kind: want ErrInvalidInput, got %v", err)
	}
}

// The fake must announce that it is a simulation. Everything downstream — the intent
// row, the ledger posting, /v1/meta, the client label — keys off this one flag, so a
// fake that claimed to be real would silently defeat the whole separation.
func TestFakeDeclaresItselfSimulated(t *testing.T) {
	f, err := NewFake([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Capabilities().Simulated {
		t.Fatal("fake provider must report Simulated=true")
	}
	var unavailable Unavailable
	if unavailable.Capabilities().Simulated {
		t.Fatal("the fail-closed provider must not claim to be a simulation")
	}
}

func TestFakeRefundBounds(t *testing.T) {
	f, _ := NewFake([]byte(testSecret))
	in := Intent{ID: "abc", AmountMinor: 500}
	if _, err := f.Refund(context.Background(), in, 600, "too much"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("over-refund: want ErrInvalidInput, got %v", err)
	}
	if _, err := f.Refund(context.Background(), in, 500, "full"); err != nil {
		t.Fatalf("full refund: %v", err)
	}
}

func TestFakeRefusesShortSecretAndUnavailableFailsClosed(t *testing.T) {
	if _, err := NewFake([]byte("short")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short secret: want ErrInvalidInput, got %v", err)
	}
	if _, err := (Unavailable{}).CreateIntent(context.Background(), Intent{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable create: want ErrUnavailable, got %v", err)
	}
	if _, err := (Unavailable{}).ParseEvent(context.Background(), http.Header{}, []byte("{}")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable parse: want ErrUnavailable, got %v", err)
	}
}

func TestConfiguredReflectsProvider(t *testing.T) {
	svc := New(nil)
	if svc.Configured() {
		t.Fatal("a service with no provider must not report configured")
	}
	if svc.ProviderName() != "unconfigured" {
		t.Fatalf("provider name: %s", svc.ProviderName())
	}
}
