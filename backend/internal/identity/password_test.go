package identity

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// Argon2id known-answer test.
//
// Parameters and expected tag are the ones published with the Argon2 reference
// implementation for Argon2id v=19: password "password", salt "somesalt",
// m=65536 KiB, t=2, p=1, 32-byte tag. The expected value was independently
// reproduced with OpenSSL's ARGON2ID KDF before being committed, so a regression
// in either the library version or the call cannot pass silently.
func TestArgon2idKnownAnswer(t *testing.T) {
	const want = "09316115d5cf24ed5a15a31a3ba326e5cf32edc24702987c02b6566f61913cf7"
	got := argon2.IDKey([]byte("password"), []byte("somesalt"), 2, 65536, 1, 32)
	if hexOf(got) != want {
		t.Fatalf("argon2id known answer mismatch:\n got %s\nwant %s", hexOf(got), want)
	}
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, v := range b {
		out = append(out, digits[v>>4], digits[v&0x0f])
	}
	return string(out)
}

// PBKDF2-HMAC-SHA256 known answers. These pin the legacy verifier: if the loop,
// counter encoding or XOR step is wrong, an existing user could not log in.
func TestLegacyPBKDF2KnownAnswers(t *testing.T) {
	cases := []struct {
		name       string
		password   string
		salt       string
		iterations int
		length     int
		want       string
	}{
		{
			name: "c=1", password: "password", salt: "salt",
			iterations: 1, length: 32,
			want: "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b",
		},
		{
			name: "c=2", password: "password", salt: "salt",
			iterations: 2, length: 32,
			want: "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43",
		},
		{
			name: "multi block", password: "passwordPASSWORDpassword",
			salt: "saltSALTsaltSALTsaltSALTsaltSALTsalt", iterations: 4096, length: 40,
			want: "348c89dbcbd32b2f32d814b8116e84cf2b17347ebc1800181c4e2a1fb8dd53e1c635518c7dac47e9",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pbkdf2SHA256([]byte(tc.password), []byte(tc.salt), tc.iterations, tc.length)
			if hexOf(got) != tc.want {
				t.Fatalf("pbkdf2 mismatch:\n got %s\nwant %s", hexOf(got), tc.want)
			}
		})
	}
}

func TestHashIsArgon2idAndVerifies(t *testing.T) {
	h := NewHasher(TestParams())
	password := "correct horse battery staple"
	encoded, err := h.Hash(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("hash does not carry the expected PHC header: %q", encoded)
	}
	if strings.Contains(encoded, password) {
		t.Fatal("hash contains the plaintext password")
	}
	ok, needsRehash := h.Verify(password, encoded)
	if !ok {
		t.Fatal("valid password rejected")
	}
	if needsRehash {
		t.Fatal("a hash produced with the current parameters must not request a rehash")
	}
	if ok, _ := h.Verify("Correct horse battery staple", encoded); ok {
		t.Fatal("password comparison is not exact")
	}
	if ok, _ := h.Verify("", encoded); ok {
		t.Fatal("empty password accepted")
	}
}

func TestHashesAreSalted(t *testing.T) {
	h := NewHasher(TestParams())
	first, err := h.Hash("a-long-enough-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	second, err := h.Hash("a-long-enough-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are identical (salt is not random)")
	}
	// The salt is the fourth field of the PHC string.
	if firstSalt, secondSalt := strings.Split(first, "$")[3], strings.Split(second, "$")[3]; firstSalt == secondSalt {
		t.Fatal("salt reused across hashes")
	}
}

func TestTamperedHashIsRejected(t *testing.T) {
	h := NewHasher(TestParams())
	encoded, err := h.Hash("a-long-enough-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	parts := strings.Split(encoded, "$")

	// Flip one byte of the expected key.
	key, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	key[0] ^= 0x01
	parts[4] = base64.RawStdEncoding.EncodeToString(key)

	if ok, _ := h.Verify("a-long-enough-password", strings.Join(parts, "$")); ok {
		t.Fatal("tampered hash accepted")
	}
}

func TestVerifyFlagsWeakerParameters(t *testing.T) {
	weak := NewHasher(Params{Algorithm: algoArgon2id, MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1})
	strong := NewHasher(DefaultParams())

	encoded, err := weak.Hash("a-long-enough-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ok, needsRehash := strong.Verify("a-long-enough-password", encoded)
	if !ok || !needsRehash {
		t.Fatalf("want ok + needsRehash against stronger parameters, got ok=%v needsRehash=%v", ok, needsRehash)
	}
}

func TestLegacyPBKDF2HashVerifiesAndRequestsUpgrade(t *testing.T) {
	// A hash in the format written by the first release.
	salt := []byte("0123456789abcdef")
	key := pbkdf2SHA256([]byte("a-long-enough-password"), salt, 1000, keyLen)
	legacy := "pbkdf2-sha256$1000$" +
		base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(key)

	h := NewHasher(DefaultParams())
	ok, needsRehash := h.Verify("a-long-enough-password", legacy)
	if !ok {
		t.Fatal("legacy PBKDF2 hash rejected; existing users could not sign in")
	}
	if !needsRehash {
		t.Fatal("legacy hash must be flagged for upgrade to Argon2id")
	}
	if ok, _ := h.Verify("wrong-password-here", legacy); ok {
		t.Fatal("legacy verifier accepted the wrong password")
	}

	// After rehashing, the account is on Argon2id and no further upgrade is asked.
	upgraded, err := h.Hash("a-long-enough-password")
	if err != nil {
		t.Fatalf("rehash: %v", err)
	}
	if !strings.HasPrefix(upgraded, "argon2id$") {
		t.Fatalf("rehash did not produce Argon2id: %q", upgraded)
	}
	if ok, needsRehash := h.Verify("a-long-enough-password", upgraded); !ok || needsRehash {
		t.Fatalf("upgraded hash not accepted cleanly: ok=%v needsRehash=%v", ok, needsRehash)
	}
}

func TestPasswordPolicy(t *testing.T) {
	h := NewHasher(TestParams())
	for name, password := range map[string]string{
		"too short": "short",
		"empty":     "",
		"too long":  strings.Repeat("a", maxPasswordLen+1),
	} {
		if _, err := h.Hash(password); !errors.Is(err, ErrWeakPassword) {
			t.Fatalf("%s: want ErrWeakPassword, got %v", name, err)
		}
	}
}

func TestMalformedHashesAreRejectedNotPanicked(t *testing.T) {
	h := NewHasher(TestParams())
	valid, err := h.Hash("a-long-enough-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	parts := strings.Split(valid, "$")

	cases := []string{
		"", "plain", "argon2id$", "argon2id$v=19$m=0,t=1,p=1$c2FsdA$a2V5",
		"argon2id$v=16$m=8192,t=1,p=1$c2FsdA$a2V5",
		"argon2id$v=19$m=8192,t=x,p=1$c2FsdA$a2V5",
		"argon2id$v=19$m=8192,t=1,p=1$***$a2V5",
		"argon2id$v=19$m=8192,t=1,p=1$c2FsdA$***",
		"argon2id$v=19$m=8192,t=1,p=9999$c2FsdA$a2V5",
		"argon2id$v=19$m=8192,t=1,p=1$c2FsdA",
		"pbkdf2-sha256$notanumber$c2FsdA$a2V5",
		"pbkdf2-sha256$0$c2FsdA$a2V5",
		"scrypt$1$2$3",
	}
	// A hash whose parameter block is missing entirely.
	cases = append(cases, parts[0]+"$"+parts[1]+"$"+parts[3]+"$"+parts[4])

	for _, encoded := range cases {
		if ok, _ := h.Verify("whatever-long-enough", encoded); ok {
			t.Fatalf("malformed hash accepted: %q", encoded)
		}
	}
}
