package identity

import (
	"errors"
	"time"
)

// Sentinel errors. Transport layers map these to status codes; nothing else in
// the codebase compares error strings.
var (
	ErrEmailTaken         = errors.New("identity: email already registered")
	ErrInvalidCredentials = errors.New("identity: invalid credentials")
	ErrAccountDisabled    = errors.New("identity: account is not active")
	ErrSessionRevoked     = errors.New("identity: session revoked")
	ErrSessionExpired     = errors.New("identity: session expired")
	ErrTokenReuse         = errors.New("identity: refresh token reuse detected")
	ErrNotFound           = errors.New("identity: not found")
	ErrForbidden          = errors.New("identity: forbidden")
	ErrInvalidInput       = errors.New("identity: invalid input")
)

// User is the safe projection of an account. It never carries password material.
type User struct {
	ID               string
	Email            string
	Status           string
	EmailVerified    bool
	IsCreator        bool
	CreatedAt        time.Time
	PasswordUpgraded bool
}

// Principal is the authenticated caller attached to a request.
type Principal struct {
	UserID        string
	SessionID     string
	EmailVerified bool
	Roles         []string
	// MFAAt is when this session last proved a second factor. Nil means the
	// session never did; reviewers require a recent value.
	MFAAt *time.Time
}

// HasRole reports whether the principal holds a role.
func (p Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole reports whether the principal holds at least one of the roles.
func (p Principal) HasAnyRole(roles ...string) bool {
	for _, want := range roles {
		if p.HasRole(want) {
			return true
		}
	}
	return false
}

// Session is the client-visible view of a login session.
type Session struct {
	ID         string     `json:"id"`
	DeviceID   *string    `json:"device_id,omitempty"`
	DeviceName *string    `json:"device_name,omitempty"`
	Platform   *string    `json:"platform,omitempty"`
	UserAgent  *string    `json:"user_agent,omitempty"`
	IP         *string    `json:"ip,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt time.Time  `json:"last_used_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	Current    bool       `json:"current"`
}

// DeviceInfo is supplied by the client on login. Only the fingerprint is used
// for grouping; the rest is display data the user can see and act on.
type DeviceInfo struct {
	Fingerprint string
	Platform    string
	Name        string
	UserAgent   string
	IP          string
}

// Result is returned by register, login and refresh.
type Result struct {
	User         User
	Principal    Principal
	AccessToken  string
	RefreshToken string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
}
