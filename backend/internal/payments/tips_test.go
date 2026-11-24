package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/earnings"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

// A tip is the one payment whose amount the client chooses. These tests cover the three
// things that can go wrong as a result: an amount outside the bounds, a recipient that is
// the sender, and a note long enough to be a message. They then prove that a settled tip
// posts a `tip` ledger transaction, grants nothing, and appears only in the simulated side
// of the earnings report.

type tipGate struct{}

func (tipGate) AllowAdult(context.Context, string) (bool, error) { return true, nil }

type tipHarness struct {
	payments *Service
	identity *identity.Service
	creators *creator.Service
	fake     *Fake
	pool     *pgxpool.Pool
	earnings *earnings.Service
}

func newTipHarness(t *testing.T) *tipHarness {
	t.Helper()
	pool := dbtest.Pool(t)
	fake, err := NewFake([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatalf("new fake: %v", err)
	}
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(tipGate{}))
	svc := New(pool, WithProvider(fake), WithCreators(creators))
	return &tipHarness{
		payments: svc,
		identity: identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams()))),
		creators: creators,
		fake:     fake,
		pool:     pool,
		earnings: earnings.New(pool),
	}
}

func (h *tipHarness) account(t *testing.T) identity.Principal {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	res, err := h.identity.Register(context.Background(),
		"tip-"+hex.EncodeToString(buf)+"@example.test", "a-very-long-password",
		identity.DeviceInfo{Fingerprint: "tip-device"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal
}

func (h *tipHarness) creatorAccount(t *testing.T) identity.Principal {
	t.Helper()
	p := h.account(t)
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	if _, err := h.creators.CreateProfile(context.Background(), p, creator.ProfileInput{
		Handle:      "tipper" + hex.EncodeToString(buf),
		DisplayName: "Mira",
		Category:    "art",
	}, ""); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return p
}

func (h *tipHarness) settle(t *testing.T, intent Intent) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"event_id":     "evt_" + intent.ID,
		"kind":         EventSucceeded,
		"intent_id":    intent.ID,
		"amount_minor": intent.AmountMinor,
		"currency":     intent.Currency,
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	header := http.Header{}
	header.Set("X-Payment-Signature", h.fake.Sign(payload, time.Now()))
	res, err := h.payments.HandleEvent(context.Background(), header, payload)
	if err != nil {
		t.Fatalf("handle event: %v", err)
	}
	if !res.Applied {
		t.Fatalf("settlement was not applied: %+v", res)
	}
}

func TestTipSettlesIntoTheLedgerWithoutGrantingAccess(t *testing.T) {
	h := newTipHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)

	intent, err := h.payments.CreateIntent(ctx, fan, CreateInput{
		Purpose:        PurposeTip,
		CreatorID:      creatorAccount.UserID,
		AmountMinor:    1200,
		Note:           "for the studio fund",
		IdempotencyKey: "tip-key-0001",
	}, "")
	if err != nil {
		t.Fatalf("create tip: %v", err)
	}
	if intent.Purpose != PurposeTip || intent.AmountMinor != 1200 || intent.Note != "for the studio fund" {
		t.Fatalf("unexpected tip intent: %+v", intent)
	}
	if !intent.Simulated {
		t.Fatal("tip from the fake provider must be marked simulated")
	}

	h.settle(t, intent)

	// A tip buys nothing: no entitlement, no unlock row.
	var entitlements int
	if err := h.pool.QueryRow(ctx, `
		SELECT count(*)::int FROM creator.entitlements
		WHERE fan_id = $1::uuid AND creator_id = $2::uuid`,
		fan.UserID, creatorAccount.UserID).Scan(&entitlements); err != nil {
		t.Fatalf("count entitlements: %v", err)
	}
	if entitlements != 0 {
		t.Fatalf("a tip granted %d entitlements", entitlements)
	}

	// The ledger carries a tip-kind transaction, stamped simulated, balanced by the
	// database's own constraint.
	var kind string
	var simulated bool
	if err := h.pool.QueryRow(ctx, `
		SELECT kind, simulated FROM ledger.transactions WHERE reference = $1`, intent.ID).
		Scan(&kind, &simulated); err != nil {
		t.Fatalf("read ledger transaction: %v", err)
	}
	if kind != "tip" || !simulated {
		t.Fatalf("ledger row = (%s, simulated=%v), want (tip, true)", kind, simulated)
	}

	// 20% platform share, so a 1200 tip leaves 960 payable.
	summary, err := h.earnings.Summary(ctx, creatorAccount.UserID, true)
	if err != nil {
		t.Fatalf("earnings: %v", err)
	}
	if summary.NetSimulatedMinor != 960 {
		t.Fatalf("simulated net = %d, want 960", summary.NetSimulatedMinor)
	}
	if summary.NetLiveMinor != 0 {
		t.Fatalf("live net = %d, want 0", summary.NetLiveMinor)
	}
	var found bool
	for _, bucket := range summary.Buckets {
		if bucket.Purpose == "tip" {
			found = true
			if !bucket.Simulated || bucket.GrossMinor != 1200 || bucket.Count != 1 {
				t.Fatalf("tip bucket wrong: %+v", bucket)
			}
		}
	}
	if !found {
		t.Fatal("the earnings report did not include the tip bucket")
	}
}

func TestEarningsCanExcludeSimulatedMoney(t *testing.T) {
	h := newTipHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)

	intent, err := h.payments.CreateIntent(ctx, fan, CreateInput{
		Purpose:        PurposeTip,
		CreatorID:      creatorAccount.UserID,
		AmountMinor:    500,
		IdempotencyKey: "tip-key-0002",
	}, "")
	if err != nil {
		t.Fatalf("create tip: %v", err)
	}
	h.settle(t, intent)

	// Default for a payout or a tax export: test money is not part of the answer.
	live, err := h.earnings.Summary(ctx, creatorAccount.UserID, false)
	if err != nil {
		t.Fatalf("live summary: %v", err)
	}
	if len(live.Buckets) != 0 {
		t.Fatalf("live summary leaked simulated buckets: %+v", live.Buckets)
	}
	if live.LiveCount != 0 || live.NetLiveMinor != 0 {
		t.Fatalf("live totals leaked simulated money: %+v", live)
	}
	// The excluded money is still reported as excluded, not silently dropped.
	if live.SimulatedCount != 1 || live.NetSimulatedMinor != 400 {
		t.Fatalf("excluded totals wrong: %+v", live)
	}
}

func TestTipBoundsAndSelfTipAreRefused(t *testing.T) {
	h := newTipHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)

	refused := func(name string, in CreateInput, want error) {
		t.Helper()
		if _, err := h.payments.CreateIntent(ctx, fan, in, ""); !errors.Is(err, want) {
			t.Fatalf("%s: want %v, got %v", name, want, err)
		}
	}

	refused("below the minimum", CreateInput{
		Purpose: PurposeTip, CreatorID: creatorAccount.UserID,
		AmountMinor: 99, IdempotencyKey: "tip-low-0001",
	}, ErrInvalidInput)
	refused("above the maximum", CreateInput{
		Purpose: PurposeTip, CreatorID: creatorAccount.UserID,
		AmountMinor: MaxTipMinor + 1, IdempotencyKey: "tip-high-0001",
	}, ErrInvalidInput)
	refused("note too long", CreateInput{
		Purpose: PurposeTip, CreatorID: creatorAccount.UserID, AmountMinor: 500,
		Note: strings.Repeat("x", MaxTipNote+1), IdempotencyKey: "tip-note-0001",
	}, ErrInvalidInput)
	refused("no recipient", CreateInput{
		Purpose: PurposeTip, AmountMinor: 500, IdempotencyKey: "tip-none-0001",
	}, ErrInvalidInput)

	if _, err := h.payments.CreateIntent(ctx, creatorAccount, CreateInput{
		Purpose:        PurposeTip,
		CreatorID:      creatorAccount.UserID,
		AmountMinor:    500,
		IdempotencyKey: "tip-self-0001",
	}, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self tip: want ErrForbidden, got %v", err)
	}
}

// A tip may name its amount; nothing else may. A subscription whose request carries an
// amount has to be refused, not quietly priced from the record.
func TestOnlyTipsMayCarryAnAmount(t *testing.T) {
	h := newTipHarness(t)
	ctx := context.Background()
	fan := h.account(t)

	if _, err := h.payments.CreateIntent(ctx, fan, CreateInput{
		Purpose:        PurposeSubscription,
		AmountMinor:    1,
		IdempotencyKey: "sub-amount-0001",
	}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("subscription with an amount: want ErrInvalidInput, got %v", err)
	}
	if _, err := h.payments.CreateIntent(ctx, fan, CreateInput{
		Purpose:        PurposeSubscription,
		Note:           "priced by the client",
		IdempotencyKey: "sub-note-0001",
	}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("subscription with a note: want ErrInvalidInput, got %v", err)
	}
}

// The same idempotency key must not create a second charge, which for a tip is the
// difference between one thank-you and two.
func TestTipIdempotencyKeyReturnsTheSameIntent(t *testing.T) {
	h := newTipHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)
	in := CreateInput{
		Purpose:        PurposeTip,
		CreatorID:      creatorAccount.UserID,
		AmountMinor:    700,
		IdempotencyKey: "tip-idem-0001",
	}
	first, err := h.payments.CreateIntent(ctx, fan, in, "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.payments.CreateIntent(ctx, fan, in, "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("a repeated key created two tips: %s then %s", first.ID, second.ID)
	}
}
