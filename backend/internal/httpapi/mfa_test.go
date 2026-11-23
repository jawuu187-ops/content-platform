package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/mail"
)

// httpTestMailer captures verification mail so the test can read the code.
type httpTestMailer struct {
	mu   sync.Mutex
	body string
}

func (m *httpTestMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.body = msg.Text
	return nil
}

func (m *httpTestMailer) code() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := regexp.MustCompile(`[A-Z2-7]{8}`).FindString(m.body)
	return found
}

func loggerDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

// These tests exercise the whole HTTP surface of P1: email verification, TOTP
// enrolment, MFA-gated login and the permission-gated admin endpoints. Allowed
// and denied paths are both asserted, because an authorization test that only
// checks the happy path proves nothing.

func authServerWith(t *testing.T, opts ...identity.Option) http.Handler {
	t.Helper()
	h, _ := authServerWithPool(t, opts...)
	return h
}

// authServerWithPool returns the handler and its pool. Callers that need to seed
// rows must use this pool: dbtest.Pool truncates the identity tables on every
// call, so asking for it again mid-test would wipe the data under test.
func authServerWithPool(t *testing.T, opts ...identity.Option) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	base := []identity.Option{
		identity.WithHasher(identity.NewHasher(identity.TestParams())),
		identity.WithIssuer("Content Platform Test"),
	}
	svc := identity.New(pool, append(base, opts...)...)
	return New(Deps{
		Config:   testConfig(),
		Logger:   loggerDiscard(),
		Identity: svc,
	}), pool
}

func encodeBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return string(b)
}

func registerAndGetToken(t *testing.T, h http.Handler, email string) authBody {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/v1/auth/register", "", registerBody(email, "a-very-long-password"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %s: want 201, got %d (%s)", email, rec.Code, rec.Body.String())
	}
	return decodeAuth(t, rec)
}

func TestEmailVerificationOverHTTP(t *testing.T) {
	mailer := &httpTestMailer{}
	h := authServerWith(t, identity.WithMailer(mailer))
	account := registerAndGetToken(t, h, "verify-http@example.test")
	token := account.Tokens.AccessToken

	rec := doJSON(t, h, http.MethodPost, "/v1/auth/email/verify/request", token, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request verification: want 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	code := mailer.code()
	if code == "" {
		t.Fatal("no verification code was delivered")
	}

	// A wrong code is rejected with a specific, non-leaking error.
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/email/verify/confirm", token,
		encodeBody(t, map[string]string{"code": "ZZZZZZZZ"}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: want 401, got %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "invalid_mfa_code" {
		t.Fatalf("wrong code problem: %q", p.Code)
	}

	rec = doJSON(t, h, http.MethodPost, "/v1/auth/email/verify/confirm", token,
		encodeBody(t, map[string]string{"code": code}))
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// /v1/auth/me reflects the new state on the same session.
	rec = doJSON(t, h, http.MethodGet, "/v1/auth/me", token, "")
	var me struct {
		EmailVerified bool `json:"email_verified"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if !me.EmailVerified {
		t.Fatal("/v1/auth/me still reports the address as unverified")
	}
}

func TestTOTPEnrolmentAndMFAProtectedLogin(t *testing.T) {
	mailer := &httpTestMailer{}
	h := authServerWith(t, identity.WithMailer(mailer), identity.WithSecretKey(testKey()))
	const email = "totp-http@example.test"
	const password = "a-very-long-password"

	account := registerAndGetToken(t, h, email)
	token := account.Tokens.AccessToken

	rec := doJSON(t, h, http.MethodPost, "/v1/auth/mfa/totp/start", token,
		encodeBody(t, map[string]string{"label": "Pixel"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("totp start: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var start struct {
		Secret string `json:"secret"`
		URI    string `json:"otpauth_uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if start.Secret == "" || !strings.HasPrefix(start.URI, "otpauth://totp/") {
		t.Fatalf("totp start payload is incomplete: %s", rec.Body.String())
	}

	code, err := identity.TOTPCodeAt(start.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/mfa/totp/confirm", token,
		encodeBody(t, map[string]string{"code": code}))
	if rec.Code != http.StatusOK {
		t.Fatalf("totp confirm: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var confirm struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &confirm); err != nil {
		t.Fatalf("decode confirm: %v", err)
	}
	if len(confirm.RecoveryCodes) != 10 {
		t.Fatalf("want 10 recovery codes, got %d", len(confirm.RecoveryCodes))
	}

	// Login without the code now fails with mfa_required, not invalid_credentials:
	// the password was correct and the client needs to know that.
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/login", "", registerBody(email, password))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login without mfa: want 401, got %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "mfa_required" {
		t.Fatalf("login without mfa code: want mfa_required, got %q", p.Code)
	}

	// Login with a recovery code succeeds and the code cannot be reused.
	body := encodeBody(t, map[string]any{
		"email": email, "password": password, "mfa_code": confirm.RecoveryCodes[0],
		"device": map[string]string{"fingerprint": "d1", "platform": "android"},
	})
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/login", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with recovery code: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeAuth(t, rec); got.Tokens.AccessToken == "" {
		t.Fatal("login with MFA returned no access token")
	}
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/login", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code: want 401, got %d", rec.Code)
	}
}

func TestPermissionMatrixAcrossProtectedEndpoints(t *testing.T) {
	h, pool := authServerWithPool(t)

	// A fan (the default role) must be denied on every admin endpoint.
	fan := registerAndGetToken(t, h, "fan-perms@example.test")
	creator := registerAndGetToken(t, h, "creator-perms@example.test") // still a fan until granted a role

	denied := []struct {
		method, path, body string
	}{
		{http.MethodPost, "/v1/admin/roles/grant", encodeBody(t, map[string]string{"user_id": creator.User.ID, "role": identity.RoleCreator})},
		{http.MethodGet, "/v1/admin/users/" + creator.User.ID + "/roles", ""},
	}
	for _, tc := range denied {
		rec := doJSON(t, h, tc.method, tc.path, fan.Tokens.AccessToken, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s as fan: want 403, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if p := decodeProblem(t, rec); p.Code != "insufficient_permissions" {
			t.Fatalf("%s %s as fan: want insufficient_permissions, got %q", tc.method, tc.path, p.Code)
		}
	}

	// Confirm nothing was granted by the denied attempts.
	roles, err := identity.New(pool).RolesOf(t.Context(), creator.User.ID)
	if err != nil {
		t.Fatalf("roles: %v", err)
	}
	for _, r := range roles {
		if r == identity.RoleCreator {
			t.Fatal("a denied request still granted a role")
		}
	}

	// Grant the acting account the admin role directly in the database (there is
	// no bootstrap endpoint, by design) and retry the same calls.
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO identity.role_grants (user_id, role, granted_by) VALUES ($1::uuid, 'admin', 'test')`,
		fan.User.ID); err != nil {
		t.Fatalf("seed admin role: %v", err)
	}

	// The session must pick up the new role: roles are read per request, not
	// frozen into the token.
	rec := doJSON(t, h, http.MethodPost, "/v1/admin/roles/grant", fan.Tokens.AccessToken,
		encodeBody(t, map[string]string{"user_id": creator.User.ID, "role": identity.RoleCreator}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("grant as admin: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var granted struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &granted); err != nil {
		t.Fatalf("decode grant: %v", err)
	}
	if len(granted.Roles) != 2 {
		t.Fatalf("want fan+creator after the grant, got %v", granted.Roles)
	}

	// An admin may read roles and permissions of another account.
	rec = doJSON(t, h, http.MethodGet, "/v1/admin/users/"+creator.User.ID+"/roles", fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("read roles as admin: want 200, got %d", rec.Code)
	}
	var view struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode roles view: %v", err)
	}
	if !containsString(view.Permissions, string(identity.PermContentPublish)) {
		t.Fatalf("creator permissions are missing content.publish: %v", view.Permissions)
	}
	if containsString(view.Permissions, string(identity.PermAdminAll)) {
		t.Fatalf("creator must not hold admin.all: %v", view.Permissions)
	}

	// The creator still has no access to role grants.
	rec = doJSON(t, h, http.MethodPost, "/v1/admin/roles/grant", creator.Tokens.AccessToken,
		encodeBody(t, map[string]string{"user_id": creator.User.ID, "role": identity.RoleAdmin}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("creator granting roles: want 403, got %d", rec.Code)
	}
}

func TestMFAEndpointsRequireAuthentication(t *testing.T) {
	h := authServerWith(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/auth/mfa"},
		{http.MethodPost, "/v1/auth/mfa/totp/start"},
		{http.MethodPost, "/v1/auth/email/verify/request"},
		{http.MethodPost, "/v1/admin/roles/grant"},
	} {
		rec := doJSON(t, h, tc.method, tc.path, "", "{}")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a token: want 401, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(100 + i)
	}
	return key
}
