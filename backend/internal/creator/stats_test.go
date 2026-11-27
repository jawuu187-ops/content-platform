package creator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

type statsFixture struct {
	pool *pgxpool.Pool
	ids  *identity.Service
	svc  *Service
	now  time.Time
}

func newStatsFixture(t *testing.T) *statsFixture {
	t.Helper()
	pool := dbtest.Pool(t)
	now := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	return &statsFixture{
		pool: pool,
		now:  now,
		ids:  identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams()))),
		svc: New(pool,
			WithFakePayments(true),
			WithAgeGate(allowAllGate{}),
			WithClock(func() time.Time { return now })),
	}
}

type allowAllGate struct{}

func (allowAllGate) AllowAdult(context.Context, string) (bool, error) { return true, nil }

func statsHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func (f *statsFixture) account(t *testing.T) identity.Principal {
	t.Helper()
	res, err := f.ids.Register(context.Background(), "stats-"+statsHex(6)+"@example.test",
		"a-very-long-password", identity.DeviceInfo{Fingerprint: "stats"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal
}

func (f *statsFixture) creator(t *testing.T, handle string) identity.Principal {
	t.Helper()
	p := f.account(t)
	if _, err := f.svc.CreateProfile(context.Background(), p, ProfileInput{
		Handle: handle, DisplayName: "Mira", Bio: "", Category: "art", Publish: ptrBool(true),
	}, ""); err != nil {
		t.Fatalf("profile: %v", err)
	}
	return p
}

func (f *statsFixture) tier(t *testing.T, owner identity.Principal, name string, price int64) Tier {
	t.Helper()
	tier, err := f.svc.CreateTier(context.Background(), owner, TierInput{
		Name: name, PriceMinor: &price, Currency: "USD",
	}, "")
	if err != nil {
		t.Fatalf("tier: %v", err)
	}
	return tier
}

// subscribe inserts a membership row directly. Activation normally goes through a
// payment; the stats query only reads the row, so a fixture is enough.
func (f *statsFixture) subscribe(t *testing.T, fan, owner identity.Principal, tier Tier, status string, end time.Time) {
	t.Helper()
	var start any
	var endValue any
	if status == StatusActive {
		start = f.now.AddDate(0, 0, -5)
		endValue = end
	}
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO creator.subscriptions
		    (fan_id, creator_id, tier_id, status, price_minor, currency,
		     current_period_start, current_period_end)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, 'USD', $6, $7)`,
		fan.UserID, owner.UserID, tier.ID, status, tier.PriceMinor, start, endValue); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
}

func (f *statsFixture) post(t *testing.T, owner identity.Principal) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO creator.posts (creator_id, body, visibility, price_minor, currency, status, published_at)
		VALUES ($1::uuid, 'ppv', 'ppv', 500, 'USD', 'published', $2)
		RETURNING id::text`, owner.UserID, f.now.AddDate(0, 0, -10)).Scan(&id); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	return id
}

func (f *statsFixture) unlock(t *testing.T, postID string, fan identity.Principal, source string, at time.Time, revoked bool) {
	t.Helper()
	var revokedAt any
	if revoked {
		revokedAt = f.now
	}
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO creator.post_unlocks (post_id, fan_id, source, granted_at, revoked_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)`,
		postID, fan.UserID, source, at, revokedAt); err != nil {
		t.Fatalf("insert unlock: %v", err)
	}
}

func TestStatsCountsLiveMembersAndPaidUnlocks(t *testing.T) {
	f := newStatsFixture(t)
	ctx := context.Background()
	owner := f.creator(t, "mira_stats")
	otherOwner := f.creator(t, "other_stats")
	studio := f.tier(t, owner, "Studio", 900)
	collector := f.tier(t, owner, "Collector", 2000)
	otherTier := f.tier(t, otherOwner, "Other", 700)

	// Three live members: two on Studio, one on Collector.
	fanA, fanB, fanC := f.account(t), f.account(t), f.account(t)
	f.subscribe(t, fanA, owner, studio, StatusActive, f.now.AddDate(0, 0, 20))
	f.subscribe(t, fanB, owner, studio, StatusActive, f.now.AddDate(0, 0, 10))
	f.subscribe(t, fanC, owner, collector, StatusActive, f.now.AddDate(0, 0, 5))

	// Rows that must not count: a lapsed period, a pending first payment, a
	// support/admin entitlement with no subscription, and another creator's member.
	f.subscribe(t, f.account(t), owner, studio, StatusActive, f.now.AddDate(0, 0, -1))
	f.subscribe(t, f.account(t), owner, studio, StatusPending, f.now)
	adminFan := f.account(t)
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'admin', $3)`,
		adminFan.UserID, owner.UserID, f.now.AddDate(0, 0, 30)); err != nil {
		t.Fatalf("admin entitlement: %v", err)
	}
	f.subscribe(t, f.account(t), otherOwner, otherTier, StatusActive, f.now.AddDate(0, 0, 20))

	// Two paid unlocks in the window, one revoked, one old, one admin grant, and
	// one on the other creator's post.
	post := f.post(t, owner)
	f.unlock(t, post, fanA, "purchase", f.now.AddDate(0, 0, -1), false)
	f.unlock(t, post, fanB, "purchase", f.now.AddDate(0, 0, -6), false)
	f.unlock(t, post, fanC, "purchase", f.now.AddDate(0, 0, -2), true)
	f.unlock(t, post, f.account(t), "purchase", f.now.AddDate(0, 0, -8), false)
	f.unlock(t, post, f.account(t), "admin", f.now.AddDate(0, 0, -1), false)
	otherPost := f.post(t, otherOwner)
	f.unlock(t, otherPost, f.account(t), "purchase", f.now.AddDate(0, 0, -1), false)

	stats, err := f.svc.Stats(ctx, owner.UserID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Members != 3 {
		t.Fatalf("members = %d, want 3 (lapsed, pending, admin and foreign rows excluded)", stats.Members)
	}
	if stats.Unlocks7d != 2 {
		t.Fatalf("unlocks7d = %d, want 2 (revoked, old, admin and foreign rows excluded)", stats.Unlocks7d)
	}
	if len(stats.Tiers) != 2 {
		t.Fatalf("tiers = %d, want 2: %+v", len(stats.Tiers), stats.Tiers)
	}
	if stats.Tiers[0].Name != studio.Name || stats.Tiers[0].Members != 2 || stats.Tiers[0].ShareBPS != 6666 {
		t.Fatalf("top tier wrong: %+v", stats.Tiers[0])
	}
	if stats.Tiers[1].Name != collector.Name || stats.Tiers[1].Members != 1 || stats.Tiers[1].ShareBPS != 3333 {
		t.Fatalf("second tier wrong: %+v", stats.Tiers[1])
	}
	if stats.TopTier == nil || stats.TopTier.TierID != studio.ID {
		t.Fatalf("top tier = %+v, want Studio", stats.TopTier)
	}

	// The other creator sees only their own single member.
	otherStats, err := f.svc.Stats(ctx, otherOwner.UserID)
	if err != nil {
		t.Fatalf("other stats: %v", err)
	}
	if otherStats.Members != 1 || len(otherStats.Tiers) != 1 || otherStats.Unlocks7d != 1 {
		t.Fatalf("stats leaked between creators: %+v", otherStats)
	}
}

func TestStatsWithoutMembersHasNoTopTier(t *testing.T) {
	f := newStatsFixture(t)
	ctx := context.Background()
	owner := f.creator(t, "empty_stats")

	stats, err := f.svc.Stats(ctx, owner.UserID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Members != 0 || stats.Unlocks7d != 0 || len(stats.Tiers) != 0 || stats.TopTier != nil {
		t.Fatalf("an empty studio reported numbers: %+v", stats)
	}
}

func TestStatsRefusesAnAccountWithoutACreatorProfile(t *testing.T) {
	f := newStatsFixture(t)
	fan := f.account(t)

	if _, err := f.svc.Stats(context.Background(), fan.UserID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("fan stats: want ErrForbidden, got %v", err)
	}
}
