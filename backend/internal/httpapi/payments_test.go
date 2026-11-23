package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/media"
	"github.com/jawuu187-ops/content-platform/backend/internal/payments"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const payTestSecret = "pay-webhook-secret-0123456789"

type payFixture struct {
	handler http.Handler
	ids     *identity.Service
	fake    *payments.Fake
	pool    *pgxpool.Pool
}

func newPayFixture(t *testing.T) *payFixture {
	t.Helper()
	pool := dbtest.Pool(t)
	ids := identity.New(pool,
		identity.WithHasher(identity.NewHasher(identity.TestParams())),
		identity.WithIssuer("Content Platform Test"))
	creators := creator.New(pool, creator.WithAgeGate(permissiveGate{}))
	fake, err := payments.NewFake([]byte(payTestSecret))
	if err != nil {
		t.Fatalf("fake provider: %v", err)
	}
	svc := payments.New(pool,
		payments.WithProvider(fake),
		payments.WithLedger(ledger.New(pool)),
		payments.WithCreators(creators),
		payments.WithCommission(2000),
	)

	// Media is wired as well so the PPV test can exercise a real delivery link.
	store, err := media.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	mediaSvc := media.New(pool,
		media.WithStorage(store),
		media.WithSigningKey([]byte(mediaTestKey)),
		media.WithTokenTTL(60*time.Second),
	)
	h := New(Deps{
		Config:   testConfig(),
		Logger:   loggerDiscard(),
		Identity: ids,
		Creators: creators,
		Payments: svc,
		Media:    mediaSvc,
	})
	return &payFixture{handler: h, ids: ids, fake: fake, pool: pool}
}

func (f *payFixture) intent(t *testing.T, token string, body map[string]any) (string, string) {
	t.Helper()
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/payments/intents", token, encodeBody(t, body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create intent: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Payment struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"payment"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode intent: %v", err)
	}
	return out.Payment.ID, out.Payment.Status
}

// sendEvent posts a signed provider callback.
func (f *payFixture) sendEvent(t *testing.T, eventID, kind, intentID string, amount int64, currency string) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"event_id": eventID, "kind": kind, "intent_id": intentID,
		"amount_minor": amount, "currency": currency,
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Payment-Signature", f.fake.Sign(body, time.Now()))
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *payFixture) ledgerStats(t *testing.T, reference string) (txns int, entries int, net int64) {
	t.Helper()
	if err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM ledger.transactions WHERE reference = $1`, reference).Scan(&txns); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if err := f.pool.QueryRow(context.Background(), `
		SELECT COALESCE(count(*), 0), COALESCE(sum(e.amount_minor), 0)
		FROM ledger.entries e JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE t.reference = $1`, reference).Scan(&entries, &net); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return txns, entries, net
}

func TestSubscriptionPurchaseSettlesOnceAndIsIdempotent(t *testing.T) {
	f := newPayFixture(t)
	_, pair := publishCreator(t, f.handler)
	handle, tierID := splitPair(pair)
	fan := registerAndGetToken(t, f.handler, "pay-fan-"+randSuffix(t)+"@example.test")
	key := "idem-" + randSuffix(t)

	intentID, status := f.intent(t, fan.Tokens.AccessToken, map[string]any{
		"purpose": "subscription", "tier_id": tierID, "idempotency_key": key,
		// A price in the request must be ignored: the server reads it from the tier.
		"amount_minor": 1,
	})
	if status != "pending" {
		t.Fatalf("new intent status: %s", status)
	}

	// The same idempotency key returns the same intent, not a second charge.
	againID, _ := f.intent(t, fan.Tokens.AccessToken, map[string]any{
		"purpose": "subscription", "tier_id": tierID, "idempotency_key": key,
	})
	if againID != intentID {
		t.Fatalf("idempotency key created a second intent: %s vs %s", againID, intentID)
	}

	// Not paid yet: no access.
	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); d.Allowed {
		t.Fatalf("unpaid subscription granted access: %+v", d)
	}

	if rec := f.sendEvent(t, "evt_"+randSuffix(t), payments.EventSucceeded, intentID, 1200, "USD"); rec.Code != http.StatusOK {
		t.Fatalf("settle: %d %s", rec.Code, rec.Body.String())
	}
	// Replay of the same event: accepted, not applied twice.
	rec := f.sendEvent(t, "evt_replay_fixed", payments.EventSucceeded, intentID, 1200, "USD")
	if rec.Code != http.StatusOK {
		t.Fatalf("first replay: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.sendEvent(t, "evt_replay_fixed", payments.EventSucceeded, intentID, 1200, "USD")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("duplicate event: want 202, got %d %s", rec.Code, rec.Body.String())
	}

	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); !d.Allowed {
		t.Fatalf("paid subscription denied: %+v", d)
	}
	txns, _, net := f.ledgerStats(t, intentID)
	if txns != 1 || net != 0 {
		t.Fatalf("ledger after settlement: txns=%d net=%d", txns, net)
	}
	// Commission 20%: 1200 -> 960 creator payable, 240 revenue.
	var payable, revenue int64
	if err := f.pool.QueryRow(context.Background(), `
		SELECT COALESCE(-sum(CASE WHEN a.code LIKE 'creator:%' THEN e.amount_minor ELSE 0 END), 0),
		       COALESCE(-sum(CASE WHEN a.code = 'platform:revenue' THEN e.amount_minor ELSE 0 END), 0)
		FROM ledger.entries e JOIN ledger.accounts a ON a.id = e.account_id`).Scan(&payable, &revenue); err != nil {
		t.Fatalf("read balances: %v", err)
	}
	if payable != 960 || revenue != 240 {
		t.Fatalf("split: payable=%d revenue=%d", payable, revenue)
	}
}

func TestFailedPaymentGrantsNothing(t *testing.T) {
	f := newPayFixture(t)
	_, pair := publishCreator(t, f.handler)
	handle, tierID := splitPair(pair)
	fan := registerAndGetToken(t, f.handler, "pay-fan-"+randSuffix(t)+"@example.test")
	intentID, _ := f.intent(t, fan.Tokens.AccessToken, map[string]any{
		"purpose": "subscription", "tier_id": tierID, "idempotency_key": "idem-" + randSuffix(t),
	})

	if rec := f.sendEvent(t, "evt_fail_"+randSuffix(t), payments.EventFailed, intentID, 1200, "USD"); rec.Code != http.StatusOK {
		t.Fatalf("failed event: %d %s", rec.Code, rec.Body.String())
	}
	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); d.Allowed {
		t.Fatal("a failed payment granted access")
	}
	if txns, _, _ := f.ledgerStats(t, intentID); txns != 0 {
		t.Fatalf("a failed payment posted %d ledger transactions", txns)
	}
	rec := doJSON(t, f.handler, http.MethodGet, "/v1/payments/"+intentID, fan.Tokens.AccessToken, "")
	var out struct {
		Payment struct {
			Status string `json:"status"`
		} `json:"payment"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Payment.Status != "failed" {
		t.Fatalf("status: %s (%v)", rec.Body.String(), err)
	}
}

func TestRefundReversesLedgerAndRevokesAccess(t *testing.T) {
	f := newPayFixture(t)
	_, pair := publishCreator(t, f.handler)
	handle, tierID := splitPair(pair)
	fan := registerAndGetToken(t, f.handler, "pay-fan-"+randSuffix(t)+"@example.test")
	intentID, _ := f.intent(t, fan.Tokens.AccessToken, map[string]any{
		"purpose": "subscription", "tier_id": tierID, "idempotency_key": "idem-" + randSuffix(t),
	})
	if rec := f.sendEvent(t, "evt_ok_"+randSuffix(t), payments.EventSucceeded, intentID, 1200, "USD"); rec.Code != http.StatusOK {
		t.Fatalf("settle: %d", rec.Code)
	}
	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); !d.Allowed {
		t.Fatal("settled subscription did not grant access")
	}

	admin := registerAndGetToken(t, f.handler, "pay-admin-"+randSuffix(t)+"@example.test")
	makeAdmin(t, f.ids, admin)

	// A fan cannot refund, and the amount must be the full charge.
	if rec := doJSON(t, f.handler, http.MethodPost, "/v1/admin/payments/"+intentID+"/refund",
		fan.Tokens.AccessToken, encodeBody(t, map[string]any{"amount_minor": 1200, "reason": "test"})); rec.Code != http.StatusForbidden {
		t.Fatalf("fan refund: want 403, got %d", rec.Code)
	}
	if rec := doJSON(t, f.handler, http.MethodPost, "/v1/admin/payments/"+intentID+"/refund",
		admin.Tokens.AccessToken, encodeBody(t, map[string]any{"amount_minor": 100, "reason": "partial"})); rec.Code != http.StatusBadRequest {
		t.Fatalf("partial refund: want 400, got %d", rec.Code)
	}
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/admin/payments/"+intentID+"/refund",
		admin.Tokens.AccessToken, encodeBody(t, map[string]any{"amount_minor": 1200, "reason": "customer request"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("refund: %d %s", rec.Code, rec.Body.String())
	}

	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); d.Allowed {
		t.Fatal("a refunded subscription still grants access")
	}
	_, _, refundNet := f.ledgerStats(t, "refund:"+intentID)
	if refundNet != 0 {
		t.Fatalf("refund transaction is unbalanced: %d", refundNet)
	}
	net, _, err := ledger.New(f.pool).TrialBalance(context.Background(), "USD")
	if err != nil || net != 0 {
		t.Fatalf("trial balance after refund: net=%d err=%v", net, err)
	}
	// Refund twice: the second call is a no-op that returns the refunded intent.
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/admin/payments/"+intentID+"/refund",
		admin.Tokens.AccessToken, encodeBody(t, map[string]any{"amount_minor": 1200, "reason": "repeat"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("second refund: %d %s", rec.Code, rec.Body.String())
	}
	var refunds int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payments.refunds WHERE intent_id = $1::uuid`, intentID).Scan(&refunds); err != nil {
		t.Fatalf("count refunds: %v", err)
	}
	if refunds != 1 {
		t.Fatalf("refund recorded %d times", refunds)
	}
}

func TestPPVPurchaseUnlocksThenRefundRevokes(t *testing.T) {
	f := newPayFixture(t)
	owner, pair := publishCreator(t, f.handler)
	handle, _ := splitPair(pair)
	_ = handle
	mediaID := uploadMedia(t, f.handler, owner.Tokens.AccessToken, pngFixture(t, 6, 6))
	postID := createPost(t, f.handler, owner.Tokens.AccessToken, map[string]any{
		"body": "ppv", "visibility": creator.VisibilityPPV, "price_minor": 500, "currency": "USD",
		"media_ids": []string{mediaID}, "published": true,
	})
	fan := registerAndGetToken(t, f.handler, "pay-fan-"+randSuffix(t)+"@example.test")

	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID); code != http.StatusForbidden {
		t.Fatalf("ppv media before purchase: want 403, got %d", code)
	}
	intentID, _ := f.intent(t, fan.Tokens.AccessToken, map[string]any{
		"purpose": "ppv", "post_id": postID, "idempotency_key": "idem-" + randSuffix(t),
	})
	if rec := f.sendEvent(t, "evt_ppv_"+randSuffix(t), payments.EventSucceeded, intentID, 500, "USD"); rec.Code != http.StatusOK {
		t.Fatalf("settle ppv: %d %s", rec.Code, rec.Body.String())
	}
	code, url := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID)
	if code != http.StatusCreated {
		t.Fatalf("ppv media after purchase: %d", code)
	}
	if res := fetchDelivery(t, f.handler, url); res.Code != http.StatusOK {
		t.Fatalf("ppv delivery: %d", res.Code)
	}

	admin := registerAndGetToken(t, f.handler, "pay-admin-"+randSuffix(t)+"@example.test")
	makeAdmin(t, f.ids, admin)
	if rec := doJSON(t, f.handler, http.MethodPost, "/v1/admin/payments/"+intentID+"/refund",
		admin.Tokens.AccessToken, encodeBody(t, map[string]any{"amount_minor": 500, "reason": "refund"})); rec.Code != http.StatusOK {
		t.Fatalf("refund ppv: %d %s", rec.Code, rec.Body.String())
	}
	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID); code != http.StatusForbidden {
		t.Fatalf("ppv media after refund: want 403, got %d", code)
	}
}

func TestWebhookRejectsBadSignatureAndWrongAmount(t *testing.T) {
	f := newPayFixture(t)
	_, pair := publishCreator(t, f.handler)
	_, tierID := splitPair(pair)
	fan := registerAndGetToken(t, f.handler, "pay-fan-"+randSuffix(t)+"@example.test")
	intentID, _ := f.intent(t, fan.Tokens.AccessToken, map[string]any{
		"purpose": "subscription", "tier_id": tierID, "idempotency_key": "idem-" + randSuffix(t),
	})

	body := []byte(`{"event_id":"evt_unsigned","kind":"payment.succeeded","intent_id":"` + intentID + `","amount_minor":1200,"currency":"USD"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/webhook", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned webhook: want 401, got %d", rec.Code)
	}

	// Correctly signed, wrong amount: refused, and the intent is untouched.
	if rec := f.sendEvent(t, "evt_wrong_"+randSuffix(t), payments.EventSucceeded, intentID, 100, "USD"); rec.Code != http.StatusConflict {
		t.Fatalf("wrong amount: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, f.handler, http.MethodGet, "/v1/payments/"+intentID, fan.Tokens.AccessToken, "")
	var out struct {
		Payment struct {
			Status string `json:"status"`
		} `json:"payment"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Payment.Status == "succeeded" {
		t.Fatalf("wrong-amount event changed the intent: %s", rec.Body.String())
	}

	// Another account cannot read this intent.
	other := registerAndGetToken(t, f.handler, "pay-other-"+randSuffix(t)+"@example.test")
	if rec := doJSON(t, f.handler, http.MethodGet, "/v1/payments/"+intentID, other.Tokens.AccessToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign intent read: want 404, got %d", rec.Code)
	}
}

func TestSubscriptionExpiryClosesAccessAfterPurchase(t *testing.T) {
	f := newPayFixture(t)
	_, pair := publishCreator(t, f.handler)
	handle, tierID := splitPair(pair)
	fan := registerAndGetToken(t, f.handler, "pay-fan-"+randSuffix(t)+"@example.test")
	intentID, _ := f.intent(t, fan.Tokens.AccessToken, map[string]any{
		"purpose": "subscription", "tier_id": tierID, "idempotency_key": "idem-" + randSuffix(t),
	})
	if rec := f.sendEvent(t, "evt_exp_"+randSuffix(t), payments.EventSucceeded, intentID, 1200, "USD"); rec.Code != http.StatusOK {
		t.Fatalf("settle: %d", rec.Code)
	}
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE creator.entitlements SET expires_at = now() - interval '1 minute'
		WHERE fan_id = $1::uuid`, fan.User.ID); err != nil {
		t.Fatalf("expire entitlement: %v", err)
	}
	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); d.Allowed {
		t.Fatal("an expired subscription still grants access")
	}
}

func TestTrialBalanceEndpointIsAdminOnly(t *testing.T) {
	f := newPayFixture(t)
	fan := registerAndGetToken(t, f.handler, "pay-fan-"+randSuffix(t)+"@example.test")
	if rec := doJSON(t, f.handler, http.MethodGet, "/v1/admin/ledger/trial-balance?currency=USD",
		fan.Tokens.AccessToken, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("fan trial balance: want 403, got %d", rec.Code)
	}
	admin := registerAndGetToken(t, f.handler, "pay-admin-"+randSuffix(t)+"@example.test")
	makeAdmin(t, f.ids, admin)
	rec := doJSON(t, f.handler, http.MethodGet, "/v1/admin/ledger/trial-balance?currency=USD",
		admin.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin trial balance: %d %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"balanced":true`)) {
		t.Fatalf("trial balance not balanced: %s", rec.Body.String())
	}
}
