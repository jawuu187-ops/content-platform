package agegate

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const testSecret = "agegate-test-secret-0123456789"

// testProvider starts checks and verifies callbacks with the real webhook
// verifier, so signature handling is exercised rather than stubbed.
type testProvider struct {
	name      string
	base      string
	lastRef   string
	webhook   *WebhookProvider
	started   int
	failStart bool
}

func (p *testProvider) Name() string { return p.name }

func (p *testProvider) Start(context.Context, StartInput) (StartOutput, error) {
	if p.failStart {
		return StartOutput{}, errors.New("provider is down")
	}
	p.started++
	p.lastRef = p.base + "_" + strconv.Itoa(p.started)
	return StartOutput{
		Reference: p.lastRef,
		CheckURL:  "https://verify.example.test/session/" + p.ref,
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (p *testProvider) ParseCallback(h http.Header, b []byte, now time.Time) (Callback, error) {
	return p.webhook.ParseCallback(h, b, now)
}

func (p *testProvider) Capabilities() Capabilities {
	return Capabilities{Name: p.name, StartSupported: true, CallbackSupport: true}
}

type harness struct {
	svc      *Service
	ids      *identity.Service
	provider *testProvider
	now      time.Time
	ref      string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), ref: "chk_" + randID(t)}
	pool := dbtest.Pool(t)
	h.ids = identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	webhook, err := NewWebhookProvider("testco", []byte(testSecret))
	if err != nil {
		t.Fatalf("webhook provider: %v", err)
	}
	h.provider = &testProvider{name: "testco", base: h.ref, webhook: webhook}
	h.svc = New(pool,
		WithProvider(h.provider),
		WithClock(func() time.Time { return h.now }),
		WithValidity(30*24*time.Hour),
		WithStartLimit(3, 24*time.Hour),
	)
	return h
}

func randID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

func (h *harness) account(t *testing.T) string {
	t.Helper()
	res, err := h.ids.Register(context.Background(), "age-"+randID(t)+"@example.test",
		"a-very-long-password", identity.DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal.UserID
}

func sign(t *testing.T, body []byte, at time.Time) http.Header {
	t.Helper()
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	h := http.Header{}
	h.Set(SignatureHeader, "t="+ts+",v1="+hex.EncodeToString(mac.Sum(nil)))
	h.Set("Content-Type", "application/json")
	return h
}

func callbackBody(t *testing.T, accountID, reference, eventID, status string, extra map[string]any) []byte {
	t.Helper()
	payload := map[string]any{
		"event_id": eventID, "reference": reference, "account_id": accountID,
		"status": status, "assurance": "document_and_liveness", "min_age": 18,
	}
	for k, v := range extra {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return body
}

func (h *harness) verify(t *testing.T, accountID, eventID string, expiresAt time.Time) Check {
	t.Helper()
	body := callbackBody(t, accountID, h.provider.lastRef, eventID, "verified", map[string]any{
		"verified_at": h.now.Format(time.RFC3339),
		"expires_at":  expiresAt.Format(time.RFC3339),
	})
	check, err := h.svc.HandleCallback(context.Background(), sign(t, body, h.now), body)
	if err != nil {
		t.Fatalf("verified callback: %v", err)
	}
	return check
}

func TestUnverifiedAccountIsNotAllowed(t *testing.T) {
	h := newHarness(t)
	user := h.account(t)

	state, err := h.svc.Status(context.Background(), user)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Status != StatusUnverified || state.Verified {
		t.Fatalf("fresh account state: %+v", state)
	}
	ok, err := h.svc.AllowAdult(context.Background(), user)
	if err != nil || ok {
		t.Fatalf("unverified account allowed: %v err=%v", ok, err)
	}
	// An unknown account id must not pass either.
	if ok, err := h.svc.AllowAdult(context.Background(), "00000000-0000-4000-8000-000000000000"); err != nil || ok {
		t.Fatalf("unknown account allowed: %v err=%v", ok, err)
	}
}

func TestStartThenVerifiedCallbackGrantsAccess(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	user := h.account(t)

	state, err := h.svc.Start(ctx, user)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if state.Status != StatusPending || state.ShowURL == "" {
		t.Fatalf("start state: %+v", state)
	}
	if ok, _ := h.svc.AllowAdult(ctx, user); ok {
		t.Fatal("a pending check granted access")
	}
	if _, err := h.svc.Start(ctx, user); err != nil {
		t.Fatalf("a retry while pending is allowed until the limit: %v", err)
	}

	h.verify(t, user, "evt_1", h.now.Add(30*24*time.Hour))

	state, err = h.svc.Status(ctx, user)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !state.Verified || state.Status != StatusVerified || state.ExpiresAt == nil {
		t.Fatalf("verified state: %+v", state)
	}
	ok, err := h.svc.AllowAdult(ctx, user)
	if err != nil || !ok {
		t.Fatalf("verified account denied: %v err=%v", ok, err)
	}
}

func TestExpiredVerificationClosesAccess(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	user := h.account(t)
	if _, err := h.svc.Start(ctx, user); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.verify(t, user, "evt_exp", h.now.Add(24*time.Hour))

	h.now = h.now.Add(25 * time.Hour)
	if ok, _ := h.svc.AllowAdult(ctx, user); ok {
		t.Fatal("an expired verification still granted access")
	}
	if n, err := h.svc.ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("expire due: n=%d err=%v", n, err)
	}
	state, err := h.svc.Status(ctx, user)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Status != StatusExpired || state.Verified || !state.CanRetry {
		t.Fatalf("expired state: %+v", state)
	}
}

func TestRevocationClosesAccessAndIsAudited(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	user := h.account(t)
	admin := h.account(t)
	if _, err := h.svc.Start(ctx, user); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.verify(t, user, "evt_rev", h.now.Add(30*24*time.Hour))

	if _, err := h.svc.Revoke(ctx, admin, user, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("revoke without a reason: want ErrInvalidInput, got %v", err)
	}
	n, err := h.svc.Revoke(ctx, admin, user, "provider withdrew the result")
	if err != nil || n != 1 {
		t.Fatalf("revoke: n=%d err=%v", n, err)
	}
	if ok, _ := h.svc.AllowAdult(ctx, user); ok {
		t.Fatal("revoked verification still granted access")
	}
	state, err := h.svc.Status(ctx, user)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Status != StatusRevoked || state.CanRetry {
		t.Fatalf("revoked state: %+v", state)
	}
	// Revoking twice is not an error, it simply has nothing left to do.
	if n, err := h.svc.Revoke(ctx, admin, user, "again"); err != nil || n != 0 {
		t.Fatalf("second revoke: n=%d err=%v", n, err)
	}
}

func TestCallbackReplayIsRejected(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	user := h.account(t)
	if _, err := h.svc.Start(ctx, user); err != nil {
		t.Fatalf("start: %v", err)
	}
	body := callbackBody(t, user, h.provider.lastRef, "evt_same", "verified", map[string]any{
		"verified_at": h.now.Format(time.RFC3339),
		"expires_at":  h.now.Add(24 * time.Hour).Format(time.RFC3339),
	})
	headers := sign(t, body, h.now)
	if _, err := h.svc.HandleCallback(ctx, headers, body); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	// The same event id is refused even when re-signed, so a captured request
	// cannot be replayed to extend a validity window.
	h.now = h.now.Add(time.Hour)
	if _, err := h.svc.HandleCallback(ctx, sign(t, body, h.now), body); !errors.Is(err, ErrDuplicateCallback) {
		t.Fatalf("replay: want ErrDuplicateCallback, got %v", err)
	}
}

func TestCallbackSignatureAndTimestamp(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	user := h.account(t)
	if _, err := h.svc.Start(ctx, user); err != nil {
		t.Fatalf("start: %v", err)
	}
	body := callbackBody(t, user, h.provider.lastRef, "evt_sig", "verified", map[string]any{
		"verified_at": h.now.Format(time.RFC3339),
	})

	if _, err := h.svc.HandleCallback(ctx, http.Header{}, body); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("unsigned callback: want ErrBadSignature, got %v", err)
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] = 'x'
	if _, err := h.svc.HandleCallback(ctx, sign(t, body, h.now), tampered); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered body: want ErrBadSignature, got %v", err)
	}
	// A signature older than the tolerance window is refused even though it is
	// cryptographically valid: that is what stops a slow replay.
	if _, err := h.svc.HandleCallback(ctx, sign(t, body, h.now.Add(-10*time.Minute)), body); !errors.Is(err, ErrStaleCallback) {
		t.Fatalf("stale callback: want ErrStaleCallback, got %v", err)
	}
	// A callback for an account that does not own the check is refused.
	other := h.account(t)
	wrong := callbackBody(t, other, h.provider.lastRef, "evt_wrong", "verified", map[string]any{
		"verified_at": h.now.Format(time.RFC3339),
	})
	if _, err := h.svc.HandleCallback(ctx, sign(t, wrong, h.now), wrong); !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("foreign account: want ErrAccountMismatch, got %v", err)
	}
	if _, err := h.svc.HandleCallback(ctx, sign(t, callbackBody(t, user, "chk_missing", "evt_x", "verified",
		map[string]any{"verified_at": h.now.Format(time.RFC3339)}), h.now),
		callbackBody(t, user, "chk_missing", "evt_x", "verified",
			map[string]any{"verified_at": h.now.Format(time.RFC3339)})); !errors.Is(err, ErrUnknownReference) {
		t.Fatalf("unknown reference: want ErrUnknownReference, got %v", err)
	}
	if ok, _ := h.svc.AllowAdult(ctx, user); ok {
		t.Fatal("rejected callbacks granted access")
	}
}

func TestFailedCallbackAllowsRetry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	user := h.account(t)
	if _, err := h.svc.Start(ctx, user); err != nil {
		t.Fatalf("start: %v", err)
	}
	body := callbackBody(t, user, h.provider.lastRef, "evt_fail", "failed", map[string]any{"reason": "no face detected"})
	if _, err := h.svc.HandleCallback(ctx, sign(t, body, h.now), body); err != nil {
		t.Fatalf("failed callback: %v", err)
	}
	state, err := h.svc.Status(ctx, user)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Status != StatusFailed || !state.CanRetry || state.Verified {
		t.Fatalf("failed state: %+v", state)
	}
	if ok, _ := h.svc.AllowAdult(ctx, user); ok {
		t.Fatal("a failed check granted access")
	}
}

func TestStartLimitsAndUnavailableProviders(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	user := h.account(t)
	for i := 0; i < 3; i++ {
		if _, err := h.svc.Start(ctx, user); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	if _, err := h.svc.Start(ctx, user); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("fourth start: want ErrRateLimited, got %v", err)
	}

	off := New(h.svc.store.pool)
	if _, err := off.Start(ctx, user); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured start: want ErrUnavailable, got %v", err)
	}
	webhook, err := NewWebhookProvider("generic", []byte(testSecret))
	if err != nil {
		t.Fatalf("webhook: %v", err)
	}
	signedOnly := New(h.svc.store.pool, WithProvider(webhook))
	if _, err := signedOnly.Start(ctx, user); !errors.Is(err, ErrStartUnsupported) {
		t.Fatalf("webhook-only start: want ErrStartUnsupported, got %v", err)
	}
	if signedOnly.Capabilities().StartSupported {
		t.Fatal("capabilities claim a hosted check that does not exist")
	}
}

func TestProviderRejectsInvalidPayloads(t *testing.T) {
	webhook, err := NewWebhookProvider("generic", []byte(testSecret))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	now := time.Now().UTC()
	if _, err := NewWebhookProvider("", []byte(testSecret)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name: want ErrInvalidInput, got %v", err)
	}
	if _, err := NewWebhookProvider("x", []byte("short")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short secret: want ErrInvalidInput, got %v", err)
	}

	bad := []map[string]any{
		{},                                  // missing everything
		{"event_id": "e", "reference": "r"}, // missing account
		{"event_id": "e", "reference": "r", "account_id": "a", "status": "pending"},
		{"event_id": "e", "reference": "r", "account_id": "a", "status": "verified", "min_age": 12},
		{"event_id": "e", "reference": "r", "account_id": "a", "status": "verified",
			"verified_at": "not-a-time"},
	}
	for i, payload := range bad {
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := webhook.ParseCallback(sign(t, body, now), body, now); !errors.Is(err, ErrInvalidCallback) {
			t.Fatalf("payload %d: want ErrInvalidCallback, got %v", i, err)
		}
	}
	// A body that is not JSON at all.
	if _, err := webhook.ParseCallback(sign(t, []byte("nope"), now), []byte("nope"), now); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("non-JSON: want ErrInvalidCallback, got %v", err)
	}
	// A verified callback without expires_at gets the configured validity.
	body := callbackBody(t, "acct", "ref", "evt_default", "verified",
		map[string]any{"verified_at": now.Format(time.RFC3339)})
	cb, err := webhook.ParseCallback(sign(t, body, now), body, now)
	if err != nil {
		t.Fatalf("valid callback: %v", err)
	}
	if cb.Status != StatusVerified || cb.ExpiresAt != nil {
		t.Fatalf("unexpected callback: %+v", cb)
	}
}
