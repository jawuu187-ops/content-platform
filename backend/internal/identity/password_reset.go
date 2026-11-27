package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/mail"
)

const (
	passwordResetTTL   = 30 * time.Minute
	passwordResetBytes = 32
)

// RequestPasswordReset sends a single-use reset token when the address belongs
// to an active account. It deliberately returns nil for unknown addresses: the
// HTTP layer answers 202 either way, and timing is dominated by the token insert
// only for real accounts. The token is stored as a SHA-256 hash.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail, requestID string) error {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		// Invalid input is not an existence oracle if the caller sees the same
		// accepted response.
		return nil
	}
	userID, found, err := s.store.resetTarget(ctx, email)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	raw := make([]byte, passwordResetBytes)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("identity: generate reset token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := s.now().UTC().Add(passwordResetTTL)
	if err := s.store.replacePasswordReset(ctx, userID, hashToken(token), expires); err != nil {
		return err
	}
	if s.mailer != nil {
		msg := mail.Message{
			To:      email,
			Subject: "Reset your password",
			Kind:    "password_reset",
			Text: "Use this reset code in the app:\n\n    " + token + "\n\n" +
				"It expires in 30 minutes and can be used once. Resetting your password\n" +
				"signs out every device.\n\nIf you did not request this, ignore this message.\n",
		}
		if err := s.mailer.Send(ctx, msg); err != nil {
			return fmt.Errorf("identity: send reset email: %w", err)
		}
	}
	s.store.auditPool(ctx, userID, "password.reset_requested", userID, requestID)
	return nil
}

// ConfirmPasswordReset consumes a token, replaces the hash and revokes every
// session for the account in one transaction.
func (s *Service) ConfirmPasswordReset(ctx context.Context, token, newPassword, requestID string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 512 {
		return ErrInvalidResetToken
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	_, email, err := s.store.consumePasswordReset(ctx, hashToken(token), hash,
		s.hasher.ParamsForStorage(), s.now().UTC(), requestID)
	if err != nil {
		return err
	}
	if s.mailer != nil {
		_ = s.mailer.Send(ctx, mail.Message{
			To:      email,
			Subject: "Your password was changed",
			Kind:    "security",
			Text: "Your password was changed and every device was signed out.\n\n" +
				"If this was not you, request another reset immediately.\n",
		})
	}
	return nil
}
