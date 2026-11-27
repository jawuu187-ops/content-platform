package agereview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/agegate"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

type fixture struct {
	pool   *pgxpool.Pool
	svc    *Service
	gate   *agegate.Service
	now    time.Time
	owner  string
	target string
}

func newFixture(t *testing.T, validity time.Duration) *fixture {
	t.Helper()
	pool := dbtest.Pool(t)
	f := &fixture{pool: pool, now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return f.now }
	f.svc = New(pool, validity, WithClock(clock))
	f.gate = agegate.New(pool, agegate.WithClock(clock))
	owner := makeUser(t, pool, "owner-review@example.test")
	target := makeUser(t, pool, "target-review@example.test")
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO identity.role_grants (user_id, role, granted_by) VALUES ($1::uuid, 'owner', 'test')`,
		owner); err != nil {
		t.Fatalf("grant owner: %v", err)
	}
	f.owner, f.target = owner, target
	return f
}

func makeUser(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO identity.users (email, password_hash, email_verified_at)
		VALUES ($1, 'x', now()) RETURNING id::text`, email).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func ownerActor(id string, mfaAt *time.Time) Actor {
	return Actor{UserID: id, SessionID: "00000000-0000-0000-0000-000000000001",
		Roles: []string{identity.RoleOwner, identity.RoleFan}, MFAAt: mfaAt}
}

func freshMFA(now time.Time) *time.Time { return &now }

func TestReviewQueueAndDecisionsRequireOwnerAndRecentMFA(t *testing.T) {
	f := newFixture(t, 24*time.Hour)
	ctx := context.Background()
	if _, err := f.svc.RequestManual(ctx, f.target, "req"); err != nil {
		t.Fatalf("request: %v", err)
	}

	fan := Actor{UserID: f.target, Roles: []string{identity.RoleFan}, MFAAt: freshMFA(f.now)}
	if _, err := f.svc.Queue(ctx, fan); !errors.Is(err, ErrForbidden) {
		t.Fatalf("fan queue: want ErrForbidden, got %v", err)
	}
	if _, err := f.svc.Queue(ctx, ownerActor(f.owner, nil)); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("no MFA: want ErrMFARequired, got %v", err)
	}
	stale := f.now.Add(-MFAStepUpWindow - time.Second)
	if _, err := f.svc.Queue(ctx, ownerActor(f.owner, &stale)); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("stale MFA: want ErrMFARequired, got %v", err)
	}
	items, err := f.svc.Queue(ctx, ownerActor(f.owner, freshMFA(f.now)))
	if err != nil {
		t.Fatalf("owner queue: %v", err)
	}
	if len(items) != 1 || items[0].UserID != f.target {
		t.Fatalf("queue = %+v, want one target request", items)
	}
}

func TestSelfApprovalAndSelfRevocationAreRejected(t *testing.T) {
	f := newFixture(t, 24*time.Hour)
	ctx := context.Background()
	owner := ownerActor(f.owner, freshMFA(f.now))
	req, err := f.svc.RequestManual(ctx, f.owner, "req")
	if err != nil {
		t.Fatalf("self request: %v", err)
	}
	if _, err := f.svc.Approve(ctx, owner, req.ID, "req"); !errors.Is(err, ErrSelfReview) {
		t.Fatalf("self approve: want ErrSelfReview, got %v", err)
	}
	if _, err := f.svc.Reject(ctx, owner, req.ID, "age_not_established", "req"); !errors.Is(err, ErrSelfReview) {
		t.Fatalf("self reject: want ErrSelfReview, got %v", err)
	}
	if _, err := f.svc.Revoke(ctx, owner, f.owner, "suspected_fraud", "req"); !errors.Is(err, ErrSelfReview) {
		t.Fatalf("self revoke: want ErrSelfReview, got %v", err)
	}
	if got := countAudits(t, f.pool, "age_review.approved", req.ID); got != 0 {
		t.Fatalf("self approval wrote %d success audits", got)
	}
}

func TestApprovalGrantsAccessAndIsAudited(t *testing.T) {
	f := newFixture(t, 24*time.Hour)
	ctx := context.Background()
	req, err := f.svc.RequestManual(ctx, f.target, "req-1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	approved, err := f.svc.Approve(ctx, ownerActor(f.owner, freshMFA(f.now)), req.ID, "req-2")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != "approved" || approved.CheckID == "" || approved.ExpiresAt == nil {
		t.Fatalf("approved request = %+v", approved)
	}
	allowed, err := f.gate.AllowAdult(ctx, f.target)
	if err != nil || !allowed {
		t.Fatalf("access after approval = %v, %v", allowed, err)
	}
	var method string
	if err := f.pool.QueryRow(ctx,
		`SELECT method FROM verify.checks WHERE id = $1::uuid`, approved.CheckID).Scan(&method); err != nil {
		t.Fatal(err)
	}
	if method != "manual_review" {
		t.Fatalf("method = %s, want manual_review", method)
	}
	if got := countAudits(t, f.pool, "age_review.approved", req.ID); got != 1 {
		t.Fatalf("approval audits = %d, want 1", got)
	}
	if _, err := f.svc.RequestManual(ctx, f.target, "req-3"); !errors.Is(err, ErrAlreadyVerified) {
		t.Fatalf("second request: want ErrAlreadyVerified, got %v", err)
	}
}

func TestRejectionCreatesNoAccessAndUsesOnlyListedReasons(t *testing.T) {
	f := newFixture(t, 24*time.Hour)
	ctx := context.Background()
	req, err := f.svc.RequestManual(ctx, f.target, "req")
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerActor(f.owner, freshMFA(f.now))
	if _, err := f.svc.Reject(ctx, owner, req.ID, "free text about passport", "req"); !errors.Is(err, ErrInvalidReason) {
		t.Fatalf("free-text reason: want ErrInvalidReason, got %v", err)
	}
	rejected, err := f.svc.Reject(ctx, owner, req.ID, "age_not_established", "req")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != "rejected" || rejected.ReasonCode != "age_not_established" {
		t.Fatalf("rejected = %+v", rejected)
	}
	if allowed, _ := f.gate.AllowAdult(ctx, f.target); allowed {
		t.Fatal("rejected account was allowed adult content")
	}
	if got := countAudits(t, f.pool, "age_review.rejected", req.ID); got != 1 {
		t.Fatalf("rejection audits = %d, want 1", got)
	}
}

func TestRevocationAndExpiryRemoveAccessImmediately(t *testing.T) {
	f := newFixture(t, 24*time.Hour)
	ctx := context.Background()
	req, err := f.svc.RequestManual(ctx, f.target, "req")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Approve(ctx, ownerActor(f.owner, freshMFA(f.now)), req.ID, "req"); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := f.gate.AllowAdult(ctx, f.target); !allowed {
		t.Fatal("approved account was not allowed")
	}
	n, err := f.svc.Revoke(ctx, ownerActor(f.owner, freshMFA(f.now)), f.target, "suspected_fraud", "req")
	if err != nil || n != 1 {
		t.Fatalf("revoke = %d, %v", n, err)
	}
	if allowed, _ := f.gate.AllowAdult(ctx, f.target); allowed {
		t.Fatal("revoked account was still allowed adult content")
	}
	latest, err := f.svc.LatestForUser(ctx, f.target)
	if err != nil || latest == nil || latest.Status != "revoked" {
		t.Fatalf("latest after revoke = %+v, %v", latest, err)
	}
	if got := countAudits(t, f.pool, "age_review.revoked", f.target); got != 1 {
		t.Fatalf("revocation audits = %d, want 1", got)
	}

	// A fresh request/approval, then moving past the validity window, must also
	// lose access without any background job.
	if _, err := f.svc.RequestManual(ctx, f.target, "req"); err != nil {
		t.Fatal(err)
	}
	fresh, err := f.svc.LatestForUser(ctx, f.target)
	if err != nil || fresh == nil {
		t.Fatalf("latest pending = %+v, %v", fresh, err)
	}
	if _, err := f.svc.Approve(ctx, ownerActor(f.owner, freshMFA(f.now)), fresh.ID, "req"); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(25 * time.Hour)
	if allowed, _ := f.gate.AllowAdult(ctx, f.target); allowed {
		t.Fatal("expired account was still allowed adult content")
	}
}

func TestAuditFailureRollsBackApproval(t *testing.T) {
	f := newFixture(t, 24*time.Hour)
	ctx := context.Background()
	req, err := f.svc.RequestManual(ctx, f.target, "req")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION ops.test_fail_review_audit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.action = 'age_review.approved' THEN
				RAISE EXCEPTION 'test: audit unavailable';
			END IF;
			RETURN NEW;
		END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		CREATE TRIGGER test_fail_review_audit BEFORE INSERT ON ops.audit_log
		FOR EACH ROW EXECUTE FUNCTION ops.test_fail_review_audit()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_fail_review_audit ON ops.audit_log`)
		_, _ = f.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS ops.test_fail_review_audit()`)
	})

	if _, err := f.svc.Approve(ctx, ownerActor(f.owner, freshMFA(f.now)), req.ID, "req"); err == nil {
		t.Fatal("approval succeeded despite audit failure")
	}
	var status string
	if err := f.pool.QueryRow(ctx,
		`SELECT status FROM verify.review_requests WHERE id = $1::uuid`, req.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("request status = %s, want pending", status)
	}
	var checks int
	if err := f.pool.QueryRow(ctx,
		`SELECT count(*) FROM verify.checks WHERE user_id = $1::uuid`, f.target).Scan(&checks); err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatalf("checks = %d, want 0 after rollback", checks)
	}
	if allowed, _ := f.gate.AllowAdult(ctx, f.target); allowed {
		t.Fatal("rollback still granted access")
	}
}

func TestVerificationSchemaHasNoDocumentColumns(t *testing.T) {
	f := newFixture(t, 24*time.Hour)
	var n int
	err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'verify'
		  AND (data_type = 'bytea'
		       OR column_name ~* '(document|selfie|image|photo|scan|attachment|blob|portrait)')`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("verification schema has %d document-like columns", n)
	}
}

func countAudits(t *testing.T, pool *pgxpool.Pool, action, target string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM ops.audit_log WHERE action = $1 AND target_id = $2`,
		action, target).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
