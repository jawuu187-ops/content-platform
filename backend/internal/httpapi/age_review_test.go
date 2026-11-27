package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/agereview"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func TestAgeReviewRoutesRequireOwnerMFAAndRejectSelfApproval(t *testing.T) {
	pool := dbtest.Pool(t)
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	review := agereview.New(pool, 24*time.Hour)
	handler := New(Deps{Config: testConfig(), Logger: loggerDiscard(), Identity: ids, AgeReview: review})
	ctx := context.Background()

	owner := registerAndGetToken(t, handler, "review-owner@example.test")
	fan := registerAndGetToken(t, handler, "review-fan@example.test")
	if _, err := pool.Exec(ctx, `
		UPDATE identity.users SET email_verified_at = now() WHERE id = $1::uuid`, owner.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO identity.role_grants (user_id, role, granted_by)
		VALUES ($1::uuid, 'owner', 'test')`, owner.User.ID); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, handler, http.MethodGet, "/v1/owner/verify/reviews", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("fan queue: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodGet, "/v1/owner/verify/reviews", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusForbidden || decodeProblem(t, rec).Code != "mfa_required" {
		t.Fatalf("owner queue without MFA: want 403 mfa_required, got %d (%s)", rec.Code, rec.Body.String())
	}

	if _, err := pool.Exec(ctx, `
		UPDATE identity.sessions SET mfa_at = now() WHERE user_id = $1::uuid`, owner.User.ID); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, handler, http.MethodGet, "/v1/owner/verify/reviews", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner queue with MFA: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The owner may request a review for themselves, but may not approve it.
	rec = doJSON(t, handler, http.MethodPost, "/v1/verify/age/review", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("self request: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	selfBody := decode[struct {
		Review struct {
			ID string `json:"id"`
		} `json:"review"`
	}](t, rec)
	rec = doJSON(t, handler, http.MethodPost,
		"/v1/owner/verify/reviews/"+selfBody.Review.ID+"/approve", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusForbidden || decodeProblem(t, rec).Code != "self_review_forbidden" {
		t.Fatalf("self approve: want 403 self_review_forbidden, got %d (%s)", rec.Code, rec.Body.String())
	}

	// A different account's request can be approved by the owner.
	rec = doJSON(t, handler, http.MethodPost, "/v1/verify/age/review", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("fan request: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	fanBody := decode[struct {
		Review struct {
			ID string `json:"id"`
		} `json:"review"`
	}](t, rec)
	rec = doJSON(t, handler, http.MethodPost,
		"/v1/owner/verify/reviews/"+fanBody.Review.ID+"/approve", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}
