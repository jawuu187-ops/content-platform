package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

type statsFixture struct {
	handler http.Handler
	pool    *pgxpool.Pool
}

func newStatsServer(t *testing.T) *statsFixture {
	t.Helper()
	pool := dbtest.Pool(t)
	ids := identity.New(pool,
		identity.WithHasher(identity.NewHasher(identity.TestParams())),
		identity.WithIssuer("Content Platform Test"))
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(permissiveGate{}))
	return &statsFixture{
		handler: New(Deps{Config: testConfig(), Logger: loggerDiscard(), Identity: ids, Creators: creators}),
		pool:    pool,
	}
}

func TestCreatorStatsIsTheCallersOwnAudienceWithNoStore(t *testing.T) {
	f := newStatsServer(t)
	owner := registerAndGetToken(t, f.handler, "mira-stats@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators", owner.Tokens.AccessToken,
		encodeBody(t, map[string]any{
			"handle": "mira_stats", "display_name": "Mira", "bio": "", "category": "art",
		}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profile: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/creators/me/tiers", owner.Tokens.AccessToken,
		encodeBody(t, map[string]any{"name": "Studio", "price_minor": 900, "currency": "USD"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tier: %d %s", rec.Code, rec.Body.String())
	}
	var tierBody struct {
		Tier struct {
			ID string `json:"id"`
		} `json:"tier"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tierBody); err != nil {
		t.Fatalf("decode tier: %v", err)
	}

	// A live paid member and an admin grant. Only the member is an audience.
	fan := registerAndGetToken(t, f.handler, "fan-stats@example.test")
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO creator.subscriptions
		    (fan_id, creator_id, tier_id, status, price_minor, currency,
		     current_period_start, current_period_end)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'active', 900, 'USD', now() - interval '1 day', now() + interval '29 days')`,
		fan.User.ID, owner.User.ID, tierBody.Tier.ID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	grantee := registerAndGetToken(t, f.handler, "grantee-stats@example.test")
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'admin', now() + interval '1 day')`,
		grantee.User.ID, owner.User.ID); err != nil {
		t.Fatalf("insert admin grant: %v", err)
	}

	rec = doJSON(t, f.handler, http.MethodGet, "/v1/creator/stats", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("an audience read was cacheable")
	}
	var body struct {
		Stats creator.Stats `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if body.Stats.Members != 1 {
		t.Fatalf("members = %d, want 1 (the admin grant is not a member)", body.Stats.Members)
	}
	if body.Stats.TopTier == nil || body.Stats.TopTier.Name != "Studio" || body.Stats.TopTier.ShareBPS != 10_000 {
		t.Fatalf("top tier wrong: %+v", body.Stats.TopTier)
	}
	if body.Stats.CreatorID != owner.User.ID {
		t.Fatalf("stats reported another creator: %s", body.Stats.CreatorID)
	}

	// A fan has no studio, so the route refuses rather than returning zeros.
	rec = doJSON(t, f.handler, http.MethodGet, "/v1/creator/stats", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("fan stats: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	// And no session at all is a 401 from the shared middleware.
	rec = doJSON(t, f.handler, http.MethodGet, "/v1/creator/stats", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous stats: want 401, got %d", rec.Code)
	}
}
