package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// Token lifetimes. Access tokens are short so a leaked one is useless quickly;
// refresh tokens are long so the client does not force a login, and each use
// rotates them.
const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 30 * 24 * time.Hour

	tokenBytes = 32
)

// ErrMalformedToken is returned when a token cannot be decoded.
var ErrMalformedToken = errors.New("identity: malformed token")

// newToken returns a URL-safe random token and the SHA-256 hash that is stored.
// The plaintext is returned to the client exactly once and never persisted.
func newToken() (plain string, hash []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("identity: generate token: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, hashToken(plain), nil
}

// hashToken hashes an opaque token before storage or lookup. Tokens are 256-bit
// random values, so a fast hash is sufficient (there is nothing to brute force);
// a password hash here would only add latency to every request.
// randRead is a seam for tests that need to simulate entropy failure.
var randRead = rand.Read

func hashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

// deviceFingerprint normalises the client-supplied device identifier. It is not
// a security boundary: a client can lie about it. It exists so users can see and
// revoke their devices, and so anomalies can be flagged.
func deviceFingerprint(v string) string {
	const max = 128
	if len(v) > max {
		return v[:max]
	}
	return v
}
