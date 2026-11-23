package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/agegate"
	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const ageTestSecret = "http-agegate-secret-0123456789"

// webhookTestProvider can start a hosted check and verifies callbacks with the
// real HMAC verifier, so the HTTP tests exercise the production signature code.
type webhookTestProvider struct {
	mu      sync.Mutex
	webhook *agegate.WebhookProvider
	lastRef string
	n       int
	start   bool
}

func (p *webhookTestProvider) Name() string { return "testco" }

func (p *webhookTestProvider) Start(context.Context, agegate.StartInput) (agegate.StartOutput, error) {
	if !p.start {
		return agegate.StartOutput{}, agegate.ErrStartUnsupported
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	p.lastRef = fmt.Sprintf("http_ref_%d", p.n)
	return agegate.StartOutput{
		Reference: p.lastRef,
		CheckURL:  "https://verify.example.test/s/" + p.lastRef,
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (p *webhookTestProvider) ParseCallback(h http.Header, b []byte, now time.Time) (agegate.Callback, error) {
	return p.webhook.ParseCallback(h, b, now)
}

func (p *webhookTestProvider) Capabilities() agegate.Capabilities {
	return agegate.Capabilities{Name: "testco", StartSupported: p.start, CallbackSupport: true}
}

type ageFixture struct {
	pool     *pgxpool.Pool
	handler  http.Handler
	provider *webhookTestProvider
	ids      *identity.Service
}

func newAgeFixture(t *testing.T, startSupported bool) *ageFixture {
	t.Helper()
	pool := dbtest.Pool(t)
	ids := identity.New(pool,
		identity.WithHasher(identity.NewHasher(identity.TestParams())),
		identity.WithIssuer("Content Platform Test"))
	webhook, err := agegate.NewWebhookProvider("testco", []byte(ageTestSecret))
	if err != nil {
		t.Fatalf("webhook provider: %v", err)
	}
	provider := &webhookTestProvider{webhook: webhook, start: startSupported}
	svc := agegate.New(pool, agegate.WithProvider(provider), agegate.WithValidity(30*24*time.Hour))
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(svc))
	h := New(Deps{
		Config:   testConfig(),
		Logger:   loggerDiscard(),
		Identity: ids,
		Creators: creators,
		AgeGate:  svc,
	})
	return &ageFixture{pool: pool, handler: h, provider: provider, ids: ids}
}

func signAge(t *testing.T, body []byte, at time.Time) http.Header {
	t.Helper()
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(ageTestSecret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	h := http.Header{}
	h.Set(agegate.SignatureHeader, "t="+ts+",v1="+hex.EncodeToString(mac.Sum(nil)))
	h.Set("Content-Type", "application/json")
	return h
}

func ageCallback(t *testing.T, accountID, reference, eventID, status string, verifiedAt time.Time, expiresAt *time.Time) (http.Header, []byte) {
	t.Helper()
	payload := map[string]any{
		"event_id": eventID, "reference": reference, "account_id": accountID,
		"status": status, "assurance": "document_and_liveness", "min_age": 18,
	}
	if !verifiedAt.IsZero() {
		payload["verified_at"] = verifiedAt.UTC().Format(time.RFC3339)
	}
	if expiresAt != nil {
		payload["expires_at"] = expiresAt.UTC().Format(time.RFC3339)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return signAge(t, body, time.Now()), body
}

func postCallback(t *testing.T, h http.Handler, headers http.Header, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return doHandler(t, h, http.MethodPost, "/v1/verify/age/callback", headers, body)
}

// doHandler is doJSON with explicit headers, for the signed callback.
func doHandler(t *testing.T, h http.Handler, method, path string, headers http.Header, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// publishCreator registers a creator, publishes the profile and returns a tier id.
func publishCreator(t *testing.T, h http.Handler) (authBody, string) {
	t.Helper()
	owner := registerAndGetToken(t, h, "age-creator-"+randSuffix(t)+"@example.test")
	rec := doJSON(t, h, http.MethodPost, "/v1/creators", owner.Tokens.AccessToken, encodeBody(t, map[string]any{
		"handle": "age_studio_" + randSuffix(t), "display_name": "Age Studio", "category": "art",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Profile struct {
			Handle string `json:"handle"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	rec = doJSON(t, h, http.MethodPatch, "/v1/creators/me", owner.Tokens.AccessToken,
		encodeBody(t, map[string]any{"published": true}))
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodPost, "/v1/creators/me/tiers", owner.Tokens.AccessToken,
		encodeBody(t, map[string]any{"name": "All Access", "price_minor": 1200, "currency": "USD"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tier: %d %s", rec.Code, rec.Body.String())
	}
	var tier struct {
		Tier struct {
			ID string `json:"id"`
		} `json:"tier"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tier); err != nil {
		t.Fatalf("decode tier: %v", err)
	}
	return owner, created.Profile.Handle + "|" + tier.Tier.ID
}

func accessDecision(t *testing.T, h http.Handler, token, handle string) creator.Access {
	t.Helper()
	rec := doJSON(t, h, http.MethodGet, "/v1/creators/"+handle+"/access?scope=subscriber", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("access: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Access creator.Access `json:"access"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode access: %v", err)
	}
	return body.Access
}

// TestUnverifiedSubscriberCannotAccessPaidContent is the headline acceptance
// test: a paid, in-period subscription does not unlock 18+ content until the
// account has completed age verification.
func TestUnverifiedSubscriberCannotAccessPaidContent(t *testing.T) {
	f := newAgeFixture(t, true)
	_, handleAndTier := publishCreator(t, f.handler)
	handle, tierID := splitPair(handleAndTier)

	fan := registerAndGetToken(t, f.handler, "age-fan-"+randSuffix(t)+"@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators/"+handle+"/subscribe", fan.Tokens.AccessToken,
		encodeBody(t, map[string]any{"tier_id": tierID}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}

	before := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle)
	if before.Allowed || before.Reason != creator.ReasonAgeRequired {
		t.Fatalf("unverified subscriber was allowed: %+v", before)
	}

	rec = doJSON(t, f.handler, http.MethodPost, "/v1/verify/age/start", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	var started struct {
		Verification agegate.State `json:"verification"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if started.Verification.Status != agegate.StatusPending || started.Verification.ShowURL == "" {
		t.Fatalf("start payload: %s", rec.Body.String())
	}
	if after := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); after.Allowed {
		t.Fatalf("a pending verification unlocked content: %+v", after)
	}

	expires := time.Now().Add(30 * 24 * time.Hour)
	headers, body := ageCallback(t, fan.User.ID, f.provider.lastRef, "evt_ok", "verified", time.Now(), &expires)
	if rec := postCallback(t, f.handler, headers, body); rec.Code != http.StatusAccepted {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body.String())
	}

	after := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle)
	if !after.Allowed {
		t.Fatalf("verified subscriber was denied: %+v", after)
	}

	rec = doJSON(t, f.handler, http.MethodGet, "/v1/verify/age", fan.Tokens.AccessToken, "")
	var state struct {
		Verification agegate.State `json:"verification"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !state.Verification.Verified || state.Verification.Status != agegate.StatusVerified {
		t.Fatalf("status after verification: %s", rec.Body.String())
	}
}

func TestRevokedVerificationClosesContentImmediately(t *testing.T) {
	f := newAgeFixture(t, true)
	_, handleAndTier := publishCreator(t, f.handler)
	handle, tierID := splitPair(handleAndTier)
	fan := registerAndGetToken(t, f.handler, "age-fan-"+randSuffix(t)+"@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators/"+handle+"/subscribe", fan.Tokens.AccessToken,
		encodeBody(t, map[string]any{"tier_id": tierID}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d", rec.Code)
	}
	verifyAccount(t, f, fan.Tokens.AccessToken, fan.User.ID, "evt_rev_ok_"+randSuffix(t), time.Now().Add(24*time.Hour))
	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); !d.Allowed {
		t.Fatalf("verified subscriber denied: %+v", d)
	}

	// Revocation needs an administrator. The role grant goes through the identity
	// service, which is how a real operator would be granted it.
	admin := registerAndGetToken(t, f.handler, "age-admin-"+randSuffix(t)+"@example.test")
	if _, err := f.ids.GrantRole(context.Background(), identity.Principal{UserID: "operator", Roles: []string{identity.RoleAdmin}},
		admin.User.ID, identity.RoleAdmin, ""); err != nil {
		t.Fatalf("grant admin: %v", err)
	}
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/admin/verify/age/"+fan.User.ID+"/revoke",
		admin.Tokens.AccessToken, encodeBody(t, map[string]any{"reason": "provider withdrew the result"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); d.Allowed {
		t.Fatalf("revoked verification still allowed content: %+v", d)
	}

	// A fan cannot revoke anything.
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/admin/verify/age/"+fan.User.ID+"/revoke",
		fan.Tokens.AccessToken, encodeBody(t, map[string]any{"reason": "let me in"}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("fan revoke: want 403, got %d", rec.Code)
	}
}

func TestExpiredVerificationClosesContent(t *testing.T) {
	f := newAgeFixture(t, true)
	_, handleAndTier := publishCreator(t, f.handler)
	handle, tierID := splitPair(handleAndTier)
	fan := registerAndGetToken(t, f.handler, "age-fan-"+randSuffix(t)+"@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators/"+handle+"/subscribe", fan.Tokens.AccessToken,
		encodeBody(t, map[string]any{"tier_id": tierID}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d", rec.Code)
	}
	// The provider reports a verification whose validity already ended.
	verifiedAt := time.Now().Add(-2 * time.Hour)
	expiresAt := time.Now().Add(-time.Hour)
	if rec := doJSON(t, f.handler, http.MethodPost, "/v1/verify/age/start", fan.Tokens.AccessToken, ""); rec.Code != http.StatusCreated {
		t.Fatalf("start: %d", rec.Code)
	}
	headers, body := ageCallback(t, fan.User.ID, f.provider.lastRef, "evt_expired_"+randSuffix(t),
		"verified", verifiedAt, &expiresAt)
	if rec := postCallback(t, f.handler, headers, body); rec.Code != http.StatusAccepted {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body.String())
	}
	if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); d.Allowed {
		t.Fatalf("expired verification allowed content: %+v", d)
	}
	rec = doJSON(t, f.handler, http.MethodGet, "/v1/verify/age", fan.Tokens.AccessToken, "")
	var state struct {
		Verification agegate.State `json:"verification"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.Verification.Status != agegate.StatusExpired || state.Verification.Verified {
		t.Fatalf("expired status: %s", rec.Body.String())
	}
}

func TestCallbackNeedsValidSignatureAndIsIdempotent(t *testing.T) {
	f := newAgeFixture(t, true)
	fan := registerAndGetToken(t, f.handler, "age-fan-"+randSuffix(t)+"@example.test")
	if rec := doJSON(t, f.handler, http.MethodPost, "/v1/verify/age/start", fan.Tokens.AccessToken, ""); rec.Code != http.StatusCreated {
		t.Fatalf("start: %d", rec.Code)
	}
	expires := time.Now().Add(24 * time.Hour)
	_, body := ageCallback(t, fan.User.ID, f.provider.lastRef, "evt_sig_"+randSuffix(t), "verified", time.Now(), &expires)

	rec := postCallback(t, f.handler, http.Header{"Content-Type": []string{"application/json"}}, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned callback: want 401, got %d %s", rec.Code, rec.Body.String())
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] = 'x'
	headers := signAge(t, body, time.Now())
	if rec := postCallback(t, f.handler, headers, tampered); rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered callback: want 401, got %d", rec.Code)
	}

	rec = postCallback(t, f.handler, headers, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid callback: %d %s", rec.Code, rec.Body.String())
	}
	// Replay with the same event id: accepted as already applied, never re-applied.
	replayHeaders := signAge(t, body, time.Now())
	rec = postCallback(t, f.handler, replayHeaders, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("replay: got %d", rec.Code)
	}
	var applied struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if applied.Status != "already_applied" {
		t.Fatalf("replay was applied twice: %s", rec.Body.String())
	}
	rec = doJSON(t, f.handler, http.MethodGet, "/v1/verify/age", fan.Tokens.AccessToken, "")
	var state struct {
		Verification agegate.State `json:"verification"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !state.Verification.Verified {
		t.Fatalf("a valid signed callback did not verify the account: %s", rec.Body.String())
	}
}

func TestStartIsHonestWhenNoHostedCheckExists(t *testing.T) {
	f := newAgeFixture(t, false) // callback verification only, no hosted session
	fan := registerAndGetToken(t, f.handler, "age-fan-"+randSuffix(t)+"@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/verify/age/start", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("start without a hosted check: want 503, got %d %s", rec.Code, rec.Body.String())
	}
	if p := decodeProblem(t, rec); p.Code != "verification_not_integrated" {
		t.Fatalf("problem code: %q", p.Code)
	}

	// No provider at all is also 503, with its own code.
	ids := identity.New(f.pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	svc := agegate.New(f.pool)
	h := New(Deps{Config: testConfig(), Logger: loggerDiscard(), Identity: ids, AgeGate: svc})
	rec = doJSON(t, h, http.MethodPost, "/v1/verify/age/start", fan.Tokens.AccessToken, "")
	// The token belongs to another server instance but the same database, so the
	// session is valid here as well.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("start with no provider: want 503, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestConcurrentAccessDuringRevocation proves a revocation cannot be raced into
// an authorization bypass: once the revoke response is returned, every later
// decision is deny, and no request during the change is answered with 5xx.
func TestConcurrentAccessDuringRevocation(t *testing.T) {
	f := newAgeFixture(t, true)
	_, handleAndTier := publishCreator(t, f.handler)
	handle, tierID := splitPair(handleAndTier)
	fan := registerAndGetToken(t, f.handler, "age-fan-"+randSuffix(t)+"@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators/"+handle+"/subscribe", fan.Tokens.AccessToken,
		encodeBody(t, map[string]any{"tier_id": tierID}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d", rec.Code)
	}
	verifyAccount(t, f, fan.Tokens.AccessToken, fan.User.ID, "evt_conc_"+randSuffix(t), time.Now().Add(24*time.Hour))

	admin := registerAndGetToken(t, f.handler, "age-admin-"+randSuffix(t)+"@example.test")
	if _, err := f.ids.GrantRole(context.Background(), identity.Principal{UserID: "operator", Roles: []string{identity.RoleAdmin}},
		admin.User.ID, identity.RoleAdmin, ""); err != nil {
		t.Fatalf("grant admin: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan string, 64)
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 5; j++ {
				rec := doJSON(t, f.handler, http.MethodGet, "/v1/creators/"+handle+"/access?scope=subscriber",
					fan.Tokens.AccessToken, "")
				if rec.Code != http.StatusOK {
					errs <- fmt.Sprintf("status %d", rec.Code)
				}
			}
		}()
	}
	close(start)
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/admin/verify/age/"+fan.User.ID+"/revoke",
		admin.Tokens.AccessToken, encodeBody(t, map[string]any{"reason": "concurrency test"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Fatalf("concurrent access request failed: %s", msg)
	}

	for i := 0; i < 10; i++ {
		if d := accessDecision(t, f.handler, fan.Tokens.AccessToken, handle); d.Allowed {
			t.Fatalf("access allowed after revocation completed: %+v", d)
		}
	}
}

func verifyAccount(t *testing.T, f *ageFixture, token, accountID, eventID string, expiresAt time.Time) {
	t.Helper()
	if rec := doJSON(t, f.handler, http.MethodPost, "/v1/verify/age/start", token, ""); rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	headers, body := ageCallback(t, accountID, f.provider.lastRef, eventID, "verified", time.Now(), &expiresAt)
	if rec := postCallback(t, f.handler, headers, body); rec.Code != http.StatusAccepted {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body.String())
	}
}

func randSuffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(buf)
}

func splitPair(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
