package identity

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/mail"
)

// Email verification.
//
// The code is short enough to type (8 base32 characters, 40 bits of entropy) so
// it has a short lifetime, a request rate limit and a hard attempt cap. Only its
// SHA-256 hash is stored; the plaintext exists in the email and nowhere else.
const (
	emailCodeTTL            = 30 * time.Minute
	emailCodeBytes          = 5
	maxVerificationAttempts = 5
)

// ErrMailUnavailable is returned when no mail sender is configured.
var ErrMailUnavailable = errors.New("identity: email delivery is not configured")

func newEmailCode() (plain string, hash []byte, err error) {
	raw := make([]byte, emailCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("identity: generate verification code: %w", err)
	}
	plain = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	return plain, hashToken(plain), nil
}

// RequestEmailVerification issues a fresh code and sends it. A new request
// invalidates any previous code for the account.
func (s *Service) RequestEmailVerification(ctx context.Context, p Principal, requestID string) (time.Time, error) {
	if s.mailer == nil {
		return time.Time{}, ErrMailUnavailable
	}
	// Read the state from the database rather than the caller's session snapshot:
	// a session opened before verification would otherwise keep requesting codes.
	verified, err := s.store.emailVerifiedState(ctx, p.UserID)
	if err != nil {
		return time.Time{}, err
	}
	if p.EmailVerified || verified {
		return time.Time{}, fmt.Errorf("%w: email is already verified", ErrInvalidInput)
	}

	email, err := s.store.emailOf(ctx, p.UserID)
	if err != nil {
		return time.Time{}, err
	}
	code, hash, err := newEmailCode()
	if err != nil {
		return time.Time{}, err
	}
	expires := s.now().UTC().Add(emailCodeTTL)
	if err := s.store.insertEmailCode(ctx, p.UserID, hash, expires); err != nil {
		return time.Time{}, err
	}

	msg := mail.Message{
		To:      email,
		Subject: "Confirm your email address",
		Text: "Your confirmation code is:\n\n    " + code + "\n\n" +
			"It expires in 30 minutes and can be used once.\n" +
			"If you did not create this account, ignore this message.\n",
	}
	if err := s.mailer.Send(ctx, msg); err != nil {
		return time.Time{}, fmt.Errorf("identity: send verification email: %w", err)
	}
	s.store.auditPool(ctx, p.UserID, "email.verification_requested", p.UserID, requestID)
	return expires, nil
}

// ConfirmEmail consumes a code and marks the address verified.
func (s *Service) ConfirmEmail(ctx context.Context, p Principal, code, requestID string) error {
	normalized := normalizeCode(code)
	if normalized == "" {
		return ErrInvalidMFACode
	}
	if err := s.store.consumeEmailCode(ctx, p.UserID, hashToken(normalized), s.now().UTC()); err != nil {
		return err
	}
	s.store.auditPool(ctx, p.UserID, "email.verified", p.UserID, requestID)
	return nil
}

// normalizeCode removes the separators and case that a user may paste in.
func normalizeCode(code string) string {
	out := make([]rune, 0, len(code))
	for _, r := range code {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r':
			continue
		case r >= 'a' && r <= 'z':
			out = append(out, r-32)
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
