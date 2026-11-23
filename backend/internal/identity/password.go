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

	"golang.org/x/crypto/argon2"
)

// Password hashing.
//
// Argon2id is the hashing algorithm for all new passwords. It is memory-hard, so
// an attacker with GPUs or ASICs gains far less than against an iteration-only
// construction, and it is the algorithm RFC 9106 recommends for password storage.
//
// PBKDF2-HMAC-SHA256 support is kept read-only: it verifies hashes written by the
// first release and reports that they must be re-hashed, so accounts upgrade
// transparently on their next successful login. No new PBKDF2 hash is ever
// written. See docs/security/password-hashing.md for the full comparison.
const (
	algoArgon2id     = "argon2id"
	algoPBKDF2SHA256 = "pbkdf2-sha256"

	saltLen = 16
	keyLen  = 32

	minPasswordLen = 12
	maxPasswordLen = 256
)

// ErrWeakPassword is returned when a password fails the policy.
var ErrWeakPassword = errors.New("identity: password does not meet the policy")

// Params describes how a password hash is produced. It is stored next to every
// hash inside the PHC string, so different accounts can use different costs
// during a parameter change.
type Params struct {
	Algorithm   string
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultParams are the production parameters: 64 MiB, 3 passes, 2 lanes. On a
// modern server core that is roughly 50-80 ms per hash, which is slow enough to
// make offline guessing expensive without making login feel slow.
func DefaultParams() Params {
	return Params{Algorithm: algoArgon2id, MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2}
}

// TestParams are deliberately cheap (8 MiB, 1 pass) so a test suite that hashes
// dozens of passwords still finishes quickly. They are never used in production.
func TestParams() Params {
	return Params{Algorithm: algoArgon2id, MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}
}

// Hasher hashes and verifies passwords with one parameter set.
type Hasher struct {
	params Params
}

// NewHasher returns a hasher for the given parameters.
func NewHasher(p Params) *Hasher {
	if p.Algorithm == "" {
		p = DefaultParams()
	}
	if p.MemoryKiB < 8 {
		p.MemoryKiB = 8
	}
	if p.Iterations < 1 {
		p.Iterations = 1
	}
	if p.Parallelism < 1 {
		p.Parallelism = 1
	}
	return &Hasher{params: p}
}

// DefaultHasher returns the production hasher.
func DefaultHasher() *Hasher { return &Hasher{params: DefaultParams()} }

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

// Hash returns a self-describing PHC string: argon2id$v=19$m=..,t=..,p=..$salt$key.
func (h *Hasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt,
		h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, keyLen)

	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		algoArgon2id, argon2.Version, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// ParamsForStorage renders the parameters of a hash for the users row, so an
// operator can audit the cost without parsing the hash.
func (h *Hasher) ParamsForStorage() string {
	return fmt.Sprintf(`{"algorithm":%q,"memory_kib":%d,"iterations":%d,"parallelism":%d}`,
		algoArgon2id, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism)
}

// Verify checks a password against a stored hash and reports whether the hash
// should be replaced because it is weaker than the current parameters. It
// understands both Argon2id and the legacy PBKDF2 format.
func (h *Hasher) Verify(password, encoded string) (ok bool, needsRehash bool) {
	switch {
	case strings.HasPrefix(encoded, algoArgon2id+"$"):
		return h.verifyArgon2id(password, encoded)
	case strings.HasPrefix(encoded, algoPBKDF2SHA256+"$"):
		return h.verifyLegacyPBKDF2(password, encoded)
	default:
		return false, false
	}
}

func (h *Hasher) verifyArgon2id(password, encoded string) (bool, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != algoArgon2id {
		return false, false
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v="))
	if err != nil || version != argon2.Version {
		return false, false
	}
	memory, iterations, parallelism, err := parseArgon2Params(parts[2])
	if err != nil {
		return false, false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[3])
	if err != nil || len(salt) < 8 {
		return false, false
	}
	want, err := enc.DecodeString(parts[4])
	if err != nil || len(want) < 16 || len(want) > 1024 {
		return false, false
	}

	// #nosec G115 -- the length is bounded to 1024 bytes above.
	keyLen := uint32(len(want))
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLen)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	weaker := memory < h.params.MemoryKiB ||
		iterations < h.params.Iterations ||
		parallelism < h.params.Parallelism
	return true, weaker
}

func parseArgon2Params(s string) (memory, iterations uint32, parallelism uint8, err error) {
	for _, kv := range strings.Split(s, ",") {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			return 0, 0, 0, errors.New("identity: malformed argon2 parameters")
		}
		n, convErr := strconv.ParseUint(v, 10, 32)
		if convErr != nil {
			return 0, 0, 0, errors.New("identity: malformed argon2 parameters")
		}
		switch k {
		case "m":
			memory = uint32(n)
		case "t":
			iterations = uint32(n)
		case "p":
			if n > 255 {
				return 0, 0, 0, errors.New("identity: malformed argon2 parameters")
			}
			parallelism = uint8(n)
		default:
			return 0, 0, 0, errors.New("identity: unknown argon2 parameter")
		}
	}
	if memory == 0 || iterations == 0 || parallelism == 0 {
		return 0, 0, 0, errors.New("identity: incomplete argon2 parameters")
	}
	return memory, iterations, parallelism, nil
}

// verifyLegacyPBKDF2 validates hashes written before the move to Argon2id and
// always asks for a rehash, so the account upgrades on the next login.
func (h *Hasher) verifyLegacyPBKDF2(password, encoded string) (bool, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != algoPBKDF2SHA256 {
		return false, false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false, false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false, false
	}
	want, err := enc.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false, false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(want))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, true
}

// pbkdf2SHA256 is PBKDF2 (RFC 8018) with HMAC-SHA256. It exists only to verify
// legacy hashes; new passwords never use it.
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
