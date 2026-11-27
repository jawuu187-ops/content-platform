package payments

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/messaging"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func TestPaidMessageUnlockIdempotencyAndRefundReunlock(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(tipGate{}))
	msgs := messaging.New(pool)
	fake, err := NewFake([]byte(strings.Repeat("m", 32)))
	if err != nil {
		t.Fatal(err)
	}
	pay := New(pool, WithProvider(fake), WithCreators(creators), WithMessages(msgs))
	owner := identity.Principal{}
	fan := identity.Principal{}
	for i, p := range []*identity.Principal{&owner, &fan} {
		res, err := ids.Register(ctx, []string{"paid-owner@example.test", "paid-fan@example.test"}[i],
			"a-very-long-password", identity.DeviceInfo{Fingerprint: "p"}, "")
		if err != nil {
			t.Fatal(err)
		}
		*p = res.Principal
	}
	if _, err := creators.CreateProfile(ctx, owner, creator.ProfileInput{
		Handle: "paid_studio", DisplayName: "Paid Studio", Category: "art", Publish: ptrBool(true),
	}, ""); err != nil {
		t.Fatal(err)
	}
	var convID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO messaging.conversations (creator_id, fan_id)
		VALUES ($1::uuid,$2::uuid) RETURNING id::text`, owner.UserID, fan.UserID).Scan(&convID); err != nil {
		t.Fatal(err)
	}
	price := int64(800)
	msg, err := msgs.SendPaid(ctx, owner, convID, "locked body", "", &price)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := pay.CreateIntent(ctx, fan, CreateInput{
		Purpose: PurposeMessage, MessageID: msg.ID, IdempotencyKey: "unlock-key-1",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pay.SimulateSettle(ctx, intent.ID); err != nil {
		t.Fatal(err)
	}
	unlocked, err := msgs.MessageByID(ctx, fan, msg.ID)
	if err != nil || unlocked.Locked || unlocked.Body != "locked body" {
		t.Fatalf("unlocked = %+v err=%v", unlocked, err)
	}
	// Retry with the same key cannot create a second charge or unlock.
	again, err := pay.CreateIntent(ctx, fan, CreateInput{
		Purpose: PurposeMessage, MessageID: msg.ID, IdempotencyKey: "unlock-key-1",
	}, "")
	if err != nil || again.ID != intent.ID {
		t.Fatalf("idempotent intent = %+v err=%v", again, err)
	}
	if err := pay.SimulateSettle(ctx, intent.ID); err != nil {
		t.Fatal(err)
	}
	var unlocks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messaging.message_unlocks WHERE message_id=$1::uuid`, msg.ID).Scan(&unlocks); err != nil || unlocks != 1 {
		t.Fatalf("unlocks = %d err=%v", unlocks, err)
	}

	admin := identity.Principal{UserID: owner.UserID, Roles: []string{"admin"}}
	if _, err := pay.Refund(ctx, admin, intent.ID, intent.AmountMinor, "safety refund", ""); err != nil {
		t.Fatal(err)
	}
	locked, err := msgs.MessageByID(ctx, fan, msg.ID)
	if err != nil || !locked.Locked || locked.Body != "" {
		t.Fatalf("after refund = %+v err=%v", locked, err)
	}
	second, err := pay.CreateIntent(ctx, fan, CreateInput{
		Purpose: PurposeMessage, MessageID: msg.ID, IdempotencyKey: "unlock-key-2",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pay.SimulateSettle(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	var history, live int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM messaging.message_unlocks WHERE message_id=$1::uuid`, msg.ID).Scan(&history)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM messaging.message_unlocks WHERE message_id=$1::uuid AND revoked_at IS NULL`, msg.ID).Scan(&live)
	if history != 2 || live != 1 {
		t.Fatalf("history=%d live=%d", history, live)
	}
}

func TestConcurrentPaidMessageSettlementsCannotDoubleCharge(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(tipGate{}))
	msgs := messaging.New(pool)
	fake, _ := NewFake([]byte(strings.Repeat("n", 32)))
	pay := New(pool, WithProvider(fake), WithCreators(creators), WithMessages(msgs))

	var owner, fan identity.Principal
	for i, p := range []*identity.Principal{&owner, &fan} {
		res, err := ids.Register(ctx, []string{"race-owner@example.test", "race-fan@example.test"}[i],
			"a-very-long-password", identity.DeviceInfo{Fingerprint: "r"}, "")
		if err != nil {
			t.Fatal(err)
		}
		*p = res.Principal
	}
	if _, err := creators.CreateProfile(ctx, owner, creator.ProfileInput{
		Handle: "race_studio", DisplayName: "Race Studio", Category: "art", Publish: ptrBool(true),
	}, ""); err != nil {
		t.Fatal(err)
	}
	var convID string
	_ = pool.QueryRow(ctx, `INSERT INTO messaging.conversations (creator_id, fan_id)
		VALUES ($1::uuid,$2::uuid) RETURNING id::text`, owner.UserID, fan.UserID).Scan(&convID)
	price := int64(900)
	msg, err := msgs.SendPaid(ctx, owner, convID, "race body", "", &price)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pay.CreateIntent(ctx, fan, CreateInput{Purpose: PurposeMessage, MessageID: msg.ID, IdempotencyKey: "race-key-a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pay.CreateIntent(ctx, fan, CreateInput{Purpose: PurposeMessage, MessageID: msg.ID, IdempotencyKey: "race-key-b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{a.ID, b.ID} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			errs[i] = pay.SimulateSettle(ctx, id)
		}(i, id)
	}
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful settlements = %d, errors=%v", success, errs)
	}
	var txns int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM ledger.transactions WHERE reference IN ($1,$2)`, a.ID, b.ID).Scan(&txns)
	if txns != 1 {
		t.Fatalf("ledger transactions = %d, want 1", txns)
	}
}

func ptrBool(v bool) *bool { return &v }

func TestSimulateSettleForRequiresOwner(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(tipGate{}))
	fake, _ := NewFake([]byte(strings.Repeat("o", 32)))
	pay := New(pool, WithProvider(fake), WithCreators(creators))
	var owner, fan, other identity.Principal
	for i, p := range []*identity.Principal{&owner, &fan, &other} {
		res, err := ids.Register(ctx, []string{"sim-owner@example.test", "sim-fan@example.test", "sim-other@example.test"}[i],
			"a-very-long-password", identity.DeviceInfo{Fingerprint: "s"}, "")
		if err != nil {
			t.Fatal(err)
		}
		*p = res.Principal
	}
	if _, err := creators.CreateProfile(ctx, owner, creator.ProfileInput{
		Handle: "sim_studio", DisplayName: "Sim", Category: "art", Publish: ptrBool(true),
	}, ""); err != nil {
		t.Fatal(err)
	}
	intent, err := pay.CreateIntent(ctx, fan, CreateInput{
		Purpose: PurposeTip, CreatorID: owner.UserID, AmountMinor: 500, IdempotencyKey: "sim-tip-key",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pay.SimulateSettleFor(ctx, other, intent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user simulate = %v", err)
	}
	done, err := pay.SimulateSettleFor(ctx, fan, intent.ID)
	if err != nil || done.Status != StatusSucceeded || !done.Simulated {
		t.Fatalf("owner simulate = %+v %v", done, err)
	}
}
