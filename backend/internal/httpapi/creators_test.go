package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// A server with the creator service wired, using fake payments so activation and
// the entitlement path can be exercised over HTTP. Production activation is a
// payment-provider concern and is documented as pending.
func creatorServer(t *testing.T) (http.Handler, *creator.Service) {
	t.Helper()
	pool := dbtest.Pool(t)
	ids := identity.New(pool,
		identity.WithHasher(identity.NewHasher(identity.TestParams())),
		identity.WithIssuer("Content Platform Test"))
	creators := creator.New(pool, creator.WithFakePayments(true))
	return New(Deps{
		Config:   testConfig(),
		Logger:   loggerDiscard(),
		Identity: ids,
		Creators: creators,
	}), creators
}

func TestCreatorProfileAndTierFlowOverHTTP(t *testing.T) {
	h, _ := creatorServer(t)
	owner := registerAndGetToken(t, h, "mira@example.test")

	// The handle is public but the profile is a draft until it is published.
	rec := doJSON(t, h, http.MethodPost, "/v1/creators", owner.Tokens.AccessToken, encodeBody(t, map[string]any{
		"handle": "mira_art", "display_name": "Mira", "bio": "Abstract work", "category": "art",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created struct {
		Profile struct {
			Handle string `json:"handle"`
		} `json:"profile"`
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if created.Profile.Handle != "mira_art" || !containsString(created.Roles, identity.RoleCreator) {
		t.Fatalf("profile or role missing: %s", rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, "/v1/creators/mira_art", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unpublished profile: want 404, got %d", rec.Code)
	}

	// The creator role is live immediately: publishing needs content.publish.
	rec = doJSON(t, h, http.MethodPatch, "/v1/creators/me", owner.Tokens.AccessToken,
		encodeBody(t, map[string]any{"published": true}))
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, "/v1/creators/mira_art", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public profile: want 200, got %d", rec.Code)
	}

	rec = doJSON(t, h, http.MethodPost, "/v1/creators/me/tiers", owner.Tokens.AccessToken,
		encodeBody(t, map[string]any{
			"name": "Inner Circle", "price_minor": 1200, "currency": "USD",
			"description": "Weekly set", "perks": []string{"Everything in Public"},
		}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tier: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var tierBody struct {
		Tier struct {
			ID string `json:"id"`
		} `json:"tier"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tierBody); err != nil {
		t.Fatalf("decode tier: %v", err)
	}

	// A fan without the permission cannot manage tiers on someone else's profile.
	fan := registerAndGetToken(t, h, "fan@example.test")
	rec = doJSON(t, h, http.MethodPost, "/v1/creators/me/tiers", fan.Tokens.AccessToken,
		encodeBody(t, map[string]any{"name": "Cheap", "price_minor": 100, "currency": "USD"}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("fan creating a tier: want 403, got %d", rec.Code)
	}

	// Subscribe: fake payments activate immediately, so access flips to allowed.
	rec = doJSON(t, h, http.MethodPost, "/v1/creators/mira_art/subscribe", fan.Tokens.AccessToken,
		encodeBody(t, map[string]any{"tier_id": tierBody.Tier.ID}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodGet, "/v1/creators/mira_art/access?scope=subscriber", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("access check: want 200, got %d", rec.Code)
	}
	var decision struct {
		Access creator.Access `json:"access"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode access: %v", err)
	}
	if !decision.Access.Allowed {
		t.Fatalf("subscriber denied: %s", rec.Body.String())
	}

	// A different fan is still refused.
	other := registerAndGetToken(t, h, "other@example.test")
	rec = doJSON(t, h, http.MethodGet, "/v1/creators/mira_art/access?scope=subscriber", other.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("access check: want 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode access: %v", err)
	}
	if decision.Access.Allowed || decision.Access.Reason != "entitlement_required" {
		t.Fatalf("unrelated fan was allowed: %s", rec.Body.String())
	}

	// Cancelling keeps access to the end of the period.
	var subBody struct {
		Subscription struct {
			ID string `json:"id"`
		} `json:"subscription"`
	}
	rec = doJSON(t, h, http.MethodGet, "/v1/me/subscriptions", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list subscriptions: want 200, got %d", rec.Code)
	}
	var list struct {
		Subscriptions []struct {
			ID string `json:"id"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Subscriptions) != 1 {
		t.Fatalf("subscription list: %s err=%v", rec.Body.String(), err)
	}
	subBody.Subscription.ID = list.Subscriptions[0].ID

	rec = doJSON(t, h, http.MethodPost, "/v1/subscriptions/"+subBody.Subscription.ID+"/cancel",
		fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodGet, "/v1/creators/mira_art/access?scope=subscriber", fan.Tokens.AccessToken, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode access: %v", err)
	}
	if !decision.Access.Allowed {
		t.Fatalf("access lost at cancellation instead of period end: %s", rec.Body.String())
	}
}

func TestCreatorWritesNeedAuthentication(t *testing.T) {
	h, _ := creatorServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/creators", `{"handle":"x","display_name":"x"}`},
		{http.MethodPatch, "/v1/creators/me", `{}`},
		{http.MethodPost, "/v1/creators/me/tiers", `{}`},
		{http.MethodPost, "/v1/creators/mira_art/subscribe", `{"tier_id":"00000000-0000-4000-8000-000000000000"}`},
		{http.MethodGet, "/v1/creators/mira_art/access", ""},
		{http.MethodGet, "/v1/me/subscriptions", ""},
	} {
		rec := doJSON(t, h, tc.method, tc.path, "", tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s anonymous: want 401, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestReadinessReportsRateLimitState(t *testing.T) {
	// No shared backend: the instance is rate limiting in-process and must say so.
	h, _ := creatorServer(t)
	rec := doJSON(t, h, http.MethodGet, "/readyz", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz: want 200 or 503, got %d", rec.Code)
	}
	var body struct {
		Checks struct {
			Database  string `json:"database"`
			RateLimit string `json:"rate_limit"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Checks.RateLimit != "in_process" {
		t.Fatalf("rate_limit state: want in_process, got %q", body.Checks.RateLimit)
	}
}

func TestReadinessFailsWhenTheSharedLimiterIsDown(t *testing.T) {
	url := os.Getenv("VALKEY_URL")
	if url == "" {
		t.Skip("VALKEY_URL is not set; the readiness check needs a Redis service")
	}
	backend, err := ratelimit.New(context.Background(), url, "readyz:")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	pool := dbtest.Pool(t)
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	h := New(Deps{
		Config:   testConfig(),
		Logger:   loggerDiscard(),
		Identity: ids,
		Limits:   backend,
	})
	rec := doJSON(t, h, http.MethodGet, "/readyz", "", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with a dead limiter: want 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Checks struct {
			RateLimit string `json:"rate_limit"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body.Status != "degraded" || body.Checks.RateLimit != "unreachable" {
		t.Fatalf("unexpected readiness body: %s", rec.Body.String())
	}
}
