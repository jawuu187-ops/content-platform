package identity

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B vectors for HMAC-SHA1. The published vectors use 8 digits;
// totpCode takes the digit count as a parameter so the same vectors apply.
// Secret "12345678901234567890" base32-encoded without padding.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, tc := range cases {
		got, err := totpCode(rfcSecret, uint64(tc.unix/30), 8)
		if err != nil {
			t.Fatalf("t=%d: %v", tc.unix, err)
		}
		if got != tc.want {
			t.Fatalf("t=%d: got %s want %s", tc.unix, got, tc.want)
		}
	}
}

func TestVerifyTOTPAcceptsAdjacentStepsOnly(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()

	current, err := TOTPCodeAt(secret, now)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if !VerifyTOTP(secret, current, now) {
		t.Fatal("current code rejected")
	}

	// One period earlier and later are accepted (clock drift).
	for _, offset := range []time.Duration{-totpPeriod, totpPeriod} {
		code, err := TOTPCodeAt(secret, now.Add(offset))
		if err != nil {
			t.Fatalf("code: %v", err)
		}
		if !VerifyTOTP(secret, code, now) {
			t.Fatalf("code from %v rejected despite the accepted skew", offset)
		}
	}
	// Two periods away must be rejected.
	code, err := TOTPCodeAt(secret, now.Add(3*totpPeriod))
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if VerifyTOTP(secret, code, now) {
		t.Fatal("code three periods away was accepted")
	}

	for _, bad := range []string{"", "12345", "abcdef", "0000000"} {
		if VerifyTOTP(secret, bad, now) {
			t.Fatalf("malformed code %q accepted", bad)
		}
	}
	if VerifyTOTP("not-base32!!", current, now) {
		t.Fatal("malformed secret accepted")
	}
}

func TestTOTPURIIsEscaped(t *testing.T) {
	uri := TOTPURI("Content Platform", "fan+test@example.test", rfcSecret)
	for _, want := range []string{
		"otpauth://totp/", "secret=" + rfcSecret, "issuer=Content+Platform", "digits=6", "period=30",
	} {
		if !strings.Contains(uri, want) {
			t.Fatalf("URI %q is missing %q", uri, want)
		}
	}
}
