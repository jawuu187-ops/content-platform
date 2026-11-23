package creator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const goodPassword = "a-very-long-password"

// stubGate stands in for the age-verification service so creator logic can be
// tested without a provider. The real gate is exercised in package agegate and
// over HTTP in package httpapi.
type stubGate struct {
	allow bool
	err   error
}

func (g stubGate) AllowAdult(context.Context, string) (bool, error) { return g.allow, g.err }

type harness struct {
	svc *Service
	ids *identity.Service
	now time.Time
}

func newHarness(t *testing.T) *harness { return newHarnessWithGate(t, stubGate{allow: true}) }

func newHarnessWithGate(t *testing.T, gate AdultGate) *harness {
	t.Helper()
	h := &harness{now: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)}
	pool := dbtest.Pool(t)
	h.ids = identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	h.svc = New(pool, WithFakePayments(true), WithAgeGate(gate), WithClock(func() time.Time { return h.now }))
	return h
}

func uniqueEmail(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(buf) + "@example.test"
}

func (h *harness) account(t *testing.T) identity.Principal {
	t.Helper()
	res, err := h.ids.Register(context.Background(), uniqueEmail("user"), goodPassword,
		identity.DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal
}

func (h *harness) creator(t *testing.T) (identity.Principal, Profile, Tier) {
	t.Helper()
	p := h.account(t)
	handle := "studio" + hex.EncodeToString(randomBytes(4))
	profile, err := h.svc.CreateProfile(context.Background(), p, ProfileInput{
		Handle: handle, DisplayName: "Mira", Bio: "Abstract work", Category: "art", Publish: ptrBool(true),
	}, "")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	tier, err := h.svc.CreateTier(context.Background(), p, TierInput{
		Name: "Inner Circle", PriceMinor: ptrInt64(1200), Currency: "USD",
		Description: "Weekly set", Perks: []string{"Everything in Public"},
	}, "")
	if err != nil {
		t.Fatalf("create tier: %v", err)
	}
	return p, profile, tier
}

func ptrInt64(v int64) *int64 { return &v }

func ptrBool(v bool) *bool { return &v }

func randomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return buf
}

func TestProfileLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p := h.account(t)

	if _, err := h.svc.CreateProfile(ctx, p, ProfileInput{Handle: "admin", DisplayName: "x"}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("reserved handle: want ErrInvalidInput, got %v", err)
	}
	if _, err := h.svc.CreateProfile(ctx, p, ProfileInput{Handle: "AB", DisplayName: "x"}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short handle: want ErrInvalidInput, got %v", err)
	}

	profile, err := h.svc.CreateProfile(ctx, p, ProfileInput{
		Handle: "Mira_Art", DisplayName: "Mira", Category: "art",
	}, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if profile.Handle != "mira_art" {
		t.Fatalf("handle was not normalised: %q", profile.Handle)
	}

	// Published=false: the public endpoint must not reveal the draft.
	if _, _, err := h.svc.PublicProfile(ctx, "mira_art"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft is publicly visible: %v", err)
	}

	if _, err := h.svc.CreateProfile(ctx, h.account(t), ProfileInput{Handle: "mira_art", DisplayName: "Other"}, ""); !errors.Is(err, ErrHandleTaken) {
		t.Fatalf("duplicate handle: want ErrHandleTaken, got %v", err)
	}

	if _, err := h.svc.UpdateProfile(ctx, p, ProfileInput{Bio: "New bio", Publish: ptrBool(true)}, ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	public, tiers, err := h.svc.PublicProfile(ctx, "mira_art")
	if err != nil {
		t.Fatalf("public profile: %v", err)
	}
	if !public.Published || len(tiers) != 0 {
		t.Fatalf("unexpected public view: %+v tiers=%d", public, len(tiers))
	}
}

func TestTierValidationAndRetirement(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, tier := h.creator(t)

	zero := int64(0)
	if _, err := h.svc.CreateTier(ctx, owner, TierInput{Name: "Free", PriceMinor: &zero, Currency: "USD"}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("free tier: want ErrInvalidInput, got %v", err)
	}
	if _, err := h.svc.CreateTier(ctx, owner, TierInput{Name: "Yen", PriceMinor: ptrInt64(500), Currency: "JPY"}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported currency: want ErrInvalidInput, got %v", err)
	}
	dup := "inner circle"
	if _, err := h.svc.CreateTier(ctx, owner, TierInput{Name: dup, PriceMinor: ptrInt64(900), Currency: "USD"}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate tier name (case-insensitive): want ErrInvalidInput, got %v", err)
	}

	// A tier belongs to its creator: another creator cannot edit it.
	otherOwner := h.account(t)
	if _, err := h.svc.UpdateTier(ctx, otherOwner, tier.ID, TierInput{Name: "Hijacked"}, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign tier edit: want ErrNotFound, got %v", err)
	}

	newPrice := int64(1500)
	updated, err := h.svc.UpdateTier(ctx, owner, tier.ID, TierInput{PriceMinor: &newPrice}, "")
	if err != nil {
		t.Fatalf("update price: %v", err)
	}
	if updated.PriceMinor != 1500 || updated.Name != tier.Name {
		t.Fatalf("partial update changed unrelated fields: %+v", updated)
	}

	if err := h.svc.DeactivateTier(ctx, owner, tier.ID, ""); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	tiers, err := h.svc.TiersOf(ctx, profile.Handle)
	if err != nil {
		t.Fatalf("list tiers: %v", err)
	}
	if len(tiers) != 0 {
		t.Fatalf("retired tier is still listed: %+v", tiers)
	}
	// The row is kept, because subscriptions reference the price they were sold at.
	if _, err := h.svc.tierByID(ctx, tier.ID); err != nil {
		t.Fatalf("retired tier row was deleted: %v", err)
	}
}

func TestSubscriptionGrantsAndRevokesAccess(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, tier := h.creator(t)
	fan := h.account(t)

	denied, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil {
		t.Fatalf("access before subscribing: %v", err)
	}
	if denied.Allowed || denied.Reason != "entitlement_required" {
		t.Fatalf("unsubscribed fan was allowed: %+v", denied)
	}

	sub, err := h.svc.Subscribe(ctx, fan, profile.Handle, tier.ID, "")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if sub.Status != StatusActive || sub.PeriodEnd == nil {
		t.Fatalf("fake payments did not activate: %+v", sub)
	}
	if sub.PriceMinor != 1200 || sub.Currency != "USD" {
		t.Fatalf("subscription did not snapshot the price: %+v", sub)
	}

	allowed, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil || !allowed.Allowed {
		t.Fatalf("subscriber denied: %+v err=%v", allowed, err)
	}
	if allowed.Reason != "entitlement" && allowed.Reason != "subscription" {
		t.Fatalf("unexpected reason: %q", allowed.Reason)
	}

	if _, err := h.svc.Subscribe(ctx, fan, profile.Handle, tier.ID, ""); !errors.Is(err, ErrAlreadySubscribed) {
		t.Fatalf("double subscribe: want ErrAlreadySubscribed, got %v", err)
	}
	if _, err := h.svc.Subscribe(ctx, owner, profile.Handle, tier.ID, ""); !errors.Is(err, ErrSelfSubscription) {
		t.Fatalf("self subscribe: want ErrSelfSubscription, got %v", err)
	}

	// Period end expires the subscription and revokes the entitlement.
	h.now = h.now.AddDate(0, 0, 31)
	expired, err := h.svc.ExpireDue(ctx)
	if err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if expired != 1 {
		t.Fatalf("want 1 expired subscription, got %d", expired)
	}
	after, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil {
		t.Fatalf("access after expiry: %v", err)
	}
	if after.Allowed {
		t.Fatalf("expired subscription still grants access: %+v", after)
	}
}

func TestPublishingAndOwnership(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, _ := h.creator(t)

	ownerAccess, err := h.svc.CheckAccess(ctx, &owner, profile.Handle, ScopeSubscriber)
	if err != nil || !ownerAccess.Allowed || ownerAccess.Reason != "owner" {
		t.Fatalf("owner denied own profile: %+v err=%v", ownerAccess, err)
	}

	admin := h.account(t)
	admin.Roles = append(admin.Roles, identity.RoleAdmin)
	adminAccess, err := h.svc.CheckAccess(ctx, &admin, profile.Handle, ScopeSubscriber)
	if err != nil || !adminAccess.Allowed || adminAccess.Reason != "admin" {
		t.Fatalf("admin denied: %+v err=%v", adminAccess, err)
	}

	// Public scope needs no session at all.
	public, err := h.svc.CheckAccess(ctx, nil, profile.Handle, ScopePublic)
	if err != nil || !public.Allowed || public.Reason != "public" {
		t.Fatalf("public scope denied to anonymous: %+v err=%v", public, err)
	}
}

func TestDirectGrantScopeAndExpiry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, _ := h.creator(t)
	fan := h.account(t)

	if err := h.svc.GrantEntitlement(ctx, owner, profile.Handle, fan.UserID, ScopePPV, 24*time.Hour, ""); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ppv, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopePPV)
	if err != nil || !ppv.Allowed {
		t.Fatalf("ppv grant not honoured: %+v err=%v", ppv, err)
	}
	// A ppv grant must not unlock subscriber scope.
	sub, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil {
		t.Fatalf("subscriber check: %v", err)
	}
	if sub.Allowed {
		t.Fatalf("ppv grant granted subscriber access: %+v", sub)
	}

	h.now = h.now.Add(25 * time.Hour)
	expired, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopePPV)
	if err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if expired.Allowed {
		t.Fatalf("expired grant still allowed: %+v", expired)
	}
}

func TestCancelKeepsAccessUntilPeriodEnd(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, profile, tier := h.creator(t)
	fan := h.account(t)

	sub, err := h.svc.Subscribe(ctx, fan, profile.Handle, tier.ID, "")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	cancelled, err := h.svc.CancelSubscription(ctx, fan, sub.ID, "")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !cancelled.CancelAtPeriodEnd || cancelled.Status != StatusActive {
		t.Fatalf("cancellation is not period-end: %+v", cancelled)
	}
	still, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil || !still.Allowed {
		t.Fatalf("access lost immediately on cancel: %+v err=%v", still, err)
	}

	// Another fan cannot cancel it.
	other := h.account(t)
	if _, err := h.svc.CancelSubscription(ctx, other, sub.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign cancel: want ErrNotFound, got %v", err)
	}

	h.now = h.now.AddDate(0, 0, 31)
	if _, err := h.svc.ExpireDue(ctx); err != nil {
		t.Fatalf("expire: %v", err)
	}
	after, err := h.svc.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil || after.Allowed {
		t.Fatalf("access survived the period: %+v err=%v", after, err)
	}

	// An expired subscription cannot be cancelled twice.
	if _, err := h.svc.CancelSubscription(ctx, fan, sub.ID, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cancel expired: want ErrInvalidInput, got %v", err)
	}
}

func TestPaidScopeRequiresAgeVerification(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, profile, tier := h.creator(t)
	fan := h.account(t)

	sub, err := h.svc.Subscribe(ctx, fan, profile.Handle, tier.ID, "")
	if err != nil || sub.Status != StatusActive {
		t.Fatalf("subscribe: %+v err=%v", sub, err)
	}

	// The subscription is live, but the account has not completed age
	// verification: the gate is independent of billing.
	unverified := New(h.svc.pool, WithFakePayments(true), WithAgeGate(stubGate{allow: false}),
		WithClock(func() time.Time { return h.now }))
	decision, err := unverified.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	if decision.Allowed || decision.Reason != ReasonAgeRequired {
		t.Fatalf("unverified subscriber was allowed: %+v", decision)
	}

	// Without a gate at all the answer is the same: fail closed, never open.
	ungated := New(h.svc.pool, WithFakePayments(true))
	decision, err = ungated.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	if decision.Allowed || decision.Reason != ReasonAgeRequired {
		t.Fatalf("a service with no gate allowed paid content: %+v", decision)
	}

	// A gate that errors must not become an allow.
	broken := New(h.svc.pool, WithFakePayments(true), WithAgeGate(stubGate{err: errGateDown}))
	if _, err := broken.CheckAccess(ctx, &fan, profile.Handle, ScopeSubscriber); err == nil {
		t.Fatal("a failing gate produced a decision instead of an error")
	}

	// Public scope never needs verification.
	publicDecision, err := unverified.CheckAccess(ctx, &fan, profile.Handle, ScopePublic)
	if err != nil || !publicDecision.Allowed {
		t.Fatalf("public scope denied: %+v err=%v", publicDecision, err)
	}
}

var errGateDown = errors.New("verification service unavailable")
