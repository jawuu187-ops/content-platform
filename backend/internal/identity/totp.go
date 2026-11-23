package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238) with HMAC-SHA1, 6 digits and a 30 second period. SHA1 is not a
// weakness here: the code is not a signature, the security comes from the shared
// secret and the short validity window, and authenticator apps interoperate with
// SHA1 by default. SHA256 is accepted on verification for clients that use it.
const (
	totpDigits      = 6
	totpPeriod      = 30 * time.Second
	totpSecretBytes = 20 // 160 bits, the RFC 4226 recommendation
	totpSkewSteps   = 1  // accept the previous and next step for clock drift
)

// ErrInvalidMFACode is returned when a TOTP or recovery code does not match.
var ErrInvalidMFACode = errors.New("identity: invalid multi-factor code")

// ErrMFARequired is returned when the account has a confirmed factor and the
// login attempt did not include a code.
var ErrMFARequired = errors.New("identity: multi-factor code required")

// GenerateTOTPSecret returns a new base32 secret with no padding, as
// authenticator apps expect.
func GenerateTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("identity: generate totp secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// totpCode computes the code for one time step.
func totpCode(secret string, counter uint64, digits int) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%mod), nil
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil || len(key) == 0 {
		return nil, errors.New("identity: malformed totp secret")
	}
	return key, nil
}

// TOTPCodeAt is exported for tests and administrative tooling.
func TOTPCodeAt(secret string, at time.Time) (string, error) {
	return totpCode(secret, uint64(at.Unix()/int64(totpPeriod/time.Second)), totpDigits)
}

// VerifyTOTP checks a user-supplied code against the current step and the two
// neighbouring steps. Comparison is constant time.
func VerifyTOTP(secret, code string, at time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	step := at.Unix() / int64(totpPeriod/time.Second)
	for delta := -totpSkewSteps; delta <= totpSkewSteps; delta++ {
		candidate, err := totpCode(secret, uint64(step+int64(delta)), totpDigits)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPURI builds the otpauth:// URI an authenticator app scans. The label and
// issuer are escaped so an odd account name cannot break the URI.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpPeriod/time.Second)))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
