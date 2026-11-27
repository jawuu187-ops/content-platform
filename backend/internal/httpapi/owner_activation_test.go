package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/owneraccess"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func TestOwnerActivationOverHTTPRequiresMFAAndConsumesOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	svc := owneraccess.New(pool, "owner-http-secret-0123456789abcdef", "", "owner-http@example.test")
	handler := New(Deps{Config: testConfig(), Logger: loggerDiscard(), Identity: ids, OwnerActivation: svc})

	// dbtest truncates identity.users CASCADE, which also clears the slot table
	// through its foreign key. Re-seed it the way the migration does.
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ALTER TABLE ops.owner_activation_slots DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ops.owner_activation_slots (slot) VALUES (1), (2) ON CONFLICT (slot) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE ops.owner_activation_slots ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}

	account := registerAndGetToken(t, handler, "owner-http@example.test")
	token := account.Tokens.AccessToken
	if _, err := pool.Exec(ctx,
		`UPDATE identity.users SET email_verified_at = now() WHERE id = $1::uuid`, account.User.ID); err != nil {
		t.Fatal(err)
	}

	// No step-up yet: the endpoint refuses, and nothing is consumed.
	rec := doJSON(t, handler, http.MethodPost, "/v1/owner/activate", token,
		encodeBody(t, map[string]string{"secret": "owner-http-secret-0123456789abcdef"}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("without MFA: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	if _, err := pool.Exec(ctx,
		`UPDATE identity.sessions SET mfa_at = now() WHERE user_id = $1::uuid`, account.User.ID); err != nil {
		t.Fatal(err)
	}

	rec = doJSON(t, handler, http.MethodPost, "/v1/owner/activate", token,
		encodeBody(t, map[string]string{"secret": "wrong-secret-0123456789abcdef"}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong secret: want 403, got %d", rec.Code)
	}

	rec = doJSON(t, handler, http.MethodPost, "/v1/owner/activate", token,
		encodeBody(t, map[string]string{"secret": "owner-http-secret-0123456789abcdef"}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("activation: want 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodPost, "/v1/owner/activate", token,
		encodeBody(t, map[string]string{"secret": "owner-http-secret-0123456789abcdef"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second activation: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
}
