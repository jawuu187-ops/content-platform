package identity

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Multi-factor authentication: TOTP enrolment, verification and recovery codes.
const (
	recoveryCodeCount = 10
	recoveryCodeBytes = 10 // 80 bits per code
)

// ErrMFAUnavailable is returned when the deployment has no encryption key, so a
// TOTP secret could not be stored safely.
var ErrMFAUnavailable = errors.New("identity: multi-factor authentication is not configured on this instance")

// Factor is the client-visible view of a registered second factor.
type Factor struct {
	Kind       string     `json:"kind"`
	Label      string     `json:"label,omitempty"`
	Confirmed  bool       `json:"confirmed"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// StartTOTP begins enrolment: it stores a pending factor and returns the secret
// and the otpauth URI. The factor does nothing until ConfirmTOTP succeeds, so a
// half-finished enrolment can never lock a user out.
func (s *Service) StartTOTP(ctx context.Context, p Principal, label, requestID string) (secret, uri string, err error) {
	if s.box == nil {
		return "", "", ErrMFAUnavailable
	}
	secret, err = GenerateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	sealed, err := s.box.seal([]byte(secret))
	if err != nil {
		return "", "", err
	}
	if label == "" {
		label = "Authenticator app"
	}
	if _, err := s.store.insertTOTPFactor(ctx, p.UserID, truncateString(label, 64), sealed); err != nil {
		return "", "", err
	}
	account, err := s.store.emailOf(ctx, p.UserID)
	if err != nil {
		return "", "", err
	}
	s.store.auditPool(ctx, p.UserID, "mfa.totp_started", p.UserID, requestID)
	return secret, TOTPURI(s.issuer, account, secret), nil
}

// ConfirmTOTP verifies the first code and returns the recovery codes. They are
// returned exactly once and stored only as hashes.
func (s *Service) ConfirmTOTP(ctx context.Context, p Principal, code, requestID string) ([]string, error) {
	if s.box == nil {
		return nil, ErrMFAUnavailable
	}
	factorID, sealed, err := s.store.pendingTOTP(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	secret, err := s.box.open(sealed)
	if err != nil {
		return nil, err
	}
	if !VerifyTOTP(string(secret), code, s.now()) {
		return nil, ErrInvalidMFACode
	}
	if err := s.store.confirmFactor(ctx, factorID); err != nil {
		return nil, err
	}

	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.store.deleteRecoveryCodes(ctx, p.UserID); err != nil {
		return nil, err
	}
	if err := s.store.insertRecoveryCodes(ctx, p.UserID, hashes); err != nil {
		return nil, err
	}
	s.store.auditPool(ctx, p.UserID, "mfa.totp_confirmed", factorID, requestID)
	return codes, nil
}

// DisableTOTP turns MFA off. It requires a valid current code, so a stolen session
// alone cannot remove the second factor.
func (s *Service) DisableTOTP(ctx context.Context, p Principal, code, requestID string) error {
	if s.box == nil {
		return ErrMFAUnavailable
	}
	factorID, sealed, err := s.store.confirmedTOTP(ctx, p.UserID)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	secret, err := s.box.open(sealed)
	if err != nil {
		return err
	}
	if !VerifyTOTP(string(secret), code, s.now()) {
		return ErrInvalidMFACode
	}
	if err := s.store.deleteFactors(ctx, p.UserID); err != nil {
		return err
	}
	if err := s.store.deleteRecoveryCodes(ctx, p.UserID); err != nil {
		return err
	}
	s.store.auditPool(ctx, p.UserID, "mfa.totp_disabled", factorID, requestID)
	return nil
}

// Factors lists the registered factors for the caller.
func (s *Service) Factors(ctx context.Context, p Principal) ([]Factor, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT kind, COALESCE(label, ''), confirmed_at IS NOT NULL, created_at, last_used_at
		FROM identity.mfa_factors
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC`, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("identity: list factors: %w", err)
	}
	defer rows.Close()

	out := make([]Factor, 0, 2)
	for rows.Next() {
		var f Factor
		if err := rows.Scan(&f.Kind, &f.Label, &f.Confirmed, &f.CreatedAt, &f.LastUsedAt); err != nil {
			return nil, fmt.Errorf("identity: scan factor: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// checkMFACode accepts either a TOTP code or a single-use recovery code.
func (s *Service) checkMFACode(ctx context.Context, userID, factorID string, sealed []byte, code string) (bool, error) {
	if s.box == nil {
		return false, ErrMFAUnavailable
	}
	secret, err := s.box.open(sealed)
	if err != nil {
		return false, err
	}
	if VerifyTOTP(string(secret), code, s.now()) {
		s.store.touchFactor(ctx, factorID)
		return true, nil
	}

	normalized := normalizeCode(code)
	if normalized == "" {
		return false, nil
	}
	used, err := s.store.useRecoveryCode(ctx, userID, hashToken(normalized))
	if err != nil {
		return false, err
	}
	if !used {
		return false, nil
	}
	remaining, err := s.store.countUnusedRecoveryCodes(ctx, userID)
	if err == nil && remaining <= 2 {
		// Surface the low-recovery-code state in the audit trail so support can
		// prompt the user to regenerate.
		s.store.auditPool(ctx, userID, "mfa.recovery_codes_low", userID, "")
	}
	return true, nil
}

// newRecoveryCodes returns the displayable codes and their hashes.
func newRecoveryCodes() (codes []string, hashes [][]byte, err error) {
	codes = make([]string, 0, recoveryCodeCount)
	hashes = make([][]byte, 0, recoveryCodeCount)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)

	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("identity: generate recovery code: %w", err)
		}
		compact := enc.EncodeToString(raw) // 16 characters
		codes = append(codes, groupCode(compact))
		hashes = append(hashes, hashToken(compact))
	}
	return codes, hashes, nil
}

// groupCode formats XXXXXXXXXXXXXXXX as XXXX-XXXX-XXXX-XXXX for transcription.
func groupCode(compact string) string {
	var b strings.Builder
	for i, r := range compact {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
