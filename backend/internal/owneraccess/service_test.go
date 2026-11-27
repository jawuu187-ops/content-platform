package owneraccess

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const testSecret = "owner-activation-secret-0123456789abcdef"

func resetSlots(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Tests need an empty slot table between cases. The runtime trigger rightly
	// forbids this; the fixture turns it off explicitly, then restores it.
	if _, err := pool.Exec(ctx, `ALTER TABLE ops.owner_activation_slots DISABLE TRIGGER USER`); err != nil {
		t.Fatalf("disable slot trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE ops.owner_activation_slots`); err != nil {
		t.Fatalf("truncate slots: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ops.owner_activation_slots (slot) VALUES (1), (2)`); err != nil {
		t.Fatalf("seed slots: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE ops.owner_activation_slots ENABLE TRIGGER USER`); err != nil {
		t.Fatalf("enable slot trigger: %v", err)
	}
}

func makeUser(t *testing.T, pool *pgxpool.Pool, email string, verified bool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO identity.users (email, password_hash, email_verified_at)
		VALUES ($1, 'x', CASE WHEN $2 THEN now() ELSE NULL END)
		RETURNING id::text`, email, verified).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func principal(id string, mfaAt *time.Time) identity.Principal {
	return identity.Principal{UserID: id, SessionID: "00000000-0000-0000-0000-000000000001",
		EmailVerified: true, MFAAt: mfaAt}
}

func freshMFA() *time.Time {
	now := time.Now().UTC()
	return &now
}

func TestActivateSuccessThenRefusesSecondAttempt(t *testing.T) {
	pool := dbtest.Pool(t)
	resetSlots(t, pool)
	userID := makeUser(t, pool, "owner@example.test", true)
	svc := New(pool, testSecret, userID, "")

	if err := svc.Activate(context.Background(), principal(userID, freshMFA()), testSecret, "req-1"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	var owner string
	if err := pool.QueryRow(context.Background(),
		`SELECT user_id::text FROM identity.role_grants WHERE role = 'owner'`).Scan(&owner); err != nil {
		t.Fatalf("owner grant missing: %v", err)
	}
	if owner != userID {
		t.Fatalf("owner = %s, want %s", owner, userID)
	}
	var consumed *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM ops.owner_activation_slots WHERE slot = 1`).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed == nil {
		t.Fatal("slot 1 not consumed")
	}

	if err := svc.Activate(context.Background(), principal(userID, freshMFA()), testSecret, "req-2"); !errors.Is(err, ErrAlreadyActivated) {
		t.Fatalf("second activation error = %v, want ErrAlreadyActivated", err)
	}
}

func TestActivateRequiresRecentMFA(t *testing.T) {
	pool := dbtest.Pool(t)
	resetSlots(t, pool)
	userID := makeUser(t, pool, "nomfa@example.test", true)
	svc := New(pool, testSecret, userID, "")

	if err := svc.Activate(context.Background(), principal(userID, nil), testSecret, "req"); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("error = %v, want ErrMFARequired", err)
	}
	old := time.Now().Add(-10 * time.Minute)
	if err := svc.Activate(context.Background(), principal(userID, &old), testSecret, "req"); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("stale error = %v, want ErrMFARequired", err)
	}
	if got := ownerCount(t, pool); got != 0 {
		t.Fatalf("owners = %d, want 0", got)
	}
}

func TestActivateRefusesWrongUserAndSecret(t *testing.T) {
	pool := dbtest.Pool(t)
	resetSlots(t, pool)
	designated := makeUser(t, pool, "designated@example.test", true)
	other := makeUser(t, pool, "other@example.test", true)
	svc := New(pool, testSecret, designated, "")

	if err := svc.Activate(context.Background(), principal(other, freshMFA()), testSecret, "req"); !errors.Is(err, ErrDenied) {
		t.Fatalf("other user error = %v, want ErrDenied", err)
	}
	if err := svc.Activate(context.Background(), principal(designated, freshMFA()), strings.Repeat("z", 40), "req"); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong secret error = %v, want ErrDenied", err)
	}
	if got := ownerCount(t, pool); got != 0 {
		t.Fatalf("owners = %d, want 0", got)
	}
	var denials int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ops.audit_log WHERE action = 'owner.activation_denied'
		   AND target_id IN ($1, $2)`, designated, other).Scan(&denials); err != nil {
		t.Fatal(err)
	}
	if denials != 2 {
		t.Fatalf("denial audits = %d, want 2", denials)
	}
}

func TestExistingOwnerIsNotSilentlyOverwritten(t *testing.T) {
	pool := dbtest.Pool(t)
	resetSlots(t, pool)
	existing := makeUser(t, pool, "existing@example.test", true)
	candidate := makeUser(t, pool, "candidate@example.test", true)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO identity.role_grants (user_id, role, granted_by) VALUES ($1::uuid, 'owner', 'test')`, existing); err != nil {
		t.Fatal(err)
	}
	svc := New(pool, testSecret, candidate, "")

	if err := svc.Activate(context.Background(), principal(candidate, freshMFA()), testSecret, "req"); !errors.Is(err, ErrOwnerExists) {
		t.Fatalf("error = %v, want ErrOwnerExists", err)
	}
	if got := ownerCount(t, pool); got != 1 {
		t.Fatalf("owners = %d, want 1", got)
	}
	var consumed *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM ops.owner_activation_slots WHERE slot = 1`).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != nil {
		t.Fatal("slot consumed despite existing owner")
	}
	var activated int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ops.audit_log WHERE action = 'owner.activated' AND target_id = $1`, candidate).Scan(&activated); err != nil {
		t.Fatal(err)
	}
	if activated != 0 {
		t.Fatalf("success audits = %d, want 0", activated)
	}
}

func TestCrossSlotRaceCreatesOneOwner(t *testing.T) {
	pool := dbtest.Pool(t)
	resetSlots(t, pool)
	first := makeUser(t, pool, "slot1@example.test", true)
	second := makeUser(t, pool, "slot2@example.test", true)
	// Slot 2 is not enabled in deployment; this proves the database stays safe if
	// a future configuration ever enables it.
	svc1 := New(pool, testSecret, first, "", WithSlot(1))
	svc2 := New(pool, testSecret, second, "", WithSlot(2))

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, svc := range []*Service{svc1, svc2} {
		i, svc := i, svc
		uid := first
		if i == 1 {
			uid = second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = svc.Activate(context.Background(), principal(uid, freshMFA()), testSecret, "race")
		}()
	}
	close(start)
	wg.Wait()

	if got := ownerCount(t, pool); got != 1 {
		t.Fatalf("owners = %d, want exactly 1 (errs: %v, %v)", got, errs[0], errs[1])
	}
	var activated int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ops.audit_log WHERE action = 'owner.activated' AND request_id = 'race'`).Scan(&activated); err != nil {
		t.Fatal(err)
	}
	if activated != 1 {
		t.Fatalf("success audits = %d, want 1 (errs: %v, %v)", activated, errs[0], errs[1])
	}
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful activations = %d, want 1 (errs: %v, %v)", successes, errs[0], errs[1])
	}
}

func TestAuditFailureRollsBackSlotAndGrant(t *testing.T) {
	pool := dbtest.Pool(t)
	resetSlots(t, pool)
	userID := makeUser(t, pool, "rollback@example.test", true)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION ops.test_fail_owner_audit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.action = 'owner.activated' THEN
				RAISE EXCEPTION 'test: audit unavailable';
			END IF;
			RETURN NEW;
		END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TRIGGER test_fail_owner_audit BEFORE INSERT ON ops.audit_log
		FOR EACH ROW EXECUTE FUNCTION ops.test_fail_owner_audit()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_fail_owner_audit ON ops.audit_log`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS ops.test_fail_owner_audit()`)
	})

	svc := New(pool, testSecret, userID, "")
	if err := svc.Activate(ctx, principal(userID, freshMFA()), testSecret, "req"); err == nil {
		t.Fatal("activation succeeded despite audit failure")
	}
	if got := ownerCount(t, pool); got != 0 {
		t.Fatalf("owners = %d, want 0", got)
	}
	var consumed *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT consumed_at FROM ops.owner_activation_slots WHERE slot = 1`).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != nil {
		t.Fatal("slot stayed consumed after audit failure")
	}

	if _, err := pool.Exec(ctx, `DROP TRIGGER test_fail_owner_audit ON ops.audit_log`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Activate(ctx, principal(userID, freshMFA()), testSecret, "req"); err != nil {
		t.Fatalf("activation after audit recovery: %v", err)
	}
}

func TestConsumedSlotIsOneWay(t *testing.T) {
	pool := dbtest.Pool(t)
	resetSlots(t, pool)
	userID := makeUser(t, pool, "oneway@example.test", true)
	svc := New(pool, testSecret, userID, "")
	if err := svc.Activate(context.Background(), principal(userID, freshMFA()), testSecret, "req"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`UPDATE ops.owner_activation_slots SET consumed_at = NULL WHERE slot = 1`); err == nil {
		t.Fatal("resetting a consumed slot succeeded, want trigger rejection")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ops.owner_activation_slots WHERE slot = 1`); err == nil {
		t.Fatal("deleting a slot succeeded, want trigger rejection")
	}
}

func ownerCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity.role_grants WHERE role = 'owner'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
