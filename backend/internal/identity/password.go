// Package identity implements accounts, credentials, sessions, devices and role
// grants. It is the only package allowed to read or write the identity schema.
package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Password hashing parameters.
//
// PBKDF2-HMAC-SHA256 is used from the standard library rather than Argon2id from
// golang.org/x/crypto so the service keeps a zero-dependency build path; the
// iteration count is chosen to exceed the OWASP floor for this construction
// (600,000 for HMAC-SHA256). The algorithm and its parameters are stored next to
// each hash, so migrating to Argon2id later is a per-user rehash on next login,
// not a migration.
const (
	algoPBKDF2SHA256 = "pbkdf2-sha256"

	defaultIterations = 600_000
	testIterations    = 2_000

	saltLen = 16
	keyLen  = 32

	minPasswordLen = 12
	maxPasswordLen = 256
)

// ErrWeakPassword is returned when a password fails the policy.
var ErrWeakPassword = errors.New("identity: password does not meet the policy")

// Hasher hashes and verifies passwords with a configurable iteration count.
type Hasher struct {
	iterations int
}

// NewHasher returns a hasher with an explicit iteration count. Tests use a small
// count; production uses DefaultHasher.
func NewHasher(iterations int) *Hasher {
	if iterations < 1 {
		iterations = defaultIterations
	}
	return &Hasher{iterations: iterations}
}

// DefaultHasher returns the production hasher.
func DefaultHasher() *Hasher { return &Hasher{iterations: defaultIterations} }

// ValidatePassword enforces the password policy: length only. Composition rules
// are deliberately not applied because they push users towards predictable
// patterns; length with rate limiting is the stronger control.
func ValidatePassword(password string) error {
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return fmt.Errorf("%w: length must be between %d and %d bytes",
			ErrWeakPassword, minPasswordLen, maxPasswordLen)
	}
	return nil
}

// Hash returns a self-describing string: algo$iterations$salt$key.
func (h *Hasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: read salt: %w", err)
	}
	key := pbkdf2SHA256([]byte(password), salt, h.iterations, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s",
		algoPBKDF2SHA256, h.iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify checks a password against an encoded hash. needsRehash is true when the
// stored parameters are weaker than the current policy, so the caller can
// upgrade the hash after a successful login.
func (h *Hasher) Verify(password, encoded string) (ok bool, needsRehash bool) {
	algo, iterations, salt, key, err := parseHash(encoded)
	if err != nil || algo != algoPBKDF2SHA256 {
		return false, false
	}
	candidate := pbkdf2SHA256([]byte(password), salt, iterations, len(key))
	if subtle.ConstantTimeCompare(candidate, key) != 1 {
		return false, false
	}
	return true, iterations < h.iterations
}

// Params reports the parameters stored in an encoded hash, for the users table.
func parseHash(encoded string) (algo string, iterations int, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 {
		return "", 0, nil, nil, errors.New("identity: malformed password hash")
	}
	algo = parts[0]
	iterations, err = strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return "", 0, nil, nil, errors.New("identity: malformed password hash iteration count")
	}
	enc := base64.RawStdEncoding
	if salt, err = enc.DecodeString(parts[2]); err != nil {
		return "", 0, nil, nil, errors.New("identity: malformed password hash salt")
	}
	if key, err = enc.DecodeString(parts[3]); err != nil {
		return "", 0, nil, nil, errors.New("identity: malformed password hash key")
	}
	if len(salt) == 0 || len(key) == 0 {
		return "", 0, nil, nil, errors.New("identity: empty password hash component")
	}
	return algo, iterations, salt, key, nil
}

// pbkdf2SHA256 is PBKDF2 (RFC 8018) with HMAC-SHA256 as the PRF. It is covered by
// known-answer tests against published vectors.
func pbkdf2SHA256(password, salt []byte, iterations, length int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	blocks := (length + hashLen - 1) / hashLen

	derived := make([]byte, 0, blocks*hashLen)
	counter := make([]byte, 4)
	u := make([]byte, 0, hashLen)

	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(counter, uint32(block))
		prf.Write(counter)

		u = prf.Sum(u[:0])
		t := make([]byte, len(u))
		copy(t, u)

		for n := 2; n <= iterations; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for i := range t {
				t[i] ^= u[i]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:length]
}
