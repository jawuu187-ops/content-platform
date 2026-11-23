package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// testAuthenticator is a software authenticator: it produces the same structures
// a platform authenticator does, so the server-side verification is exercised
// without a browser or a real device.
type testAuthenticator struct {
	key       *ecdsa.PrivateKey
	credID    []byte
	signCount uint32
	rpID      string
	origin    string
}

func newTestAuthenticator(t *testing.T, rpID, origin string) *testAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	credID := make([]byte, 32)
	if _, err := rand.Read(credID); err != nil {
		t.Fatalf("credential id: %v", err)
	}
	return &testAuthenticator{key: key, credID: credID, rpID: rpID, origin: origin}
}

func (a *testAuthenticator) coseKey() []byte {
	// PublicKey.Bytes returns the uncompressed point: 0x04 || X || Y.
	point := a.key.PublicKey.Bytes()
	if len(point) != 65 {
		panic("unexpected public key length")
	}
	x, y := point[1:33], point[33:65]
	raw, err := cbor.Marshal(map[int]any{
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: x,
		-3: y,
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func (a *testAuthenticator) clientData(t *testing.T, typ, challenge string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type":        typ,
		"challenge":   challenge,
		"origin":      a.origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("client data: %v", err)
	}
	return raw
}

// authData builds the binary authenticator data blob.
func (a *testAuthenticator) authData(t *testing.T, flags byte, count uint32) []byte {
	t.Helper()
	rpHash := sha256.Sum256([]byte(a.rpID))
	out := make([]byte, 0, 128)
	out = append(out, rpHash[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, count)
	if flags&flagAttestedData != 0 {
		out = append(out, make([]byte, 16)...) // aaguid: all zero, "no model claim"
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.credID)))
		out = append(out, a.credID...)
		out = append(out, a.coseKey()...)
	}
	return out
}

// registration produces a `none`-attestation create() response.
func (a *testAuthenticator) registration(t *testing.T, challenge string) RegistrationResponse {
	t.Helper()
	cd := a.clientData(t, "webauthn.create", challenge)
	ad := a.authData(t, flagUserPresent|flagUserVerified|flagAttestedData, 0)
	att, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": ad,
	})
	if err != nil {
		t.Fatalf("attestation object: %v", err)
	}
	return RegistrationResponse{
		ID:          base64.RawURLEncoding.EncodeToString(a.credID),
		RawID:       base64.RawURLEncoding.EncodeToString(a.credID),
		ClientData:  base64.RawURLEncoding.EncodeToString(cd),
		Attestation: base64.RawURLEncoding.EncodeToString(att),
		Transports:  []string{"internal", "usb", "evil-transport"},
	}
}

// packedRegistration produces a `packed` self-attestation response.
func (a *testAuthenticator) packedRegistration(t *testing.T, challenge string) RegistrationResponse {
	t.Helper()
	cd := a.clientData(t, "webauthn.create", challenge)
	ad := a.authData(t, flagUserPresent|flagUserVerified|flagAttestedData, 0)
	clientHash := sha256.Sum256(cd)
	msg := append(append([]byte{}, ad...), clientHash[:]...)
	digest := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatalf("attestation signature: %v", err)
	}
	att, err := cbor.Marshal(map[string]any{
		"fmt":      "packed",
		"attStmt":  map[string]any{"alg": -7, "sig": sig},
		"authData": ad,
	})
	if err != nil {
		t.Fatalf("packed attestation: %v", err)
	}
	return RegistrationResponse{
		ID:          base64.RawURLEncoding.EncodeToString(a.credID),
		RawID:       base64.RawURLEncoding.EncodeToString(a.credID),
		ClientData:  base64.RawURLEncoding.EncodeToString(cd),
		Attestation: base64.RawURLEncoding.EncodeToString(att),
	}
}

// assertion produces a get() response and advances the counter.
func (a *testAuthenticator) assertion(t *testing.T, challenge string) AssertionResponse {
	t.Helper()
	a.signCount++
	return a.assertionWithCount(t, challenge, a.signCount)
}

// assertionFrom builds a correctly signed assertion from a given origin and RP
// ID, using the same key and credential id. That is exactly what a relay attack
// looks like: cryptographically valid, contextually wrong.
func (a *testAuthenticator) assertionFrom(t *testing.T, challenge, origin, rpID string) AssertionResponse {
	t.Helper()
	clone := &testAuthenticator{key: a.key, credID: a.credID, origin: origin, rpID: rpID}
	return clone.assertion(t, challenge)
}

func (a *testAuthenticator) assertionWithCount(t *testing.T, challenge string, count uint32) AssertionResponse {
	t.Helper()
	cd := a.clientData(t, "webauthn.get", challenge)
	ad := a.authData(t, flagUserPresent|flagUserVerified, count)
	clientHash := sha256.Sum256(cd)
	msg := append(append([]byte{}, ad...), clientHash[:]...)
	digest := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatalf("assertion signature: %v", err)
	}
	return AssertionResponse{
		ID:            base64.RawURLEncoding.EncodeToString(a.credID),
		RawID:         base64.RawURLEncoding.EncodeToString(a.credID),
		ClientData:    base64.RawURLEncoding.EncodeToString(cd),
		Authenticator: base64.RawURLEncoding.EncodeToString(ad),
		Signature:     base64.RawURLEncoding.EncodeToString(sig),
	}
}

func testWebAuthnConfig(rpID, origin string) WebAuthnConfig {
	return WebAuthnConfig{
		RPID:                    rpID,
		RPName:                  "Content Platform Test",
		Origins:                 []string{origin},
		RequireUserVerification: true,
	}
}

// passkeyService builds a service with passkeys enabled on a fresh database.
func passkeyService(t *testing.T) (*Service, WebAuthnConfig) {
	t.Helper()
	cfg := testWebAuthnConfig("example.test", "https://app.example.test")
	svc, _, _ := newTestService(t, WithWebAuthn(cfg))
	return svc, cfg
}

func TestPasskeyRegistrationAndLogin(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	email := uniqueEmail("passkey")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])

	opts, err := svc.StartRegistration(ctx, res.Principal, "Pixel 9")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	if opts.PublicKey.RP.ID != cfg.RPID || opts.PublicKey.Challenge == "" {
		t.Fatalf("registration options are incomplete: %+v", opts.PublicKey)
	}
	if len(opts.PublicKey.PubKeyCredParams) != 2 {
		t.Fatalf("want ES256 and RS256 offered, got %v", opts.PublicKey.PubKeyCredParams)
	}

	cred, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "Pixel 9", "")
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	if cred.ID == "" || cred.Label != "Pixel 9" {
		t.Fatalf("stored credential is wrong: %+v", cred)
	}
	// Unknown transports are dropped rather than stored.
	for _, tr := range cred.Transports {
		if tr == "evil-transport" {
			t.Fatalf("unsanitised transport stored: %v", cred.Transports)
		}
	}

	list, err := svc.Credentials(ctx, res.Principal)
	if err != nil || len(list) != 1 {
		t.Fatalf("list credentials: %v %v", list, err)
	}

	// Login by email: the options must name the credential so the platform
	// authenticator can preselect it.
	login, err := svc.StartLogin(ctx, email)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	if len(login.PublicKey.AllowCredentials) != 1 {
		t.Fatalf("login options do not list the credential: %+v", login.PublicKey)
	}

	got, err := svc.FinishLogin(ctx, login.ChallengeID, auth.assertion(t, login.PublicKey.Challenge),
		DeviceInfo{Fingerprint: "d1", Platform: "android"}, "")
	if err != nil {
		t.Fatalf("finish login: %v", err)
	}
	if got.AccessToken == "" {
		t.Fatal("passkey login produced no access token")
	}
	if _, err := svc.Authenticate(ctx, got.AccessToken); err != nil {
		t.Fatalf("the issued token does not authenticate: %v", err)
	}

	// The stored counter advanced, so a regression can be detected next time.
	list, err = svc.Credentials(ctx, res.Principal)
	if err != nil {
		t.Fatalf("relist: %v", err)
	}
	if list[0].SignCount == 0 {
		t.Fatal("signature counter was not persisted")
	}
}

func TestPasskeyLoginWithoutEmailDiscoversTheAccount(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	email := uniqueEmail("discover")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", ""); err != nil {
		t.Fatalf("finish registration: %v", err)
	}

	login, err := svc.StartLogin(ctx, "") // no email, no user binding
	if err != nil {
		t.Fatalf("start discoverable login: %v", err)
	}
	if len(login.PublicKey.AllowCredentials) != 0 {
		t.Fatal("a discoverable login must not narrow the credentials")
	}
	got, err := svc.FinishLogin(ctx, login.ChallengeID, auth.assertion(t, login.PublicKey.Challenge),
		DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("finish login: %v", err)
	}
	// Addresses are stored lower-cased, so compare without case.
	if !strings.EqualFold(got.User.Email, email) {
		t.Fatalf("logged in as %q, want %q", got.User.Email, email)
	}
}

func TestPasskeyRegistrationRejectsWrongChallenge(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	res, err := svc.Register(ctx, uniqueEmail("wrongchal"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	other := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	// Signed over a different challenge than the one issued.
	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		other.registration(t, "AAAA"+opts.PublicKey.Challenge), "", ""); err == nil {
		t.Fatal("registration with a mismatched challenge was accepted")
	}
}

func TestPasskeyRegistrationRejectsForeignChallengeOwner(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	alice, err := svc.Register(ctx, uniqueEmail("alice-pk"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register alice: %v", err)
	}
	bob, err := svc.Register(ctx, uniqueEmail("bob-pk"), goodPassword, DeviceInfo{Fingerprint: "d2"}, "")
	if err != nil {
		t.Fatalf("register bob: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, alice.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	// Bob tries to finish Alice's ceremony.
	if _, err := svc.FinishRegistration(ctx, bob.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", ""); err == nil {
		t.Fatal("one account finished another account's registration challenge")
	}
}

func TestPasskeyReplayOfAConsumedChallengeFails(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	email := uniqueEmail("replay")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", ""); err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	// The same enrollment response again: the challenge is already consumed.
	_, err = svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", "")
	if err == nil {
		t.Fatal("a registration response was replayed successfully")
	}

	login, err := svc.StartLogin(ctx, email)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	assertion := auth.assertion(t, login.PublicKey.Challenge)
	if _, err := svc.FinishLogin(ctx, login.ChallengeID, assertion, DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("first login: %v", err)
	}
	// Replaying the assertion with the same challenge id must fail before the
	// signature is even considered.
	if _, err := svc.FinishLogin(ctx, login.ChallengeID, assertion, DeviceInfo{Fingerprint: "d1"}, ""); err == nil {
		t.Fatal("an assertion was replayed successfully")
	}
}

func TestPasskeyAssertionRejectsTampering(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	email := uniqueEmail("tamper")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", ""); err != nil {
		t.Fatalf("finish registration: %v", err)
	}

	var lastChallenge string
	cases := []struct {
		name   string
		mutate func(*AssertionResponse)
	}{
		{"bad signature", func(a *AssertionResponse) {
			// Flip one base64 character of the signature.
			sig := []byte(a.Signature)
			if sig[0] == 'A' {
				sig[0] = 'B'
			} else {
				sig[0] = 'A'
			}
			a.Signature = string(sig)
		}},
		{"wrong origin", func(a *AssertionResponse) {
			relayed := auth.assertionFrom(t, lastChallenge, "https://evil.example", cfg.RPID)
			*a = relayed
		}},
		{"wrong rp id", func(a *AssertionResponse) {
			relayed := auth.assertionFrom(t, lastChallenge, cfg.Origins[0], "evil.example")
			*a = relayed
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			login, err := svc.StartLogin(ctx, email)
			if err != nil {
				t.Fatalf("start login: %v", err)
			}
			lastChallenge = login.PublicKey.Challenge
			assertion := auth.assertion(t, login.PublicKey.Challenge)
			tc.mutate(&assertion)
			if _, err := svc.FinishLogin(ctx, login.ChallengeID, assertion, DeviceInfo{Fingerprint: "d1"}, ""); err == nil {
				t.Fatalf("%s: assertion was accepted", tc.name)
			}
		})
	}
}

func TestPasskeyCounterRegressionIsReported(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	email := uniqueEmail("clone")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", ""); err != nil {
		t.Fatalf("finish registration: %v", err)
	}

	login, err := svc.StartLogin(ctx, email)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	if _, err := svc.FinishLogin(ctx, login.ChallengeID,
		auth.assertionWithCount(t, login.PublicKey.Challenge, 7), DeviceInfo{Fingerprint: "d1"}, ""); err != nil {
		t.Fatalf("first login at counter 7: %v", err)
	}

	second, err := svc.StartLogin(ctx, email)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_, err = svc.FinishLogin(ctx, second.ChallengeID,
		auth.assertionWithCount(t, second.PublicKey.Challenge, 4), DeviceInfo{Fingerprint: "d1"}, "")
	if err == nil || err != ErrCredentialCloned {
		t.Fatalf("want ErrCredentialCloned, got %v", err)
	}
}

func TestRevokedPasskeyCannotAuthenticate(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	email := uniqueEmail("revoked")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	cred, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", "")
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}

	// Another account cannot revoke it.
	other, err := svc.Register(ctx, uniqueEmail("other"), goodPassword, DeviceInfo{Fingerprint: "d2"}, "")
	if err != nil {
		t.Fatalf("register other: %v", err)
	}
	if err := svc.RevokeCredential(ctx, other.Principal, cred.ID, ""); err == nil {
		t.Fatal("a credential was revoked by a different account")
	}
	if err := svc.RevokeCredential(ctx, res.Principal, cred.ID, ""); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := svc.RevokeCredential(ctx, res.Principal, cred.ID, ""); err == nil {
		t.Fatal("revoking twice reported success")
	}

	login, err := svc.StartLogin(ctx, email)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	if len(login.PublicKey.AllowCredentials) != 0 {
		t.Fatal("a revoked credential is still offered to the authenticator")
	}
	if _, err := svc.FinishLogin(ctx, login.ChallengeID, auth.assertion(t, login.PublicKey.Challenge),
		DeviceInfo{Fingerprint: "d1"}, ""); err == nil {
		t.Fatal("a revoked credential authenticated")
	}
}

func TestPasskeyPackedAttestationIsAccepted(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	res, err := svc.Register(ctx, uniqueEmail("packed"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.packedRegistration(t, opts.PublicKey.Challenge), "", ""); err != nil {
		t.Fatalf("packed attestation rejected: %v", err)
	}
}

func TestPasskeyUnknownAttestationFormatIsRejected(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	res, err := svc.Register(ctx, uniqueEmail("fmt"), goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	in := auth.registration(t, opts.PublicKey.Challenge)
	att, _ := base64.RawURLEncoding.DecodeString(in.Attestation)
	var m map[string]any
	if err := cbor.Unmarshal(att, &m); err != nil {
		t.Fatalf("decode attestation: %v", err)
	}
	m["fmt"] = "tpm"
	raw, _ := cbor.Marshal(m)
	in.Attestation = base64.RawURLEncoding.EncodeToString(raw)

	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID, in, "", ""); err == nil {
		t.Fatal("an unsupported attestation format was accepted")
	}
}

func TestExpiredPasskeyChallengeIsRejected(t *testing.T) {
	svc, cfg := passkeyService(t)
	ctx := t.Context()
	base := time.Now().UTC()
	svc.now = func() time.Time { return base }

	email := uniqueEmail("expiredchal")
	res, err := svc.Register(ctx, email, goodPassword, DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	auth := newTestAuthenticator(t, cfg.RPID, cfg.Origins[0])
	opts, err := svc.StartRegistration(ctx, res.Principal, "")
	if err != nil {
		t.Fatalf("start registration: %v", err)
	}
	// The challenge TTL is measured by Postgres now(), so move the clock past it
	// by expiring the row directly: the service must refuse the ceremony.
	if _, err := svc.store.pool.Exec(ctx, `
		UPDATE identity.webauthn_challenges SET expires_at = now() - interval '1 second'
		WHERE id = $1::uuid`, opts.ChallengeID); err != nil {
		t.Fatalf("expire challenge: %v", err)
	}
	if _, err := svc.FinishRegistration(ctx, res.Principal, opts.ChallengeID,
		auth.registration(t, opts.PublicKey.Challenge), "", ""); err == nil {
		t.Fatal("an expired challenge was accepted")
	}
}

func TestWebAuthnConfigValidation(t *testing.T) {
	if err := (WebAuthnConfig{}).Validate(); err == nil {
		t.Fatal("empty config accepted")
	}
	if err := (WebAuthnConfig{RPID: "example.test"}).Validate(); err == nil {
		t.Fatal("config without origins accepted")
	}
	if err := (WebAuthnConfig{RPID: "example.test", Origins: []string{"http://example.test"}}).Validate(); err == nil {
		t.Fatal("plain http origin accepted for a non-local host")
	}
	if err := (WebAuthnConfig{RPID: "localhost", Origins: []string{"http://localhost"}}).Validate(); err != nil {
		t.Fatalf("localhost over http was rejected: %v", err)
	}
}
