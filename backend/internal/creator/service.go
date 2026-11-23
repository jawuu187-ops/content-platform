package creator

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// ProfileInput is a create-or-update payload. Update treats empty strings as
// "leave unchanged" for the nullable presentation fields, which is what a PATCH
// means here.
type ProfileInput struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	Category    string `json:"category"`
	AvatarURL   string `json:"avatar_url"`
	CoverURL    string `json:"cover_url"`
	Publish     *bool  `json:"published"`
}

// TierInput is used for both create and update; pointer fields mean "not supplied".
type TierInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	PriceMinor  *int64   `json:"price_minor"`
	Currency    string   `json:"currency"`
	PeriodDays  *int     `json:"period_days"`
	Perks       []string `json:"perks"`
	SortOrder   *int     `json:"sort_order"`
	Active      *bool    `json:"active"`
}

var (
	handlePattern = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)
	// Handles that would collide with platform routes or imply authority.
	reservedHandles = map[string]bool{
		"admin": true, "api": true, "www": true, "help": true, "support": true,
		"root": true, "me": true, "settings": true, "terms": true, "privacy": true,
		"creator": true, "creators": true, "login": true, "register": true, "about": true,
	}
	allowedCategories = map[string]bool{
		"art": true, "music": true, "fitness": true, "lifestyle": true,
		"photography": true, "writing": true, "education": true, "other": true,
	}
	allowedCurrencies = map[string]bool{"USD": true, "EUR": true, "GBP": true, "CAD": true, "AUD": true}
)

// NormalizeHandle lowercases and trims a handle. It is exported so the HTTP layer
// can echo the canonical form back.
func NormalizeHandle(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// publishValue is the effective publish flag when creating a profile.
func (in ProfileInput) publishValue() bool {
	return in.Publish != nil && *in.Publish
}

func validateProfile(in ProfileInput, creating bool) (ProfileInput, error) {
	in.Handle = NormalizeHandle(in.Handle)
	if creating {
		if !handlePattern.MatchString(in.Handle) {
			return in, fmt.Errorf("%w: handle must be 3-30 characters of a-z, 0-9 or underscore", ErrInvalidInput)
		}
		if reservedHandles[in.Handle] {
			return in, fmt.Errorf("%w: that handle is reserved", ErrInvalidInput)
		}
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if creating && in.DisplayName == "" {
		return in, fmt.Errorf("%w: display name is required", ErrInvalidInput)
	}
	if len([]rune(in.DisplayName)) > 60 {
		return in, fmt.Errorf("%w: display name is too long", ErrInvalidInput)
	}
	in.Bio = strings.TrimSpace(in.Bio)
	if len([]rune(in.Bio)) > 600 {
		return in, fmt.Errorf("%w: bio is too long", ErrInvalidInput)
	}
	if in.Category == "" {
		in.Category = "other"
	}
	if !allowedCategories[in.Category] {
		return in, fmt.Errorf("%w: unknown category", ErrInvalidInput)
	}
	for _, u := range []string{in.AvatarURL, in.CoverURL} {
		if u != "" && !strings.HasPrefix(u, "https://") {
			return in, fmt.Errorf("%w: image URLs must be https", ErrInvalidInput)
		}
	}
	return in, nil
}

func validateTier(in TierInput, creating bool) (TierInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if creating {
		if in.Name == "" || len([]rune(in.Name)) > 40 {
			return in, fmt.Errorf("%w: tier name must be 1-40 characters", ErrInvalidInput)
		}
		if in.PriceMinor == nil {
			return in, fmt.Errorf("%w: price_minor is required", ErrInvalidInput)
		}
		if in.Currency == "" {
			return in, fmt.Errorf("%w: currency is required", ErrInvalidInput)
		}
	}
	if in.PriceMinor != nil && (*in.PriceMinor < 100 || *in.PriceMinor > 1_000_000) {
		// Bounds: at least 1.00 in minor units, at most 10,000.00.
		return in, fmt.Errorf("%w: price must be between 100 and 1000000 minor units", ErrInvalidInput)
	}
	if in.Currency != "" && !allowedCurrencies[strings.ToUpper(in.Currency)] {
		return in, fmt.Errorf("%w: unsupported currency", ErrInvalidInput)
	}
	in.Currency = strings.ToUpper(in.Currency)
	if in.PeriodDays != nil && (*in.PeriodDays < 1 || *in.PeriodDays > 366) {
		return in, fmt.Errorf("%w: period_days must be between 1 and 366", ErrInvalidInput)
	}
	if in.Perks == nil && creating {
		// The column is NOT NULL; an omitted perk list on create means "none".
		in.Perks = []string{}
	}
	if len(in.Perks) > 10 {
		return in, fmt.Errorf("%w: at most 10 perks", ErrInvalidInput)
	}
	for i, p := range in.Perks {
		p = strings.TrimSpace(p)
		if p == "" || len([]rune(p)) > 80 {
			return in, fmt.Errorf("%w: perk %d must be 1-80 characters", ErrInvalidInput, i+1)
		}
		in.Perks[i] = p
	}
	in.Description = strings.TrimSpace(in.Description)
	if len([]rune(in.Description)) > 300 {
		return in, fmt.Errorf("%w: description is too long", ErrInvalidInput)
	}
	return in, nil
}

// CreateProfile turns the caller into a creator. The creator role is granted by
// the identity service after this succeeds (see the HTTP layer), because role
// administration lives there.
func (s *Service) CreateProfile(ctx context.Context, p identity.Principal, in ProfileInput, requestID string) (Profile, error) {
	in, err := validateProfile(in, true)
	if err != nil {
		return Profile{}, err
	}
	profile, err := s.insertProfile(ctx, p.UserID, in)
	if err != nil {
		return Profile{}, err
	}
	s.audit(ctx, p.UserID, "creator.profile_created", "creator_profile", profile.UserID, requestID)
	return profile, nil
}

// UpdateProfile edits the caller's own profile.
func (s *Service) UpdateProfile(ctx context.Context, p identity.Principal, in ProfileInput, requestID string) (Profile, error) {
	in, err := validateProfile(in, false)
	if err != nil {
		return Profile{}, err
	}
	profile, err := s.updateProfile(ctx, p.UserID, in)
	if err != nil {
		return Profile{}, err
	}
	s.audit(ctx, p.UserID, "creator.profile_updated", "creator_profile", profile.UserID, requestID)
	return profile, nil
}

// PublicProfile returns a published profile and its active tiers. An unpublished
// profile is reported as not found, so a draft cannot be enumerated.
func (s *Service) PublicProfile(ctx context.Context, handle string) (Profile, []Tier, error) {
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if errors.Is(err, ErrNotFound) {
		return Profile{}, nil, ErrNotFound
	}
	if err != nil {
		return Profile{}, nil, err
	}
	if !profile.Published {
		return Profile{}, nil, ErrNotFound
	}
	tiers, err := s.tiersOf(ctx, profile.UserID, true)
	if err != nil {
		return Profile{}, nil, err
	}
	return profile, tiers, nil
}

// ProfileByUserID is used by endpoints that already know the account.
func (s *Service) ProfileByUserID(ctx context.Context, userID string) (Profile, error) {
	var handle string
	err := s.pool.QueryRow(ctx, `SELECT handle::text FROM creator.profiles WHERE user_id = $1::uuid`, userID).Scan(&handle)
	if err != nil {
		return Profile{}, ErrNotFound
	}
	return s.profileByHandle(ctx, handle)
}

// CreateTier adds a tier to the caller's profile.
func (s *Service) CreateTier(ctx context.Context, p identity.Principal, in TierInput, requestID string) (Tier, error) {
	in, err := validateTier(in, true)
	if err != nil {
		return Tier{}, err
	}
	tier, err := s.insertTier(ctx, p.UserID, in)
	if err != nil {
		return Tier{}, err
	}
	s.audit(ctx, p.UserID, "creator.tier_created", "creator_tier", tier.ID, requestID)
	return tier, nil
}

// UpdateTier edits a tier owned by the caller.
func (s *Service) UpdateTier(ctx context.Context, p identity.Principal, tierID string, in TierInput, requestID string) (Tier, error) {
	in, err := validateTier(in, false)
	if err != nil {
		return Tier{}, err
	}
	tier, err := s.updateTier(ctx, p.UserID, tierID, in)
	if err != nil {
		return Tier{}, err
	}
	s.audit(ctx, p.UserID, "creator.tier_updated", "creator_tier", tier.ID, requestID)
	return tier, nil
}

// DeactivateTier retires a tier without deleting it, because existing
// subscriptions still reference it and the price they were sold at.
func (s *Service) DeactivateTier(ctx context.Context, p identity.Principal, tierID, requestID string) error {
	inactive := false
	tier, err := s.updateTier(ctx, p.UserID, tierID, TierInput{Active: &inactive})
	if err != nil {
		return err
	}
	s.audit(ctx, p.UserID, "creator.tier_deactivated", "creator_tier", tier.ID, requestID)
	return nil
}

// TiersOf lists a creator's active tiers by handle.
func (s *Service) TiersOf(ctx context.Context, handle string) ([]Tier, error) {
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return nil, err
	}
	if !profile.Published {
		return nil, ErrNotFound
	}
	return s.tiersOf(ctx, profile.UserID, true)
}

// Subscribe starts a membership. Without a payment provider the subscription is
// created as pending and the caller is told payment is required; with the
// development fake-payments switch it is activated immediately, which is how the
// entitlement path is exercised in tests.
func (s *Service) Subscribe(ctx context.Context, p identity.Principal, handle, tierID, requestID string) (Subscription, error) {
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return Subscription{}, err
	}
	if !profile.Published {
		return Subscription{}, ErrNotFound
	}
	if profile.UserID == p.UserID {
		return Subscription{}, ErrSelfSubscription
	}
	tier, err := s.tierByID(ctx, tierID)
	if err != nil {
		return Subscription{}, err
	}
	if tier.CreatorID != profile.UserID {
		// A tier from another creator is a 404, not a 400: it does not exist here.
		return Subscription{}, ErrNotFound
	}
	if !tier.Active {
		return Subscription{}, ErrTierInactive
	}

	sub, err := s.insertSubscription(ctx, p.UserID, tier, StatusPending)
	if err != nil {
		return Subscription{}, err
	}
	s.audit(ctx, p.UserID, "creator.subscription_started", "creator_subscription", sub.ID, requestID)

	if !s.fakePayments {
		return sub, nil
	}
	active, err := s.activate(ctx, sub.ID, s.now().UTC(), tier.PeriodDays)
	if err != nil {
		return Subscription{}, err
	}
	s.audit(ctx, p.UserID, "creator.subscription_activated", "creator_subscription", active.ID, requestID)
	return active, nil
}

// Activate is the entry point a payment provider callback will use. It is
// exported so the webhook layer in P4 can call it without duplicating the
// transaction that grants access.
func (s *Service) Activate(ctx context.Context, subscriptionID string, requestID string) (Subscription, error) {
	sub, err := s.subscriptionByID(ctx, subscriptionID)
	if err != nil {
		return Subscription{}, err
	}
	tier, err := s.tierByID(ctx, sub.TierID)
	if err != nil {
		return Subscription{}, err
	}
	active, err := s.activate(ctx, sub.ID, s.now().UTC(), tier.PeriodDays)
	if err != nil {
		return Subscription{}, err
	}
	s.audit(ctx, sub.FanID, "creator.subscription_activated", "creator_subscription", active.ID, requestID)
	return active, nil
}

// Subscriptions lists the caller's memberships.
func (s *Service) Subscriptions(ctx context.Context, p identity.Principal) ([]Subscription, error) {
	return s.subscriptionsOfFan(ctx, p.UserID)
}

// CancelSubscription cancels a membership owned by the caller.
func (s *Service) CancelSubscription(ctx context.Context, p identity.Principal, subscriptionID, requestID string) (Subscription, error) {
	sub, err := s.subscriptionByID(ctx, subscriptionID)
	if err != nil {
		return Subscription{}, err
	}
	// Another fan's subscription is reported as not found: its existence is not
	// the caller's business.
	if sub.FanID != p.UserID {
		return Subscription{}, ErrNotFound
	}
	out, err := s.cancel(ctx, sub)
	if err != nil {
		return Subscription{}, err
	}
	s.audit(ctx, p.UserID, "creator.subscription_cancelled", "creator_subscription", out.ID, requestID)
	return out, nil
}

// ExpireDue is the job body for period rollover. It is called from tests now and
// from a scheduler when one exists.
func (s *Service) ExpireDue(ctx context.Context) (int64, error) {
	return s.expireDue(ctx, s.now().UTC())
}

// CheckAccess decides whether a viewer may see a scope on a creator.
//
// Precedence: owner, admin, public scope, then a live entitlement, then an
// in-period subscription. The subscription check exists as a second gate: an
// entitlement row with no paid subscription behind it must not grant access, and
// a subscription that has run out must not either.
func (s *Service) CheckAccess(ctx context.Context, viewer *identity.Principal, handle, scope string) (Access, error) {
	switch scope {
	case ScopePublic, ScopeSubscriber, ScopePPV:
	default:
		return Access{}, fmt.Errorf("%w: unknown access scope", ErrInvalidInput)
	}
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return Access{}, err
	}
	isOwner := viewer != nil && viewer.UserID == profile.UserID
	isAdmin := viewer != nil && identity.HasPermission(viewer.Roles, identity.PermAdminAll)
	if !profile.Published && !isOwner && !isAdmin {
		return Access{}, ErrNotFound
	}
	if isOwner {
		return Access{Allowed: true, Scope: scope, Reason: "owner"}, nil
	}
	if isAdmin {
		return Access{Allowed: true, Scope: scope, Reason: "admin"}, nil
	}
	if scope == ScopePublic {
		return Access{Allowed: true, Scope: scope, Reason: "public"}, nil
	}
	if viewer == nil {
		return Access{Allowed: false, Scope: scope, Reason: "authentication_required"}, nil
	}
	// Age verification is checked before any entitlement, for every non-admin
	// viewer including the creator: a paid subscription is not a substitute for
	// it, and a verification that lapses closes the door again.
	if s.adult == nil {
		return Access{Allowed: false, Scope: scope, Reason: ReasonAgeRequired}, nil
	}
	adult, err := s.adult.AllowAdult(ctx, viewer.UserID)
	if err != nil {
		// Fail closed. The HTTP layer reports the error; the access decision does
		// not become "allowed" because a dependency is unwell.
		return Access{}, fmt.Errorf("creator: age verification check: %w", err)
	}
	if !adult {
		return Access{Allowed: false, Scope: scope, Reason: ReasonAgeRequired}, nil
	}

	now := s.now().UTC()
	ent, err := s.liveEntitlement(ctx, viewer.UserID, profile.UserID, scope, now)
	if err != nil {
		return Access{}, err
	}
	if ent.Allowed {
		return ent, nil
	}
	subscribed, err := s.activeSubscription(ctx, viewer.UserID, profile.UserID, now)
	if err != nil {
		return Access{}, err
	}
	if subscribed {
		return Access{Allowed: true, Scope: scope, Reason: "subscription"}, nil
	}
	return Access{Allowed: false, Scope: scope, Reason: "entitlement_required"}, nil
}

// GrantEntitlement gives a fan direct access, used by admin tooling and by the
// pay-per-view path in a later phase.
func (s *Service) GrantEntitlement(ctx context.Context, p identity.Principal, handle, fanID, scope string, ttl time.Duration, requestID string) error {
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return err
	}
	if scope != ScopeSubscriber && scope != ScopePPV && scope != ScopeAll {
		return fmt.Errorf("%w: unknown scope", ErrInvalidInput)
	}
	var expires *time.Time
	if ttl > 0 {
		t := s.now().UTC().Add(ttl)
		expires = &t
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, 'grant', $4)
		ON CONFLICT (fan_id, creator_id, scope) WHERE revoked_at IS NULL
		DO UPDATE SET expires_at = EXCLUDED.expires_at, granted_at = now()`,
		fanID, profile.UserID, scope, expires)
	if err != nil {
		return fmt.Errorf("creator: grant entitlement: %w", err)
	}
	s.audit(ctx, p.UserID, "creator.entitlement_granted", "creator_entitlement", profile.UserID, requestID)
	return nil
}
