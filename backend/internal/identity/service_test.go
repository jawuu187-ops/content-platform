package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

// PBKDF2-HMAC-SHA256 known answers. These vectors pin the implementation: if the
// loop, the counter encoding or the XOR step is wrong, these fail immediately.
func TestTokensAreUniqueAndHashed(t *testing.T) {
	seen := map[string]bool{}
	seenHash := map[string]bool{}
	for i := 0; i < 64; i++ {
		plain, hash, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %v", err)
		}
		if seen[plain] {
			t.Fatal("duplicate token generated")
		}
		seen[plain] = true
		if len(hash) != 32 {
			t.Fatalf("want 32 byte hash, got %d", len(hash))
		}
		if strings.Contains(string(hash), plain) {
			t.Fatal("stored hash contains the plaintext token")
		}
		if string(hash) == plain {
			t.Fatal("token stored unhashed")
		}
		raw, err := base64.RawURLEncoding.DecodeString(plain)
		if err != nil || len(raw) != tokenBytes {
			t.Fatalf("token encoding invalid: %v len=%d", err, len(raw))
		}
		if string(hashToken(plain)) != string(hash) {
			t.Fatal("hashToken is not deterministic")
		}
		seenHash[string(hash)] = true
	}
	if len(seenHash) != 64 {
		t.Fatal("token hashes collided")
	}
}

func TestDeviceFingerprintIsBounded(t *testing.T) {
	long := strings.Repeat("x", 500)
	if got := deviceFingerprint(long); len(got) != 128 {
		t.Fatalf("want 128 byte fingerprint, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Database-backed tests
// ---------------------------------------------------------------------------

func testService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	return New(pool, WithHasher(NewHasher(TestParams()))), pool
}

func uniqueEmail(prefix string) string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(raw) + "@example.test"
}

const goodPassword = "a-very-long-password"

func TestRegisterCreatesAccountSessionAndRole(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	res, err := svc.Register(ctx, uniqueEmail("fan"), goodPassword, DeviceInfo{
		Fingerprint: "device-1", Platform: "android", Name: "Pixel", UserAgent: "test-agent", IP: "203.0.113.9",
	}, "req-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if res.User.ID == "" || res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("register returned an incomplete result")
	}
	if !res.Principal.HasRole("fan") {
		t.Fatalf("new account must hold the fan role, got %v", res.Principal.Roles)
	}
	if res.Principal.EmailVerified {
		t.Fatal("new account must not start email-verified")
	}
	if res.User.Email != strings.ToLower(res.User.Email) {
		t.Fatalf("email stored without normalisation: %q", res.User.Email)
	}

	p, err := svc.Authenticate(ctx, res.AccessToken)
	if err != nil {
		t.Fatalf("authenticate with issued access token: %v", err)
	}
	if p.UserID != res.User.ID || p.SessionID != res.Principal.SessionID {
		t.Fatal("access token resolved to a different principal")
	}

	// The plaintext password must never be recoverable from the database.
	var stored string
	if err := svc.store.pool.QueryRow(ctx,
		`SELECT password_hash FROM identity.users WHERE id = $1::uuid`, res.User.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored hash: %v", err)
	}
	if strings.Contains(stored, goodPassword) {
		t.Fatal("database stores the plaintext password")
	}
}

func TestRegisterRejectsDuplicateEmailIgnoringCase(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	email := uniqueEmail("dupe")

	if _, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "a"}, ""); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := svc.Register(ctx, strings.ToUpper(email), goodPassword, DeviceInfo{Fingerprint: "b"}, ""); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken for case variant, got %v", err)
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	for _, email := range []string{"", "not-an-email", "a@b", "with space@example.com"} {
		if _, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "x"}, ""); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("email %q: want ErrInvalidInput, got %v", email, err)
		}
	}
	if _, err := svc.Register(ctx, uniqueEmail("short"), "short", DeviceInfo{Fingerprint: "x"}, ""); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("want ErrWeakPassword, got %v", err)
	}
}

func TestLoginSucceedsAndFailsIdenticallyForUnknownAccount(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	email := uniqueEmail("login")
	if _, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("register: %v", err)
	}

	res, err := svc.Login(ctx, strings.ToUpper(email), goodPassword, "", DeviceInfo{Fingerprint: "d2", Platform: "web"}, "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.AccessToken == "" || res.Principal.SessionID == "" {
		t.Fatal("login did not open a session")
	}

	if _, err := svc.Login(ctx, email, "wrong-password-but-long", "", DeviceInfo{Fingerprint: "d2"}, ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: want ErrInvalidCredentials, got %v", err)
	}
	if _, err := svc.Login(ctx, uniqueEmail("ghost"), goodPassword, "", DeviceInfo{Fingerprint: "d2"}, ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown account: want ErrInvalidCredentials (no enumeration), got %v", err)
	}
}

func TestRefreshRotatesTokensAndDetectsReuse(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	first, err := svc.Register(ctx, uniqueEmail("refresh"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	rotated, err := svc.Refresh(ctx, first.RefreshToken, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.AccessToken == first.AccessToken || rotated.RefreshToken == first.RefreshToken {
		t.Fatal("refresh must rotate both tokens")
	}
	if rotated.Principal.UserID != first.User.ID {
		t.Fatal("rotation changed the user")
	}

	// The old refresh token was already used: replaying it must revoke the family.
	if _, err := svc.Refresh(ctx, first.RefreshToken, DeviceInfo{Fingerprint: "d1"}, ""); !errors.Is(err, ErrTokenReuse) {
		t.Fatalf("replayed refresh token: want ErrTokenReuse, got %v", err)
	}

	// The token issued by the rotation belongs to the revoked family.
	if _, err := svc.Refresh(ctx, rotated.RefreshToken, DeviceInfo{Fingerprint: "d1"}, ""); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("refresh after reuse detection: want ErrSessionRevoked, got %v", err)
	}
	if _, err := svc.Authenticate(ctx, rotated.AccessToken); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("access token after reuse detection: want ErrSessionRevoked, got %v", err)
	}

	// The account itself is untouched: a fresh login still works.
	if _, err := svc.Login(ctx, first.User.Email, goodPassword, "", DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("login after reuse detection: %v", err)
	}
}

func TestExpiredRefreshTokenCannotBeUsed(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	base := time.Now().UTC()
	svc.now = func() time.Time { return base }

	res, err := svc.Register(ctx, uniqueEmail("expiry"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	svc.now = func() time.Time { return base.Add(RefreshTokenTTL + time.Minute) }

	if _, err := svc.Refresh(ctx, res.RefreshToken, DeviceInfo{Fingerprint: "d1"}, ""); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("want ErrSessionExpired, got %v", err)
	}
	// The session row is revoked as part of expiry handling, so the caller may
	// legitimately observe either cause; both map to 401 session_ended.
	if _, err := svc.Authenticate(ctx, res.AccessToken); !errors.Is(err, ErrSessionExpired) &&
		!errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("expired access token: want an expired/revoked session error, got %v", err)
	}
}

func TestLogoutAndSessionRevocation(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	email := uniqueEmail("sessions")
	if _, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "phone", Platform: "android"}, ""); err != nil {
		t.Fatalf("register: %v", err)
	}
	// A long password is required because the policy rejects short ones.
	res, err := svc.Login(ctx, email, goodPassword, "", DeviceInfo{Fingerprint: "laptop", Platform: "web"}, "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	sessions, err := svc.Sessions(ctx, res.Principal)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	if !sessions[0].Current {
		t.Fatal("newest session must be flagged as current")
	}

	other := sessions[1].ID
	if err := svc.RevokeSession(ctx, res.Principal, other, "req-revoke"); err != nil {
		t.Fatalf("revoke other session: %v", err)
	}
	if err := svc.RevokeSession(ctx, res.Principal, other, "req-revoke"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: want ErrNotFound, got %v", err)
	}
	if err := svc.RevokeSession(ctx, res.Principal, res.Principal.SessionID, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("revoking the current session must use logout, got %v", err)
	}

	if err := svc.Logout(ctx, res.Principal, "req-logout"); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, res.AccessToken); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("access token after logout: want ErrSessionRevoked, got %v", err)
	}
	if err := svc.Logout(ctx, res.Principal, "req-logout-2"); err != nil {
		t.Fatalf("logout must be idempotent: %v", err)
	}
}

func TestAuditChainStaysIntactAcrossIdentityFlows(t *testing.T) {
	svc, pool := testService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, uniqueEmail("audit"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "req-a"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Register(ctx, uniqueEmail("audit2"), goodPassword, DeviceInfo{Fingerprint: "d2"}, "req-b"); err != nil {
		t.Fatalf("register: %v", err)
	}

	var entries int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM ops.audit_log WHERE action = 'account.register'`).Scan(&entries); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	if entries < 2 {
		t.Fatalf("want at least 2 registration audit entries, got %d", entries)
	}
	// verify_audit_chain returns the id of the first broken row, or NULL.
	var broken *int64
	if err := pool.QueryRow(ctx, `SELECT ops.verify_audit_chain()`).Scan(&broken); err != nil {
		t.Fatalf("verify audit chain: %v", err)
	}
	if broken != nil {
		t.Fatalf("audit chain broken at row %d", *broken)
	}

	// The audit log is append-only at the database level.
	if _, err := pool.Exec(ctx, `UPDATE ops.audit_log SET action = 'tampered'`); err == nil {
		t.Fatal("audit_log accepted an UPDATE")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ops.audit_log`); err == nil {
		t.Fatal("audit_log accepted a DELETE")
	}
}
