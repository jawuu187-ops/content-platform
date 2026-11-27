// Package creator owns creator profiles, subscription tiers, subscriptions and
// the entitlement decisions that gate paid content.
//
// Money is integer minor units plus an ISO-4217 code everywhere. A subscription
// snapshots the price it was sold at, and access is granted only by an
// entitlement row that is live at the moment of the check.
package creator

import (
	"context"
	"errors"
	"time"
)

// Domain errors. The HTTP layer maps each to one stable problem code.
var (
	ErrNotFound          = errors.New("creator: not found")
	ErrInvalidInput      = errors.New("creator: invalid input")
	ErrHandleTaken       = errors.New("creator: handle already taken")
	ErrForbidden         = errors.New("creator: forbidden")
	ErrAlreadySubscribed = errors.New("creator: already subscribed")
	ErrSelfSubscription  = errors.New("creator: cannot subscribe to yourself")
	ErrTierInactive      = errors.New("creator: tier is not available")
	ErrPaymentRequired   = errors.New("creator: payment is required before activation")
	ErrAgeRequired       = errors.New("creator: age verification is required")
	ErrNotPublished      = errors.New("creator: profile is not published")
)

// Scopes an entitlement can grant.
const (
	ScopePublic     = "public"
	ScopeSubscriber = "subscriber"
	ScopePPV        = "ppv"
	ScopeAll        = "all"
)

// Reasons in an Access decision. They are stable strings so clients can branch
// on them without parsing prose.
const (
	ReasonAgeRequired = "age_verification_required"
)

// Subscription statuses, mirrored by a CHECK constraint.
const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"
	StatusPastDue   = "past_due"
)

// Profile is the public face of a creator.
type Profile struct {
	UserID      string    `json:"user_id"`
	Handle      string    `json:"handle"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	Category    string    `json:"category"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	Published   bool      `json:"published"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Tier is one subscription level.
type Tier struct {
	ID          string    `json:"id"`
	CreatorID   string    `json:"creator_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceMinor  int64     `json:"price_minor"`
	Currency    string    `json:"currency"`
	PeriodDays  int       `json:"period_days"`
	TrialDays   int       `json:"trial_days"`
	Perks       []string  `json:"perks"`
	Active      bool      `json:"active"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
}

// Subscription is a fan's membership of one creator, at the price it was sold.
type Subscription struct {
	ID                string     `json:"id"`
	FanID             string     `json:"fan_id"`
	CreatorID         string     `json:"creator_id"`
	TierID            string     `json:"tier_id"`
	Status            string     `json:"status"`
	PriceMinor        int64      `json:"price_minor"`
	Currency          string     `json:"currency"`
	PeriodStart       *time.Time `json:"current_period_start,omitempty"`
	PeriodEnd         *time.Time `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	DiscountMinor     int64      `json:"discount_minor"`
	PromotionCode     string     `json:"promotion_code,omitempty"`
	IsTrial           bool       `json:"is_trial"`
	CreatedAt         time.Time  `json:"created_at"`
}

// AdultGate answers whether an account has completed age verification. The
// creator package depends on this narrow interface, not on the verification
// package, so the two can evolve independently.
type AdultGate interface {
	AllowAdult(ctx context.Context, accountID string) (bool, error)
}

// Access is the answer to "may this viewer see this scope".
type Access struct {
	Allowed   bool       `json:"allowed"`
	Scope     string     `json:"scope"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Visibility of a post.
const (
	VisibilityPublic     = "public"
	VisibilitySubscriber = "subscriber"
	VisibilityPPV        = "ppv"
)

// Post statuses.
const (
	PostDraft     = "draft"
	PostScheduled = "scheduled"
	PostPublished = "published"
	PostRemoved   = "removed"
)

// PostMedia is one attachment on a post. Only identifiers and technical
// metadata are exposed; storage keys and URLs never are.
type PostMedia struct {
	MediaID     string `json:"media_id"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// Post is a piece of creator content.
type Post struct {
	ID          string      `json:"id"`
	CreatorID   string      `json:"creator_id"`
	Body        string      `json:"body"`
	Visibility  string      `json:"visibility"`
	PriceMinor  *int64      `json:"price_minor,omitempty"`
	Currency    string      `json:"currency,omitempty"`
	Status      string      `json:"status"`
	PublishedAt *time.Time  `json:"published_at,omitempty"`
	ScheduledAt *time.Time  `json:"scheduled_at,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	Media       []PostMedia `json:"media"`
	Locked      bool        `json:"locked"`
	Likes       int         `json:"likes"`
	Liked       bool        `json:"liked"`
	Comments    int         `json:"comments"`
	Bookmarked  bool        `json:"bookmarked"`
}

// Comment is one fan or creator reply on a post.
type Comment struct {
	ID         string    `json:"id"`
	PostID     string    `json:"post_id"`
	AuthorID   string    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	Mine       bool      `json:"mine"`
}

// Unlock is a one-off grant that opens a single PPV post.
type Unlock struct {
	ID        string     `json:"id"`
	PostID    string     `json:"post_id"`
	Source    string     `json:"source"`
	GrantedAt time.Time  `json:"granted_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
