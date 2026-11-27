package privacy

import (
	"context"
	"strings"
	"testing"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func TestExportConsentAndScheduledDeletion(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	svc := New(pool, ids)
	reg, err := ids.Register(ctx, "privacy-export@example.test", "a-very-long-password",
		identity.DeviceInfo{Fingerprint: "p1"}, "reg")
	if err != nil {
		t.Fatal(err)
	}
	userID := reg.User.ID
	if _, err := pool.Exec(ctx, `INSERT INTO creator.profiles (user_id, handle, display_name, category, is_published)
		VALUES ($1::uuid, 'privacy_studio', 'Privacy Studio', 'art', true)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveConsent(ctx, userID, "analytics", true); err != nil {
		t.Fatal(err)
	}
	consents, err := svc.Consents(ctx, userID)
	if err != nil || len(consents) != 1 || consents[0].RevokedAt != nil {
		t.Fatalf("consents = %+v err=%v", consents, err)
	}
	export, err := svc.Export(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if export["account"] == nil || export["creator_profile"] == nil {
		t.Fatalf("export missing sections: %+v", export)
	}

	if _, err := svc.RequestDeletion(ctx, reg.Principal, "wrong-password", "req"); !strings.Contains(err.Error(), "invalid credentials") {
		t.Fatalf("wrong password = %v", err)
	}
	req, err := svc.RequestDeletion(ctx, reg.Principal, "a-very-long-password", "req")
	if err != nil || req.Status != "scheduled" {
		t.Fatalf("request = %+v err=%v", req, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE privacy.deletion_requests SET scheduled_for = now() - interval '1 second' WHERE id=$1::uuid`, req.ID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.ExecuteDue(ctx)
	if err != nil || n != 1 {
		t.Fatalf("execute due = %d err=%v", n, err)
	}
	var status, email string
	if err := pool.QueryRow(ctx, `SELECT status, email::text FROM identity.users WHERE id=$1::uuid`, userID).Scan(&status, &email); err != nil {
		t.Fatal(err)
	}
	if status != "deleted" || !strings.Contains(email, "deleted+") {
		t.Fatalf("deleted account = %s %s", status, email)
	}
	if _, err := ids.Login(ctx, "privacy-export@example.test", "a-very-long-password", "",
		identity.DeviceInfo{Fingerprint: "p1"}, "login"); err == nil {
		t.Fatal("deleted account still logs in")
	}
	var published bool
	if err := pool.QueryRow(ctx, `SELECT is_published FROM creator.profiles WHERE user_id=$1::uuid`, userID).Scan(&published); err != nil || published {
		t.Fatalf("profile still published: %v %v", published, err)
	}
}
