package creatorverify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func TestVerificationLifecycleAndMFAGate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	svc := New(pool)
	var userID, ownerID string
	if err := pool.QueryRow(ctx, `INSERT INTO identity.users (email, password_hash) VALUES ('creator-kyc@example.test','x') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO identity.users (email, password_hash) VALUES ('owner-kyc@example.test','x') RETURNING id::text`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO creator.profiles (user_id, handle, display_name, category)
		VALUES ($1::uuid, 'kyc_studio', 'KYC Studio', 'art')`, userID); err != nil {
		t.Fatal(err)
	}
	creator := identity.Principal{UserID: userID}
	if _, err := svc.Submit(ctx, creator, SubmitInput{
		LegalName: "Test Creator", CountryCode: "US", DateOfBirth: "1990-01-01", Attestation: true,
	}, "req"); err != nil {
		t.Fatal(err)
	}
	latest, err := svc.Latest(ctx, userID)
	if err != nil || latest == nil || latest.Status != "pending" {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	var kyc string
	if err := pool.QueryRow(ctx, `SELECT kyc_status FROM creator.profiles WHERE user_id=$1::uuid`, userID).Scan(&kyc); err != nil || kyc != "pending" {
		t.Fatalf("profile status=%q err=%v", kyc, err)
	}

	owner := identity.Principal{UserID: ownerID, Roles: []string{"owner"}}
	if _, err := svc.Decide(ctx, owner, latest.ID, true, "", "no-mfa"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("without MFA = %v, want ErrForbidden", err)
	}
	now := time.Now()
	owner.MFAAt = &now
	approved, err := svc.Decide(ctx, owner, latest.ID, true, "", "mfa")
	if err != nil || approved.Status != "approved" {
		t.Fatalf("approve=%+v err=%v", approved, err)
	}
	if err := pool.QueryRow(ctx, `SELECT kyc_status FROM creator.profiles WHERE user_id=$1::uuid`, userID).Scan(&kyc); err != nil || kyc != "verified" {
		t.Fatalf("verified status=%q err=%v", kyc, err)
	}
	if _, err := svc.Submit(ctx, creator, SubmitInput{
		LegalName: "Test Creator", CountryCode: "US", DateOfBirth: "1990-01-01", Attestation: true,
	}, "again"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("verified reapply = %v, want ErrInvalid", err)
	}
}

func TestUnderageCreatorIsRefused(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	svc := New(pool)
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO identity.users (email, password_hash) VALUES ('underage-kyc@example.test','x') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO creator.profiles (user_id, handle, display_name, category)
		VALUES ($1::uuid, 'young_studio', 'Young', 'art')`, userID); err != nil {
		t.Fatal(err)
	}
	dob := time.Now().AddDate(-17, 0, 0).Format("2006-01-02")
	if _, err := svc.Submit(ctx, identity.Principal{UserID: userID}, SubmitInput{
		LegalName: "Young Creator", CountryCode: "US", DateOfBirth: dob, Attestation: true,
	}, "req"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("underage = %v, want ErrInvalid", err)
	}
}
