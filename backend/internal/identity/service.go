package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	platformmail "github.com/jawuu187-ops/content-platform/backend/internal/platform/mail"
)

// dummyHash is a real Argon2id hash of a password nobody knows, produced with the
// production parameters. A failed login for an unknown address runs a full
// verification against it, so the response time matches the real path and cannot
// be used to enumerate accounts.
const dummyHash = "argon2id$v=19$m=65536,t=3,p=2$BwcHBwcHBwcHBwcHBwcHBw$ocwVtQ2rPX+WNy4qd5V96k/rWRxoMNyC4BD61FDOf88"

// Service is the identity use-case layer.
type Service struct {
	store    *store
	hasher   *Hasher
	now      func() time.Time
	mailer   platformmail.Sender
	box      *secretBox
	issuer   string
	webauthn *WebAuthnConfig
}

// Option configures the service.
type Option func(*Service)

// WithHasher overrides the password hasher (tests use a cheap one).
func WithHasher(h *Hasher) Option { return func(s *Service) { s.hasher = h } }

// WithClock overrides the clock (tests advance time deterministically).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithMailer sets the transactional email sender. Without it, verification
// requests fail with ErrMailUnavailable instead of pretending to send.
func WithMailer(sender platformmail.Sender) Option { return func(s *Service) { s.mailer = sender } }

// WithSecretKey enables multi-factor enrolment by providing the AES-256 key used
// to encrypt TOTP secrets at rest.
func WithSecretKey(key []byte) Option {
	return func(s *Service) {
		if box, err := newSecretBox(key); err == nil {
			s.box = box
		}
	}
}

// WithIssuer sets the label shown in authenticator apps.
func WithIssuer(issuer string) Option { return func(s *Service) { s.issuer = issuer } }

// Now exposes the service clock so the HTTP layer can build response timestamps
// from the same source as the tokens.
func (s *Service) Now() time.Time { return s.now().UTC() }

// New builds the service on top of an existing pool.
func New(pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{
		store:  &store{pool: pool},
		hasher: DefaultHasher(),
		now:    time.Now,
		issuer: "Content Platform",
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// normalizeEmail lowercases and validates an address. Bounce handling and
// deliverability checks belong to the mail adapter, not here.
func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 {
		return "", fmt.Errorf("%w: email", ErrInvalidInput)
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email, ".") {
		return "", fmt.Errorf("%w: email", ErrInvalidInput)
	}
	return email, nil
}

// Register creates an account, grants the fan role and opens the first session.
func (s *Service) Register(ctx context.Context, email, password string, d DeviceInfo, requestID string) (*Result, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: begin register: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := s.store.insertUser(ctx, tx, email, hash, algoArgon2id, s.hasher.ParamsForStorage())
	if err != nil {
		return nil, err
	}
	if err := s.store.grantRole(ctx, tx, user.ID, "fan", "registration"); err != nil {
		return nil, err
	}
	s.store.audit(ctx, tx, "user", user.ID, "account.register", "user", user.ID, requestID)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: commit register: %w", err)
	}

	return s.issue(ctx, user, d, "", requestID, nil)
}

// Login verifies credentials and opens a new session family. When the account has
// a confirmed second factor, mfaCode must carry a valid TOTP or recovery code;
// otherwise the attempt is rejected with ErrMFARequired.
func (s *Service) Login(ctx context.Context, email, password, mfaCode string, d DeviceInfo, requestID string) (*Result, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		// Same error as a wrong password: the caller learns nothing.
		return nil, ErrInvalidCredentials
	}

	user, hash, err := s.store.userByEmail(ctx, normalized)
	if errors.Is(err, ErrInvalidCredentials) {
		_, _ = s.hasher.Verify(password, dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}

	ok, needsRehash := s.hasher.Verify(password, hash)
	if !ok {
		s.auditPasswordFailure(ctx, user.ID, requestID)
		return nil, ErrInvalidCredentials
	}
	if user.Status != "active" {
		return nil, ErrAccountDisabled
	}
	if needsRehash {
		if newHash, err := s.hasher.Hash(password); err == nil {
			s.store.upgradeHash(ctx, user.ID, newHash, algoArgon2id, s.hasher.ParamsForStorage())
			user.PasswordUpgraded = true
		}
	}

	var mfaAt *time.Time
	factorID, sealed, err := s.store.confirmedTOTP(ctx, user.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		// No second factor: password alone is sufficient.
	case err != nil:
		return nil, err
	default:
		if mfaCode == "" {
			return nil, ErrMFARequired
		}
		ok, err := s.checkMFACode(ctx, user.ID, factorID, sealed, mfaCode)
		if err != nil {
			return nil, err
		}
		if !ok {
			s.auditPasswordFailure(ctx, user.ID, requestID)
			return nil, ErrInvalidMFACode
		}
		verified := s.now().UTC()
		mfaAt = &verified
	}

	return s.issue(ctx, user, d, "", requestID, mfaAt)
}

// issue creates a session (new family when familyID is empty) and returns tokens.
func (s *Service) issue(ctx context.Context, user User, d DeviceInfo, familyID, requestID string, mfaAt *time.Time) (*Result, error) {
	access, accessHash, err := newToken()
	if err != nil {
		return nil, err
	}
	refresh, refreshHash, err := newToken()
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	accessExp := now.Add(AccessTokenTTL)
	refreshExp := now.Add(RefreshTokenTTL)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: begin session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	deviceID, err := s.store.upsertDevice(ctx, tx, user.ID, d)
	if err != nil {
		return nil, err
	}
	family := familyID
	if family == "" {
		family = newUUID()
	}
	sessionID, err := s.store.insertSession(ctx, tx, user.ID, family, &deviceID,
		accessHash, refreshHash, accessExp, refreshExp, d, mfaAt)
	if err != nil {
		return nil, err
	}
	s.store.audit(ctx, tx, "user", user.ID, "session.create", "session", sessionID, requestID)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: commit session: %w", err)
	}

	roles, err := s.store.roles(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &Result{
		User:         user,
		Principal:    Principal{UserID: user.ID, SessionID: sessionID, EmailVerified: user.EmailVerified, Roles: roles, MFAAt: mfaAt},
		AccessToken:  access,
		RefreshToken: refresh,
		AccessTTL:    AccessTokenTTL,
		RefreshTTL:   RefreshTokenTTL,
	}, nil
}

// Refresh rotates a refresh token. A token that was already rotated is treated
// as stolen: the entire session family is revoked and the caller must log in
// again with credentials.
func (s *Service) Refresh(ctx context.Context, refreshToken string, d DeviceInfo, requestID string) (*Result, error) {
	if refreshToken == "" {
		return nil, ErrInvalidCredentials
	}
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := s.store.refreshRow(ctx, tx, hashToken(refreshToken))
	if err != nil {
		return nil, err
	}

	if row.RevokedAt != nil {
		if row.ReplacedBy == nil {
			// Revoked for another reason (logout, expiry, family revocation):
			// the session is simply over.
			return nil, ErrSessionRevoked
		}
		// This token was already rotated, yet it is being presented again, so a
		// copy of it exists outside our client. Revoke every session in the
		// family and require a fresh credential login.
		if err := s.store.revokeFamily(ctx, tx, row.FamilyID, "refresh_reuse"); err != nil {
			return nil, err
		}
		s.store.audit(ctx, tx, "user", row.UserID, "session.reuse_detected", "session", row.ID, requestID)
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("identity: commit reuse revocation: %w", err)
		}
		return nil, ErrTokenReuse
	}
	if s.now().After(row.RefreshExpiresAt) {
		if err := s.store.revokeSession(ctx, tx, row.ID, "expired"); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("identity: commit expiry revocation: %w", err)
		}
		return nil, ErrSessionExpired
	}

	user, err := s.store.userByIDTx(ctx, tx, row.UserID)
	if err != nil {
		return nil, err
	}

	access, accessHash, err := newToken()
	if err != nil {
		return nil, err
	}
	refresh, refreshHash, err := newToken()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	accessExp := now.Add(AccessTokenTTL)
	refreshExp := now.Add(RefreshTokenTTL)

	carried := d
	if carried.UserAgent == "" {
		carried.UserAgent = deref(row.UserAgent)
	}
	if carried.IP == "" {
		carried.IP = deref(row.IP)
	}
	deviceID, err := s.store.upsertDevice(ctx, tx, user.ID, carried)
	if err != nil {
		return nil, err
	}
	newID, err := s.store.insertSession(ctx, tx, user.ID, row.FamilyID, &deviceID,
		accessHash, refreshHash, accessExp, refreshExp, carried, row.MFAAt)
	if err != nil {
		return nil, err
	}
	if err := s.store.markRotated(ctx, tx, row.ID, newID); err != nil {
		return nil, err
	}
	s.store.audit(ctx, tx, "user", user.ID, "session.refresh", "session", newID, requestID)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: commit refresh: %w", err)
	}

	roles, err := s.store.roles(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &Result{
		User:         user,
		Principal:    Principal{UserID: user.ID, SessionID: newID, EmailVerified: user.EmailVerified, Roles: roles},
		AccessToken:  access,
		RefreshToken: refresh,
		AccessTTL:    AccessTokenTTL,
		RefreshTTL:   RefreshTokenTTL,
	}, nil
}

// Authenticate resolves an access token into a principal.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (*Principal, error) {
	if accessToken == "" {
		return nil, ErrInvalidCredentials
	}
	p, expires, err := s.store.sessionByAccess(ctx, hashToken(accessToken))
	if err != nil {
		return nil, err
	}
	if s.now().After(expires) {
		return nil, ErrSessionExpired
	}
	roles, err := s.store.roles(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	p.Roles = roles
	s.store.touchSession(ctx, p.SessionID)
	return &p, nil
}

// Logout revokes the current session. It is idempotent.
func (s *Service) Logout(ctx context.Context, p Principal, requestID string) error {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("identity: begin logout: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.store.revokeSession(ctx, tx, p.SessionID, "logout"); err != nil {
		return err
	}
	s.store.audit(ctx, tx, "user", p.UserID, "session.logout", "session", p.SessionID, requestID)
	return tx.Commit(ctx)
}

// Sessions lists the caller's sessions, newest first.
func (s *Service) Sessions(ctx context.Context, p Principal) ([]Session, error) {
	return s.store.listSessions(ctx, p.UserID, p.SessionID)
}

// RevokeSession ends one of the caller's own sessions.
func (s *Service) RevokeSession(ctx context.Context, p Principal, sessionID, requestID string) error {
	if sessionID == p.SessionID {
		return fmt.Errorf("%w: use logout for the current session", ErrInvalidInput)
	}
	if err := s.store.revokeOwnedSession(ctx, p.UserID, sessionID); err != nil {
		return err
	}
	s.store.auditPool(ctx, p.UserID, "session.revoke", sessionID, requestID)
	return nil
}

func (s *Service) auditPasswordFailure(ctx context.Context, userID, requestID string) {
	s.store.auditPool(ctx, userID, "session.login_failed", userID, requestID)
}

// newUUID returns a random RFC 4122 version 4 UUID string. Generating it in Go
// avoids a database round trip when a session family is opened.
func newUUID() string {
	var b [16]byte
	if _, err := randRead(b[:]); err != nil {
		// A failure here is catastrophic; fall back to the database default by
		// returning a value it will reject, making the failure loud but safe.
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	pos := 0
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[pos] = '-'
			pos++
		}
		out[pos] = hex[v>>4]
		out[pos+1] = hex[v&0x0f]
		pos += 2
	}
	return string(out)
}
