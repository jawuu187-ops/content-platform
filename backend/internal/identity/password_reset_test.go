package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func resetTokenFrom(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 43 && !strings.ContainsAny(line, " :") {
			return line
		}
	}
	t.Fatalf("no reset token in body %q", body)
	return ""
}

func TestPasswordResetChangesPasswordAndRevokesSessions(t *testing.T) {
	svc, pool, mailer := newTestService(t)
	ctx := context.Background()
	email := uniqueEmail("reset")
	reg, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "device-a", Platform: "android"}, "reg")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RequestPasswordReset(ctx, email, "req"); err != nil {
		t.Fatal(err)
	}
	msg := mailer.last(t)
	if msg.Kind != "password_reset" {
		t.Fatalf("kind = %q", msg.Kind)
	}
	token := resetTokenFrom(t, msg.Text)

	if err := svc.ConfirmPasswordReset(ctx, token, "a-brand-new-password", "confirm"); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity.sessions WHERE user_id=$1::uuid AND revoked_at IS NULL`, reg.User.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("active sessions = %d, want 0", active)
	}
	if _, err := svc.Login(ctx, email, goodPassword, "", DeviceInfo{Fingerprint: "device-a"}, "old"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password login = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, email, "a-brand-new-password", "", DeviceInfo{Fingerprint: "device-b"}, "new"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

func TestPasswordResetTokenIsSingleUse(t *testing.T) {
	svc, _, mailer := newTestService(t)
	ctx := context.Background()
	email := uniqueEmail("reset-once")
	if _, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d", Platform: "android"}, "reg"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestPasswordReset(ctx, email, "req"); err != nil {
		t.Fatal(err)
	}
	token := resetTokenFrom(t, mailer.last(t).Text)
	if err := svc.ConfirmPasswordReset(ctx, token, "another-long-password", "one"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmPasswordReset(ctx, token, "third-long-password", "two"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("second use = %v, want ErrInvalidResetToken", err)
	}
}

func TestPasswordResetDoesNotRevealUnknownAddress(t *testing.T) {
	svc, pool, mailer := newTestService(t)
	ctx := context.Background()
	if err := svc.RequestPasswordReset(ctx, "nobody-here@example.test", "req"); err != nil {
		t.Fatalf("unknown address returned an error: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity.password_resets`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("reset rows = %d, want 0", count)
	}
	if len(mailer.messages) != 0 {
		t.Fatalf("messages = %d, want 0", len(mailer.messages))
	}
}
