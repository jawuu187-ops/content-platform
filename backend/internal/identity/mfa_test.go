package identity

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/mail"
)

// captureMailer records what would have been sent so a test can read the code.
type captureMailer struct {
	mu       sync.Mutex
	messages []mail.Message
	failWith error
}

func (c *captureMailer) Send(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failWith != nil {
		return c.failWith
	}
	c.messages = append(c.messages, m)
	return nil
}

func (c *captureMailer) last(t *testing.T) mail.Message {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) == 0 {
		t.Fatal("no email was sent")
	}
	return c.messages[len(c.messages)-1]
}

func newTestService(t *testing.T, opts ...Option) (*Service, *pgxpool.Pool, *captureMailer) {
	t.Helper()
	pool := dbtest.Pool(t)
	mailer := &captureMailer{}
	base := []Option{
		WithHasher(NewHasher(TestParams())),
		WithMailer(mailer),
		WithSecretKey(testSecretKey()),
		WithIssuer("Content Platform Test"),
	}
	svc := New(pool, append(base, opts...)...)
	return svc, pool, mailer
}

func testSecretKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

var codePattern = regexp.MustCompile(`[A-Z2-7]{8}`)

func TestEmailVerificationIsSingleUseAndExpires(t *testing.T) {
	svc, _, mailer := newTestService(t)
	ctx := context.Background()

	res, err := svc.Register(ctx, uniqueEmail("verify"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	p := res.Principal

	if _, err := svc.RequestEmailVerification(ctx, p, "req-1"); err != nil {
		t.Fatalf("request verification: %v", err)
	}
	msg := mailer.last(t)
	if !strings.Contains(msg.Text, "confirmation code") {
		t.Fatalf("verification email body is unexpected: %q", msg.Text)
	}
	code := codePattern.FindString(msg.Text)
	if code == "" {
		t.Fatalf("no code found in the email body: %q", msg.Text)
	}

	if err := svc.ConfirmEmail(ctx, p, strings.ToLower(code), "req-2"); err != nil {
		t.Fatalf("confirm with lower-case code: %v", err)
	}

	// The same code cannot be replayed.
	if err := svc.ConfirmEmail(ctx, p, code, "req-3"); !errors.Is(err, ErrInvalidMFACode) {
		t.Fatalf("replayed code: want ErrInvalidMFACode, got %v", err)
	}

	// Once verified, a new request is refused rather than sending another email.
	verified := p
	verified.EmailVerified = true
	if _, err := svc.RequestEmailVerification(ctx, verified, "req-4"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("request on verified account: want ErrInvalidInput, got %v", err)
	}
}

func TestEmailVerificationRejectsWrongCodeAndReportsIt(t *testing.T) {
	svc, _, mailer := newTestService(t)
	ctx := context.Background()
	res, err := svc.Register(ctx, uniqueEmail("wrongcode"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.RequestEmailVerification(ctx, res.Principal, ""); err != nil {
		t.Fatalf("request verification: %v", err)
	}
	code := codePattern.FindString(mailer.last(t).Text)
	wrong := "AAAAAAA" + string(nextRune(code[len(code)-1]))
	if err := svc.ConfirmEmail(ctx, res.Principal, wrong, ""); !errors.Is(err, ErrInvalidMFACode) {
		t.Fatalf("wrong code: want ErrInvalidMFACode, got %v", err)
	}
	// The correct code still works after one failed attempt.
	if err := svc.ConfirmEmail(ctx, res.Principal, code, ""); err != nil {
		t.Fatalf("correct code after a failure: %v", err)
	}
}

func nextRune(r byte) byte {
	if r == '7' {
		return '2'
	}
	return r + 1
}

func TestExpiredEmailCodeIsRejected(t *testing.T) {
	svc, _, mailer := newTestService(t)
	ctx := context.Background()
	base := time.Now().UTC()
	svc.now = func() time.Time { return base }

	res, err := svc.Register(ctx, uniqueEmail("expiredcode"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.RequestEmailVerification(ctx, res.Principal, ""); err != nil {
		t.Fatalf("request verification: %v", err)
	}
	code := codePattern.FindString(mailer.last(t).Text)

	svc.now = func() time.Time { return base.Add(emailCodeTTL + time.Minute) }
	if err := svc.ConfirmEmail(ctx, res.Principal, code, ""); !errors.Is(err, ErrInvalidMFACode) {
		t.Fatalf("expired code: want ErrInvalidMFACode, got %v", err)
	}
}

func TestTOTPEnrolmentThenLoginRequiresCode(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	email := uniqueEmail("totp")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	p := res.Principal

	secret, uri, err := svc.StartTOTP(ctx, p, "Test phone", "")
	if err != nil {
		t.Fatalf("start totp: %v", err)
	}
	if !strings.HasPrefix(uri, "otpauth://totp/") || !strings.Contains(uri, secret) {
		t.Fatalf("otpauth URI is wrong: %q", uri)
	}
	// A pending factor does not affect login yet.
	if _, err := svc.Login(ctx, email, goodPassword, "", DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("login before confirming the factor: %v", err)
	}

	code, err := TOTPCodeAt(secret, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	codes, err := svc.ConfirmTOTP(ctx, p, code, "")
	if err != nil {
		t.Fatalf("confirm totp: %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("want %d recovery codes, got %d", recoveryCodeCount, len(codes))
	}
	for _, c := range codes {
		if len(c) != 19 || strings.Count(c, "-") != 3 {
			t.Fatalf("recovery code %q is not formatted XXXX-XXXX-XXXX-XXXX", c)
		}
	}

	// Now login without a code is refused, and the password alone is not enough.
	if _, err := svc.Login(ctx, email, goodPassword, "", DeviceInfo{Fingerprint: "d1"}, ""); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("login without code: want ErrMFARequired, got %v", err)
	}
	if _, err := svc.Login(ctx, email, goodPassword, "000000", DeviceInfo{Fingerprint: "d1"}, ""); !errors.Is(err, ErrInvalidMFACode) {
		t.Fatalf("login with wrong code: want ErrInvalidMFACode, got %v", err)
	}

	fresh, err := TOTPCodeAt(secret, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	if _, err := svc.Login(ctx, email, goodPassword, fresh, DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("login with a valid code: %v", err)
	}

	// A recovery code works exactly once.
	if _, err := svc.Login(ctx, email, goodPassword, codes[0], DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("login with recovery code: %v", err)
	}
	if _, err := svc.Login(ctx, email, goodPassword, codes[0], DeviceInfo{Fingerprint: "d1"}, ""); !errors.Is(err, ErrInvalidMFACode) {
		t.Fatalf("reused recovery code: want ErrInvalidMFACode, got %v", err)
	}
}

func TestTOTPConfirmRejectsWrongCodeAndKeepsFactorPending(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	res, err := svc.Register(ctx, uniqueEmail("totpbad"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	secret, _, err := svc.StartTOTP(ctx, res.Principal, "", "")
	if err != nil {
		t.Fatalf("start totp: %v", err)
	}
	if _, err := svc.ConfirmTOTP(ctx, res.Principal, "123456", ""); !errors.Is(err, ErrInvalidMFACode) {
		t.Fatalf("wrong confirm code: want ErrInvalidMFACode, got %v", err)
	}
	// Still usable: the correct code confirms.
	code, _ := TOTPCodeAt(secret, time.Now())
	if _, err := svc.ConfirmTOTP(ctx, res.Principal, code, ""); err != nil {
		t.Fatalf("confirm after a failed attempt: %v", err)
	}
}

func TestDisableMFARequiresACurrentCode(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	email := uniqueEmail("disable")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	secret, _, err := svc.StartTOTP(ctx, res.Principal, "", "")
	if err != nil {
		t.Fatalf("start totp: %v", err)
	}
	code, _ := TOTPCodeAt(secret, time.Now())
	if _, err := svc.ConfirmTOTP(ctx, res.Principal, code, ""); err != nil {
		t.Fatalf("confirm totp: %v", err)
	}

	if err := svc.DisableTOTP(ctx, res.Principal, "999999", ""); !errors.Is(err, ErrInvalidMFACode) {
		t.Fatalf("disable with wrong code: want ErrInvalidMFACode, got %v", err)
	}
	fresh, _ := TOTPCodeAt(secret, time.Now())
	if err := svc.DisableTOTP(ctx, res.Principal, fresh, ""); err != nil {
		t.Fatalf("disable with valid code: %v", err)
	}
	if _, err := svc.Login(ctx, email, goodPassword, "", DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("login after disabling MFA: %v", err)
	}
}

func TestMFAUnavailableWithoutKey(t *testing.T) {
	pool := dbtest.Pool(t)
	svc := New(pool, WithHasher(NewHasher(TestParams())))
	ctx := context.Background()
	res, err := svc.Register(ctx, uniqueEmail("nokey"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, _, err := svc.StartTOTP(ctx, res.Principal, "", ""); !errors.Is(err, ErrMFAUnavailable) {
		t.Fatalf("start totp without a key: want ErrMFAUnavailable, got %v", err)
	}
}

func TestEmailVerificationRequiresAMailer(t *testing.T) {
	pool := dbtest.Pool(t)
	svc := New(pool, WithHasher(NewHasher(TestParams())))
	ctx := context.Background()
	res, err := svc.Register(ctx, uniqueEmail("nomailer"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.RequestEmailVerification(ctx, res.Principal, ""); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("want ErrMailUnavailable, got %v", err)
	}
}

func TestGrantRoleValidatesAndAudits(t *testing.T) {
	svc, pool, _ := newTestService(t)
	ctx := context.Background()

	adminActor := Principal{UserID: "00000000-0000-4000-8000-000000000001", Roles: []string{RoleAdmin}}
	target, err := svc.Register(ctx, uniqueEmail("grantee"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register target: %v", err)
	}
	// The acting principal must exist for the audit entry to reference it.
	if _, err := pool.Exec(ctx, `INSERT INTO identity.users (id, email, password_hash)
		VALUES ($1::uuid, 'admin@example.test', 'x') ON CONFLICT DO NOTHING`, adminActor.UserID); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	if _, err := svc.GrantRole(ctx, adminActor, target.User.ID, "wizard", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown role: want ErrInvalidInput, got %v", err)
	}
	if _, err := svc.GrantRole(ctx, adminActor, "00000000-0000-4000-8000-0000000000ff", RoleCreator, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user: want ErrNotFound, got %v", err)
	}

	roles, err := svc.GrantRole(ctx, adminActor, target.User.ID, RoleCreator, "req-grant")
	if err != nil {
		t.Fatalf("grant role: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("want fan+creator, got %v", roles)
	}

	// Granting twice is idempotent, not an error.
	if _, err := svc.GrantRole(ctx, adminActor, target.User.ID, RoleCreator, "req-grant-2"); err != nil {
		t.Fatalf("repeat grant: %v", err)
	}

	var entries int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ops.audit_log WHERE action = 'role.granted'`).Scan(&entries); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	if entries < 2 {
		t.Fatalf("want 2 role.granted audit entries, got %d", entries)
	}
}

func TestStepUpMFABindsTheProofToTheSession(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	res, err := svc.Register(ctx, uniqueEmail("stepup"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	secret, _, err := svc.StartTOTP(ctx, res.Principal, "", "")
	if err != nil {
		t.Fatalf("start totp: %v", err)
	}
	code, err := TOTPCodeAt(secret, time.Now())
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	if _, err := svc.ConfirmTOTP(ctx, res.Principal, code, ""); err != nil {
		t.Fatalf("confirm totp: %v", err)
	}

	if at, err := svc.StepUpMFA(ctx, res.Principal, "000000", ""); !errors.Is(err, ErrInvalidMFACode) || !at.IsZero() {
		t.Fatalf("wrong code: want ErrInvalidMFACode and zero time, got %v / %v", err, at)
	}
	at, err := svc.StepUpMFA(ctx, res.Principal, code, "")
	if err != nil {
		t.Fatalf("step up: %v", err)
	}
	if at.IsZero() {
		t.Fatal("step-up returned zero time")
	}
	principal, err := svc.Authenticate(ctx, res.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if principal.MFAAt == nil {
		t.Fatal("session has no mfa_at after step-up")
	}
}
