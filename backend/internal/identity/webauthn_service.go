package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// Passkey ceremonies. The flow is deliberately the standard one:
//
//	register: start -> browser create() -> finish (challenge consumed, credential stored)
//	login:    start -> browser get()    -> finish (challenge consumed, assertion verified, session issued)
//
// The server issues the challenge and stores it; nothing in the request is
// trusted until it has been checked against that stored value.

// Credential is the public view of a registered passkey.
type Credential struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Alg        int        `json:"alg"`
	Transports []string   `json:"transports"`
	SignCount  uint64     `json:"sign_count"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// RegistrationOptions and LoginOptions are handed to the client's WebAuthn API.
// Field names follow the WebAuthn JSON model so a browser or Android
// CredentialManager can consume them with no translation layer.
type RegistrationOptions struct {
	ChallengeID string           `json:"challenge_id"`
	PublicKey   PublicKeyOptions `json:"public_key"`
}

type PublicKeyOptions struct {
	Challenge              string                 `json:"challenge"`
	RP                     RelyingParty           `json:"rp"`
	User                   UserEntity             `json:"user"`
	PubKeyCredParams       []CredentialParameter  `json:"pubKeyCredParams"`
	Timeout                int                    `json:"timeout"`
	Attestation            string                 `json:"attestation"`
	AuthenticatorSelection AuthenticatorSelection `json:"authenticatorSelection"`
	ExcludeCredentials     []CredentialDescriptor `json:"excludeCredentials,omitempty"`
}

type LoginOptions struct {
	ChallengeID string         `json:"challenge_id"`
	PublicKey   LoginPublicKey `json:"public_key"`
}

type LoginPublicKey struct {
	Challenge        string                 `json:"challenge"`
	RPID             string                 `json:"rpId"`
	Timeout          int                    `json:"timeout"`
	UserVerification string                 `json:"userVerification"`
	AllowCredentials []CredentialDescriptor `json:"allowCredentials,omitempty"`
}

type RelyingParty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type CredentialParameter struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

type CredentialDescriptor struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	Transports []string `json:"transports,omitempty"`
}

type AuthenticatorSelection struct {
	ResidentKey      string `json:"residentKey"`
	UserVerification string `json:"userVerification"`
}

// WithWebAuthn enables passkey ceremonies. Without it every passkey route
// answers 503 instead of pretending passkeys are unsupported by the client.
func WithWebAuthn(cfg WebAuthnConfig) Option {
	return func(s *Service) {
		if cfg.Validate() == nil {
			c := cfg
			s.webauthn = &c
		}
	}
}

func (s *Service) webAuthnConfig() (WebAuthnConfig, error) {
	if s.webauthn == nil {
		return WebAuthnConfig{}, ErrWebAuthnUnavailable
	}
	return *s.webauthn, nil
}

func newChallenge() ([]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("identity: generate challenge: %w", err)
	}
	return buf, nil
}

func userVerificationRequirement(cfg WebAuthnConfig) string {
	if cfg.RequireUserVerification {
		return "required"
	}
	return "preferred"
}

// StartRegistration issues a challenge bound to the authenticated user.
func (s *Service) StartRegistration(ctx context.Context, p Principal, label string) (RegistrationOptions, error) {
	cfg, err := s.webAuthnConfig()
	if err != nil {
		return RegistrationOptions{}, err
	}
	challenge, err := newChallenge()
	if err != nil {
		return RegistrationOptions{}, err
	}
	email, err := s.store.emailOf(ctx, p.UserID)
	if err != nil {
		return RegistrationOptions{}, err
	}
	expires := s.now().UTC().Add(webauthnChallengeTTL)
	challengeID, err := s.store.insertChallenge(ctx, "register", &p.UserID, challenge, expires)
	if err != nil {
		return RegistrationOptions{}, err
	}

	existing, err := s.store.credentialsOf(ctx, p.UserID)
	if err != nil {
		return RegistrationOptions{}, err
	}
	exclude := make([]CredentialDescriptor, 0, len(existing))
	for _, c := range existing {
		exclude = append(exclude, CredentialDescriptor{
			Type:       "public-key",
			ID:         base64.RawURLEncoding.EncodeToString(c.CredentialID),
			Transports: c.Transports,
		})
	}

	return RegistrationOptions{
		ChallengeID: challengeID,
		PublicKey: PublicKeyOptions{
			Challenge: base64.RawURLEncoding.EncodeToString(challenge),
			RP:        RelyingParty{ID: cfg.RPID, Name: cfg.RPName},
			User: UserEntity{
				// The user handle is the account id, not the email: it must be
				// opaque and stable, because it is stored on the authenticator.
				ID:          base64.RawURLEncoding.EncodeToString([]byte(p.UserID)),
				Name:        email,
				DisplayName: email,
			},
			PubKeyCredParams: []CredentialParameter{
				{Type: "public-key", Alg: coseAlgES256},
				{Type: "public-key", Alg: coseAlgRS256},
			},
			Timeout:     int(webauthnChallengeTTL / time.Millisecond),
			Attestation: "none",
			AuthenticatorSelection: AuthenticatorSelection{
				ResidentKey:      "preferred",
				UserVerification: userVerificationRequirement(cfg),
			},
			ExcludeCredentials: exclude,
		},
	}, nil
}

// FinishRegistration verifies the attestation and stores the credential.
func (s *Service) FinishRegistration(ctx context.Context, p Principal, challengeID string,
	in RegistrationResponse, label string, requestID string) (Credential, error) {
	cfg, err := s.webAuthnConfig()
	if err != nil {
		return Credential{}, err
	}
	if challengeID == "" {
		return Credential{}, fmt.Errorf("%w: challenge id is required", ErrInvalidInput)
	}
	kind, boundUser, challenge, err := s.store.consumeChallenge(ctx, challengeID)
	if err != nil {
		return Credential{}, err
	}
	if kind != "register" {
		return Credential{}, fmt.Errorf("%w: challenge is not a registration challenge", ErrInvalidAssertion)
	}
	// The challenge must have been issued to this account: otherwise one user
	// could finish a ceremony another user started.
	if boundUser == nil || *boundUser != p.UserID {
		return Credential{}, fmt.Errorf("%w: challenge belongs to another account", ErrInvalidAssertion)
	}
	verified, err := cfg.verifyRegistration(challenge, in)
	if err != nil {
		return Credential{}, err
	}
	if label == "" {
		label = "Passkey"
	}
	stored, err := s.store.insertCredential(ctx, p.UserID, verified, truncateString(label, 64))
	if err != nil {
		return Credential{}, err
	}
	s.auditEvent(ctx, "user", p.UserID, "passkey.registered", "webauthn_credential", stored.ID, requestID)
	return toCredential(stored), nil
}

// StartLogin issues a challenge for authentication. The email is optional: when
// absent the challenge is unbound and the credential id in the assertion decides
// which account is logging in (discoverable credentials).
func (s *Service) StartLogin(ctx context.Context, email string) (LoginOptions, error) {
	cfg, err := s.webAuthnConfig()
	if err != nil {
		return LoginOptions{}, err
	}
	challenge, err := newChallenge()
	if err != nil {
		return LoginOptions{}, err
	}

	var userID *string
	var allow []CredentialDescriptor
	if email != "" {
		normalized, err := normalizeEmail(email)
		if err != nil {
			return LoginOptions{}, ErrInvalidCredentials
		}
		user, _, err := s.store.userByEmail(ctx, normalized)
		if errors.Is(err, ErrInvalidCredentials) {
			// Do not reveal whether the address exists: issue an unbound
			// challenge and let the assertion fail later.
			user = User{}
		} else if err != nil {
			return LoginOptions{}, err
		}
		if user.ID != "" {
			userID = &user.ID
			creds, err := s.store.credentialsOf(ctx, user.ID)
			if err != nil {
				return LoginOptions{}, err
			}
			for _, c := range creds {
				allow = append(allow, CredentialDescriptor{
					Type:       "public-key",
					ID:         base64.RawURLEncoding.EncodeToString(c.CredentialID),
					Transports: c.Transports,
				})
			}
		}
	}

	expires := s.now().UTC().Add(webauthnChallengeTTL)
	challengeID, err := s.store.insertChallenge(ctx, "login", userID, challenge, expires)
	if err != nil {
		return LoginOptions{}, err
	}
	return LoginOptions{
		ChallengeID: challengeID,
		PublicKey: LoginPublicKey{
			Challenge:        base64.RawURLEncoding.EncodeToString(challenge),
			RPID:             cfg.RPID,
			Timeout:          int(webauthnChallengeTTL / time.Millisecond),
			UserVerification: userVerificationRequirement(cfg),
			AllowCredentials: allow,
		},
	}, nil
}

// FinishLogin verifies an assertion and, on success, issues a session.
func (s *Service) FinishLogin(ctx context.Context, challengeID string, in AssertionResponse,
	d DeviceInfo, requestID string) (*Result, error) {
	cfg, err := s.webAuthnConfig()
	if err != nil {
		return nil, err
	}
	if challengeID == "" {
		return nil, fmt.Errorf("%w: challenge id is required", ErrInvalidInput)
	}

	claimedID := in.RawID
	if claimedID == "" {
		claimedID = in.ID
	}
	credID, err := decodeBase64URL(claimedID)
	if err != nil {
		return nil, err
	}
	cred, err := s.store.credentialByCredentialID(ctx, credID)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("%w: unknown or revoked credential", ErrInvalidAssertion)
	}
	if err != nil {
		return nil, err
	}

	kind, boundUser, challenge, err := s.store.consumeChallenge(ctx, challengeID)
	if err != nil {
		return nil, err
	}
	if kind != "login" {
		return nil, fmt.Errorf("%w: challenge is not a login challenge", ErrInvalidAssertion)
	}
	if boundUser != nil && *boundUser != cred.UserID {
		return nil, fmt.Errorf("%w: credential does not belong to the challenged account", ErrInvalidAssertion)
	}

	storedKey, err := parseCOSEKey(cred.PublicKey)
	if err != nil {
		return nil, err
	}
	newCount, err := cfg.verifyAssertion(challenge, storedKey, cred.SignCount, in)
	if err != nil {
		if errors.Is(err, ErrCredentialCloned) {
			s.auditEvent(ctx, "user", cred.UserID, "passkey.counter_regression", "webauthn_credential", cred.ID, requestID)
		}
		return nil, err
	}
	if err := s.store.touchCredential(ctx, cred.ID, newCount); err != nil {
		return nil, err
	}

	user, err := s.store.userByID(ctx, cred.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status != "active" {
		return nil, ErrAccountDisabled
	}
	s.auditEvent(ctx, "user", user.ID, "passkey.login", "webauthn_credential", cred.ID, requestID)
	return s.issue(ctx, user, d, "", requestID)
}

// Credentials lists the active passkeys of the authenticated account.
func (s *Service) Credentials(ctx context.Context, p Principal) ([]Credential, error) {
	creds, err := s.store.credentialsOf(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]Credential, 0, len(creds))
	for _, c := range creds {
		out = append(out, toCredential(c))
	}
	return out, nil
}

// RevokeCredential permanently disables one passkey. Revocation is soft: the row
// stays so the owner can see what was removed, and the unique credential id keeps
// the authenticator from being enrolled again by someone else.
func (s *Service) RevokeCredential(ctx context.Context, p Principal, id string, requestID string) error {
	if id == "" {
		return fmt.Errorf("%w: credential id is required", ErrInvalidInput)
	}
	if err := s.store.revokeCredential(ctx, p.UserID, id); err != nil {
		return err
	}
	s.auditEvent(ctx, "user", p.UserID, "passkey.revoked", "webauthn_credential", id, requestID)
	return nil
}

func toCredential(c storedCredential) Credential {
	return Credential{
		ID:         c.ID,
		Label:      c.Label,
		Alg:        c.Alg,
		Transports: c.Transports,
		SignCount:  c.SignCount,
		CreatedAt:  c.CreatedAt,
		LastUsedAt: c.LastUsedAt,
	}
}

// auditEvent writes an audit row outside a transaction. Audit failures are not
// allowed to fail the user action.
func (s *Service) auditEvent(ctx context.Context, actorType, actorID, action, targetType, targetID, requestID string) {
	s.store.auditTyped(ctx, actorType, actorID, action, targetType, targetID, requestID)
}
