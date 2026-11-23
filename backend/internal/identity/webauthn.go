package identity

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// WebAuthn (passkeys) verification.
//
// Scope, stated plainly: this implements the parts of the Web Authentication
// Level 2 ceremony that a server must check, for the two attestation formats
// that matter in practice (`none` and `packed` self-attestation) and the two
// algorithms Android and Apple platform authenticators use (ES256, RS256).
// Attestation chains, enterprise attestation and metadata services are out of
// scope: we do not make a trust decision about the authenticator model, we only
// want a phishing-resistant second factor.
//
// Every check below is a rejection, not a preference:
//   - the challenge must match the one we issued, in constant time, and be
//     consumed atomically (single use) or a captured response could be replayed;
//   - the origin must be one we configured, or a look-alike site could relay;
//   - the RP ID hash must be ours, or a credential registered for another site
//     would be accepted;
//   - the signature must verify against the stored public key, or possession of
//     the credential id alone would be enough;
//   - the signature counter must not go backwards, which is the only signal a
//     cloned authenticator gives.

var (
	// ErrInvalidAssertion covers every malformed or unverifiable WebAuthn
	// response. The caller gets one generic error on purpose: telling an
	// attacker which check failed helps them.
	ErrInvalidAssertion = errors.New("identity: invalid webauthn assertion")
	// ErrChallengeConsumed means the challenge was already used or has expired.
	ErrChallengeConsumed = errors.New("identity: webauthn challenge expired or already used")
	// ErrCredentialCloned is reported when the counter regresses. The credential
	// is not deleted automatically; that is a decision for the account owner.
	ErrCredentialCloned = errors.New("identity: authenticator counter regressed; possible clone")
	// ErrUnsupportedAlgorithm is returned for COSE keys we do not verify.
	ErrUnsupportedAlgorithm = errors.New("identity: unsupported webauthn algorithm")
	// ErrWebAuthnUnavailable means the RP configuration is missing.
	ErrWebAuthnUnavailable = errors.New("identity: webauthn is not configured")
)

const (
	coseAlgES256 = -7
	coseAlgRS256 = -257

	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttestedData = 0x40
	flagExtensions   = 0x80

	webauthnChallengeTTL = 5 * time.Minute
	credentialIDMinLen   = 16
	credentialIDMaxLen   = 1023
)

// WebAuthnConfig is the relying-party identity. RPID is the registrable domain
// (or host for localhost), and every allowed origin must be consistent with it.
type WebAuthnConfig struct {
	RPID                    string
	RPName                  string
	Origins                 []string
	RequireUserVerification bool
}

// Validate rejects configurations that would make verification meaningless.
func (c WebAuthnConfig) Validate() error {
	if c.RPID == "" {
		return fmt.Errorf("%w: relying party id is empty", ErrInvalidInput)
	}
	if len(c.Origins) == 0 {
		return fmt.Errorf("%w: at least one origin is required", ErrInvalidInput)
	}
	for _, o := range c.Origins {
		if !strings.HasPrefix(o, "https://") && !strings.HasPrefix(o, "http://localhost") {
			return fmt.Errorf("%w: origin %q must be https (http is allowed only for localhost)", ErrInvalidInput, o)
		}
	}
	return nil
}

func (c WebAuthnConfig) originAllowed(origin string) bool {
	for _, o := range c.Origins {
		if subtle.ConstantTimeCompare([]byte(o), []byte(origin)) == 1 {
			return true
		}
	}
	return false
}

// clientData is the JSON blob the browser signs. Field names are fixed by the
// spec; anything else is ignored.
type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

func parseClientData(raw []byte) (clientData, error) {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return clientData{}, fmt.Errorf("%w: client data is not JSON", ErrInvalidAssertion)
	}
	return cd, nil
}

func (c WebAuthnConfig) checkClientData(raw []byte, wantType string, challenge []byte) error {
	cd, err := parseClientData(raw)
	if err != nil {
		return err
	}
	if cd.Type != wantType {
		return fmt.Errorf("%w: expected client data type %q", ErrInvalidAssertion, wantType)
	}
	got, err := decodeBase64URL(cd.Challenge)
	if err != nil {
		return fmt.Errorf("%w: challenge is not base64url", ErrInvalidAssertion)
	}
	if subtle.ConstantTimeCompare(got, challenge) != 1 {
		return fmt.Errorf("%w: challenge does not match the issued one", ErrInvalidAssertion)
	}
	if cd.CrossOrigin {
		return fmt.Errorf("%w: cross-origin ceremonies are refused", ErrInvalidAssertion)
	}
	if !c.originAllowed(cd.Origin) {
		return fmt.Errorf("%w: origin %q is not allowed", ErrInvalidAssertion, cd.Origin)
	}
	return nil
}

// decodeBase64URL accepts both padded and unpadded base64url, because browsers
// and native libraries disagree about padding.
func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	if strings.ContainsAny(s, "+/") {
		return nil, fmt.Errorf("%w: not url-safe base64", ErrInvalidAssertion)
	}
	return base64.RawURLEncoding.DecodeString(s)
}

type parsedAuthenticatorData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint64
	credID    []byte
	coseKey   []byte
	aaguid    []byte
	raw       []byte
}

func parseAuthenticatorData(raw []byte) (parsedAuthenticatorData, error) {
	if len(raw) < 37 {
		return parsedAuthenticatorData{}, fmt.Errorf("%w: authenticator data is truncated", ErrInvalidAssertion)
	}
	ad := parsedAuthenticatorData{
		rpIDHash: raw[0:32],
		flags:    raw[32],
		raw:      raw,
	}
	ad.signCount = uint64(binary.BigEndian.Uint32(raw[33:37]))
	rest := raw[37:]
	if ad.flags&flagAttestedData != 0 {
		if len(rest) < 18 {
			return parsedAuthenticatorData{}, fmt.Errorf("%w: attested credential data is truncated", ErrInvalidAssertion)
		}
		ad.aaguid = rest[0:16]
		idLen := int(binary.BigEndian.Uint16(rest[16:18]))
		if idLen < credentialIDMinLen || idLen > credentialIDMaxLen {
			return parsedAuthenticatorData{}, fmt.Errorf("%w: credential id length %d is out of range", ErrInvalidAssertion, idLen)
		}
		if len(rest) < 18+idLen {
			return parsedAuthenticatorData{}, fmt.Errorf("%w: credential id is truncated", ErrInvalidAssertion)
		}
		ad.credID = rest[18 : 18+idLen]
		key := rest[18+idLen:]
		if len(key) == 0 {
			return parsedAuthenticatorData{}, fmt.Errorf("%w: credential public key is missing", ErrInvalidAssertion)
		}
		// The COSE key is a self-delimiting CBOR item; anything after it is an
		// extension we do not need.
		var rest any
		dec := cbor.NewDecoder(bytes.NewReader(key))
		if err := dec.Decode(&rest); err != nil {
			return parsedAuthenticatorData{}, fmt.Errorf("%w: credential public key is not valid CBOR", ErrInvalidAssertion)
		}
		ad.coseKey = key[:dec.NumBytesRead()]
	}
	return ad, nil
}

func (c WebAuthnConfig) checkAuthenticatorData(ad parsedAuthenticatorData) error {
	want := sha256.Sum256([]byte(c.RPID))
	if subtle.ConstantTimeCompare(want[:], ad.rpIDHash) != 1 {
		return fmt.Errorf("%w: relying party id hash does not match", ErrInvalidAssertion)
	}
	if ad.flags&flagUserPresent == 0 {
		return fmt.Errorf("%w: user presence flag is not set", ErrInvalidAssertion)
	}
	if c.RequireUserVerification && ad.flags&flagUserVerified == 0 {
		return fmt.Errorf("%w: user verification is required", ErrInvalidAssertion)
	}
	return nil
}

// cosePublicKey is the subset of COSE keys we can verify with.
type cosePublicKey struct {
	alg   int
	ecdsa *ecdsa.PublicKey
	rsa   *rsa.PublicKey
}

func parseCOSEKey(raw []byte) (cosePublicKey, error) {
	var m map[int64]any
	if err := cbor.Unmarshal(raw, &m); err != nil {
		return cosePublicKey{}, fmt.Errorf("%w: COSE key is not a map", ErrInvalidAssertion)
	}
	kty, ok := cborInt(m[1])
	if !ok {
		return cosePublicKey{}, fmt.Errorf("%w: COSE key has no kty", ErrInvalidAssertion)
	}
	alg, ok := cborInt(m[3])
	if !ok {
		return cosePublicKey{}, fmt.Errorf("%w: COSE key has no alg", ErrInvalidAssertion)
	}
	switch {
	case kty == 2 && alg == coseAlgES256:
		crv, _ := cborInt(m[-1])
		x, xok := m[-2].([]byte)
		y, yok := m[-3].([]byte)
		if crv != 1 || !xok || !yok {
			return cosePublicKey{}, fmt.Errorf("%w: ES256 key is malformed", ErrInvalidAssertion)
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return cosePublicKey{}, fmt.Errorf("%w: EC point is not on the curve", ErrInvalidAssertion)
		}
		return cosePublicKey{alg: alg, ecdsa: pub}, nil
	case kty == 3 && alg == coseAlgRS256:
		n, nok := m[-1].([]byte)
		e, eok := m[-2].([]byte)
		if !nok || !eok || len(e) == 0 || len(e) > 4 {
			return cosePublicKey{}, fmt.Errorf("%w: RSA key is malformed", ErrInvalidAssertion)
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		if exponent < 3 {
			return cosePublicKey{}, fmt.Errorf("%w: RSA exponent is too small", ErrInvalidAssertion)
		}
		return cosePublicKey{alg: alg, rsa: &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}}, nil
	}
	return cosePublicKey{}, fmt.Errorf("%w: kty=%d alg=%d", ErrUnsupportedAlgorithm, kty, alg)
}

// cborInt extracts a small integer from a decoded CBOR value. COSE keys and
// algorithm identifiers are small by definition, so anything outside the int32
// range is rejected instead of narrowed.
func cborInt(v any) (int, bool) {
	const limit = 1 << 31
	switch n := v.(type) {
	case int64:
		if n < -limit || n > limit {
			return 0, false
		}
		return int(n), true // #nosec G115 -- range checked above
	case uint64:
		if n > limit {
			return 0, false
		}
		return int(n), true // #nosec G115 -- range checked above
	case int:
		return n, true
	}
	return 0, false
}

func (k cosePublicKey) verify(msg, sig []byte) error {
	digest := sha256.Sum256(msg)
	switch {
	case k.ecdsa != nil:
		// WebAuthn carries ES256 signatures as ASN.1 DER, not raw r||s.
		if !ecdsa.VerifyASN1(k.ecdsa, digest[:], sig) {
			return fmt.Errorf("%w: ES256 signature does not verify", ErrInvalidAssertion)
		}
		return nil
	case k.rsa != nil:
		if err := rsa.VerifyPKCS1v15(k.rsa, crypto.SHA256, digest[:], sig); err != nil {
			return fmt.Errorf("%w: RS256 signature does not verify", ErrInvalidAssertion)
		}
		return nil
	}
	return ErrUnsupportedAlgorithm
}

// registrationResponse is the JSON body a client sends after create().
type RegistrationResponse struct {
	ID                      string   `json:"id"`
	RawID                   string   `json:"raw_id"`
	ClientData              string   `json:"client_data_json"`
	Attestation             string   `json:"attestation_object"`
	Transports              []string `json:"transports"`
	AuthenticatorAttachment string   `json:"authenticator_attachment"`
}

// verifiedCredential is what registration produces: the public key and metadata
// needed to verify future assertions.
type verifiedCredential struct {
	credentialID []byte
	publicKey    []byte
	alg          int
	signCount    uint64
	aaguid       []byte
	transports   []string
}

func (c WebAuthnConfig) verifyRegistration(challenge []byte, in RegistrationResponse) (verifiedCredential, error) {
	clientDataRaw, err := decodeBase64URL(in.ClientData)
	if err != nil {
		return verifiedCredential{}, err
	}
	if err := c.checkClientData(clientDataRaw, "webauthn.create", challenge); err != nil {
		return verifiedCredential{}, err
	}
	attRaw, err := decodeBase64URL(in.Attestation)
	if err != nil {
		return verifiedCredential{}, fmt.Errorf("%w: attestation object is not base64url", ErrInvalidAssertion)
	}
	var att struct {
		Format   string          `cbor:"fmt"`
		AuthData []byte          `cbor:"authData"`
		AttStmt  cbor.RawMessage `cbor:"attStmt"`
	}
	if err := cbor.Unmarshal(attRaw, &att); err != nil {
		return verifiedCredential{}, fmt.Errorf("%w: attestation object is not valid CBOR", ErrInvalidAssertion)
	}
	ad, err := parseAuthenticatorData(att.AuthData)
	if err != nil {
		return verifiedCredential{}, err
	}
	if ad.flags&flagAttestedData == 0 {
		return verifiedCredential{}, fmt.Errorf("%w: registration is missing attested credential data", ErrInvalidAssertion)
	}
	if err := c.checkAuthenticatorData(ad); err != nil {
		return verifiedCredential{}, err
	}
	key, err := parseCOSEKey(ad.coseKey)
	if err != nil {
		return verifiedCredential{}, err
	}
	clientHash := sha256.Sum256(clientDataRaw)

	switch att.Format {
	case "none":
		// No attestation statement to check. The signature over future
		// assertions still proves possession, which is what we rely on.
	case "packed":
		if err := c.verifyPackedAttestation(att.AttStmt, ad.raw, clientHash[:], key); err != nil {
			return verifiedCredential{}, err
		}
	default:
		return verifiedCredential{}, fmt.Errorf("%w: attestation format %q is not supported", ErrInvalidAssertion, att.Format)
	}

	rawID := in.RawID
	if rawID == "" {
		rawID = in.ID
	}
	claimedID, err := decodeBase64URL(rawID)
	if err != nil {
		return verifiedCredential{}, fmt.Errorf("%w: credential id is not base64url", ErrInvalidAssertion)
	}
	// The id the client claims must be the id the authenticator signed over,
	// otherwise a client could register one credential and store another.
	if subtle.ConstantTimeCompare(claimedID, ad.credID) != 1 {
		return verifiedCredential{}, fmt.Errorf("%w: credential id does not match the attested one", ErrInvalidAssertion)
	}

	return verifiedCredential{
		credentialID: ad.credID,
		publicKey:    ad.coseKey,
		alg:          key.alg,
		signCount:    ad.signCount,
		aaguid:       ad.aaguid,
		transports:   sanitizeTransports(in.Transports),
	}, nil
}

func (c WebAuthnConfig) verifyPackedAttestation(raw cbor.RawMessage, authData, clientHash []byte, key cosePublicKey) error {
	var stmt map[string]any
	if len(raw) == 0 {
		return fmt.Errorf("%w: packed attestation has no statement", ErrInvalidAssertion)
	}
	if err := cbor.Unmarshal(raw, &stmt); err != nil {
		return fmt.Errorf("%w: packed attestation statement is malformed", ErrInvalidAssertion)
	}
	sig, ok := stmt["sig"].([]byte)
	if !ok {
		return fmt.Errorf("%w: packed attestation has no signature", ErrInvalidAssertion)
	}
	if alg, ok := cborInt(stmt["alg"]); ok && alg != coseAlgES256 && alg != coseAlgRS256 {
		return fmt.Errorf("%w: packed attestation alg %d", ErrUnsupportedAlgorithm, alg)
	}
	// Self-attestation: the credential key signs authData || clientDataHash.
	msg := append(append([]byte{}, authData...), clientHash...)
	if err := key.verify(msg, sig); err != nil {
		return fmt.Errorf("%w: packed attestation signature", ErrInvalidAssertion)
	}
	return nil
}

func sanitizeTransports(in []string) []string {
	allowed := map[string]bool{"usb": true, "nfc": true, "ble": true, "internal": true, "hybrid": true, "smart-card": true}
	out := make([]string, 0, len(in))
	for _, t := range in {
		if allowed[t] {
			out = append(out, t)
		}
	}
	return out
}

// assertionResponse is the JSON body a client sends after get().
type AssertionResponse struct {
	ID            string `json:"id"`
	RawID         string `json:"raw_id"`
	ClientData    string `json:"client_data_json"`
	Authenticator string `json:"authenticator_data"`
	Signature     string `json:"signature"`
	UserHandle    string `json:"user_handle"`
}

// verifyAssertion checks an authentication response against a stored credential.
// It returns the new signature counter, which the caller persists.
func (c WebAuthnConfig) verifyAssertion(challenge []byte, stored cosePublicKey, storedCount uint64, in AssertionResponse) (uint64, error) {
	clientDataRaw, err := decodeBase64URL(in.ClientData)
	if err != nil {
		return 0, err
	}
	if err := c.checkClientData(clientDataRaw, "webauthn.get", challenge); err != nil {
		return 0, err
	}
	authDataRaw, err := decodeBase64URL(in.Authenticator)
	if err != nil {
		return 0, fmt.Errorf("%w: authenticator data is not base64url", ErrInvalidAssertion)
	}
	sig, err := decodeBase64URL(in.Signature)
	if err != nil {
		return 0, fmt.Errorf("%w: signature is not base64url", ErrInvalidAssertion)
	}
	ad, err := parseAuthenticatorData(authDataRaw)
	if err != nil {
		return 0, err
	}
	if ad.flags&flagAttestedData != 0 {
		// An assertion must not carry attested credential data; accepting it
		// would allow a registration blob to be replayed as an assertion.
		return 0, fmt.Errorf("%w: assertion carries attested credential data", ErrInvalidAssertion)
	}
	if err := c.checkAuthenticatorData(ad); err != nil {
		return 0, err
	}
	clientHash := sha256.Sum256(clientDataRaw)
	msg := append(append([]byte{}, authDataRaw...), clientHash[:]...)
	if err := stored.verify(msg, sig); err != nil {
		return 0, err
	}
	if storedCount > 0 && ad.signCount > 0 && ad.signCount <= storedCount {
		return 0, ErrCredentialCloned
	}
	return ad.signCount, nil
}
