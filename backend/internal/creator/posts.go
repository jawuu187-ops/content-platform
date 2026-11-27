package creator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// PostInput is a create-or-update payload for a post.
type PostInput struct {
	Body       string   `json:"body"`
	Visibility string   `json:"visibility"`
	PriceMinor *int64   `json:"price_minor"`
	Currency   string   `json:"currency"`
	MediaIDs   []string `json:"media_ids"`
	Publish    *bool    `json:"published"`
}

func validatePost(in PostInput, creating bool) (PostInput, error) {
	in.Body = strings.TrimSpace(in.Body)
	if len([]rune(in.Body)) > 2000 {
		return in, fmt.Errorf("%w: post body is too long", ErrInvalidInput)
	}
	if creating && in.Visibility == "" {
		in.Visibility = VisibilityPublic
	}
	switch in.Visibility {
	case "", VisibilityPublic:
		if in.PriceMinor != nil {
			return in, fmt.Errorf("%w: only a pay-per-view post carries a price", ErrInvalidInput)
		}
	case VisibilitySubscriber:
		if in.PriceMinor != nil {
			return in, fmt.Errorf("%w: only a pay-per-view post carries a price", ErrInvalidInput)
		}
	case VisibilityPPV:
		if creating && in.PriceMinor == nil {
			return in, fmt.Errorf("%w: a pay-per-view post needs a price", ErrInvalidInput)
		}
		if in.PriceMinor != nil && (*in.PriceMinor < 100 || *in.PriceMinor > 1_000_000) {
			return in, fmt.Errorf("%w: price must be between 100 and 1000000 minor units", ErrInvalidInput)
		}
		in.Currency = strings.ToUpper(in.Currency)
		if in.Currency != "" && !allowedCurrencies[in.Currency] {
			return in, fmt.Errorf("%w: unsupported currency", ErrInvalidInput)
		}
	default:
		return in, fmt.Errorf("%w: visibility must be public, subscriber or ppv", ErrInvalidInput)
	}
	if len(in.MediaIDs) > 10 {
		return in, fmt.Errorf("%w: at most 10 media items per post", ErrInvalidInput)
	}
	return in, nil
}

// CreatePost publishes (or drafts) a post with media the caller owns.
func (s *Service) CreatePost(ctx context.Context, p identity.Principal, in PostInput, requestID string) (Post, error) {
	in, err := validatePost(in, true)
	if err != nil {
		return Post{}, err
	}
	publish := in.Publish != nil && *in.Publish
	if publish && in.Visibility == VisibilityPPV && in.PriceMinor == nil {
		return Post{}, fmt.Errorf("%w: a pay-per-view post needs a price", ErrInvalidInput)
	}
	if err := s.assertMediaOwned(ctx, p.UserID, in.MediaIDs); err != nil {
		return Post{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("creator: begin create post: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var publishedAt *time.Time
	status := PostDraft
	if publish {
		now := s.now().UTC()
		publishedAt, status = &now, PostPublished
	}
	var post Post
	var price *int64
	var currency *string
	if in.Visibility == VisibilityPPV {
		price = in.PriceMinor
		c := in.Currency
		if c == "" {
			c = "USD"
		}
		currency = &c
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO creator.posts (creator_id, body, visibility, price_minor, currency, status, published_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		RETURNING id::text, creator_id::text, body, visibility, price_minor, COALESCE(currency, ''),
		          status, published_at, created_at`,
		p.UserID, in.Body, in.Visibility, price, currency, status, publishedAt,
	).Scan(&post.ID, &post.CreatorID, &post.Body, &post.Visibility, &post.PriceMinor, &post.Currency,
		&post.Status, &post.PublishedAt, &post.CreatedAt)
	if err != nil {
		return Post{}, fmt.Errorf("creator: insert post: %w", err)
	}
	if err := attachMedia(ctx, tx, post.ID, in.MediaIDs); err != nil {
		return Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, fmt.Errorf("creator: commit post: %w", err)
	}
	s.audit(ctx, p.UserID, "creator.post_created", "creator_post", post.ID, requestID)
	post.Media, _ = s.postMedia(ctx, post.ID)
	return post, nil
}

func attachMedia(ctx context.Context, tx pgx.Tx, postID string, mediaIDs []string) error {
	for i, id := range mediaIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO creator.post_media (post_id, media_id, sort_order) VALUES ($1::uuid, $2::uuid, $3)`,
			postID, id, i); err != nil {
			return fmt.Errorf("creator: attach media: %w", err)
		}
	}
	return nil
}

func (s *Service) assertMediaOwned(ctx context.Context, ownerID string, mediaIDs []string) error {
	for _, id := range mediaIDs {
		var ok bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM media.objects WHERE id = $1::uuid AND owner_id = $2::uuid AND status = 'ready')`,
			id, ownerID).Scan(&ok); err != nil {
			return fmt.Errorf("creator: check media ownership: %w", err)
		}
		if !ok {
			// Another creator's media is reported as not found.
			return fmt.Errorf("%w: media %s is not yours to attach", ErrNotFound, id)
		}
	}
	return nil
}

// UpdatePost edits the caller's post. Changing visibility to ppv requires a price.
func (s *Service) UpdatePost(ctx context.Context, p identity.Principal, postID string, in PostInput, requestID string) (Post, error) {
	in, err := validatePost(in, false)
	if err != nil {
		return Post{}, err
	}
	existing, err := s.postByID(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	if existing.CreatorID != p.UserID {
		return Post{}, ErrNotFound
	}
	visibility := in.Visibility
	if visibility == "" {
		visibility = existing.Visibility
	}
	var price *int64
	var currency *string
	if visibility == VisibilityPPV {
		price = in.PriceMinor
		if price == nil {
			price = existing.PriceMinor
		}
		if price == nil {
			return Post{}, fmt.Errorf("%w: a pay-per-view post needs a price", ErrInvalidInput)
		}
		c := in.Currency
		if c == "" {
			c = existing.Currency
		}
		if c == "" {
			c = "USD"
		}
		currency = &c
	}
	publishNow := in.Publish != nil && *in.Publish
	if err := s.assertMediaOwned(ctx, p.UserID, in.MediaIDs); err != nil {
		return Post{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Post{}, fmt.Errorf("creator: begin update post: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var post Post
	var publishedAt *time.Time
	err = tx.QueryRow(ctx, `
		UPDATE creator.posts
		SET body = CASE WHEN $3 = '' THEN body ELSE $3 END,
		    visibility = CASE WHEN $4 = '' THEN visibility ELSE $4 END,
		    price_minor = $5,
		    currency = $6,
		    published_at = CASE WHEN $7 THEN COALESCE(published_at, now()) ELSE published_at END,
		    status = CASE WHEN $7 THEN 'published' ELSE status END,
		    updated_at = now()
		WHERE id = $1::uuid AND creator_id = $2::uuid
		RETURNING id::text, creator_id::text, body, visibility, price_minor, COALESCE(currency, ''),
		          status, published_at, created_at`,
		postID, p.UserID, in.Body, visibility, price, currency, publishNow,
	).Scan(&post.ID, &post.CreatorID, &post.Body, &post.Visibility, &post.PriceMinor, &post.Currency,
		&post.Status, &publishedAt, &post.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, fmt.Errorf("creator: update post: %w", err)
	}
	post.PublishedAt = publishedAt
	if len(in.MediaIDs) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM creator.post_media WHERE post_id = $1::uuid`, post.ID); err != nil {
			return Post{}, fmt.Errorf("creator: clear post media: %w", err)
		}
		if err := attachMedia(ctx, tx, post.ID, in.MediaIDs); err != nil {
			return Post{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, fmt.Errorf("creator: commit post update: %w", err)
	}
	s.audit(ctx, p.UserID, "creator.post_updated", "creator_post", post.ID, requestID)
	post.Media, _ = s.postMedia(ctx, post.ID)
	return post, nil
}

// RemovePost hides a post without deleting its history.
func (s *Service) RemovePost(ctx context.Context, p identity.Principal, postID, requestID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE creator.posts SET status = 'removed', updated_at = now()
		WHERE id = $1::uuid AND creator_id = $2::uuid`, postID, p.UserID)
	if err != nil {
		return fmt.Errorf("creator: remove post: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.audit(ctx, p.UserID, "creator.post_removed", "creator_post", postID, requestID)
	return nil
}

func (s *Service) postByID(ctx context.Context, postID string) (Post, error) {
	var p Post
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, creator_id::text, body, visibility, price_minor, COALESCE(currency, ''),
		       status, published_at, created_at
		FROM creator.posts WHERE id = $1::uuid`, postID).
		Scan(&p.ID, &p.CreatorID, &p.Body, &p.Visibility, &p.PriceMinor, &p.Currency, &p.Status,
			&p.PublishedAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, fmt.Errorf("creator: load post: %w", err)
	}
	p.Media, err = s.postMedia(ctx, p.ID)
	if err != nil {
		return Post{}, err
	}
	return p, nil
}

func (s *Service) postMedia(ctx context.Context, postID string) ([]PostMedia, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id::text, m.kind, m.content_type, m.byte_size, COALESCE(m.width, 0), COALESCE(m.height, 0)
		FROM creator.post_media pm JOIN media.objects m ON m.id = pm.media_id
		WHERE pm.post_id = $1::uuid AND m.status = 'ready'
		ORDER BY pm.sort_order`, postID)
	if err != nil {
		return nil, fmt.Errorf("creator: list post media: %w", err)
	}
	defer rows.Close()
	var out []PostMedia
	for rows.Next() {
		var m PostMedia
		if err := rows.Scan(&m.MediaID, &m.Kind, &m.ContentType, &m.ByteSize, &m.Width, &m.Height); err != nil {
			return nil, fmt.Errorf("creator: scan post media: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Posts lists a creator's published posts. Visibility is reported per post and
// the entitlement check that matters runs at delivery; a locked post carries no
// usable media URL.
func (s *Service) Posts(ctx context.Context, p *identity.Principal, handle string) ([]Post, error) {
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return nil, err
	}
	isOwner := p != nil && p.UserID == profile.UserID
	if !profile.Published && !isOwner {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, creator_id::text, body, visibility, price_minor, COALESCE(currency, ''),
		       status, published_at, created_at
		FROM creator.posts
		WHERE creator_id = $1::uuid AND status = 'published'
		ORDER BY published_at DESC
		LIMIT 50`, profile.UserID)
	if err != nil {
		return nil, fmt.Errorf("creator: list posts: %w", err)
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var post Post
		if err := rows.Scan(&post.ID, &post.CreatorID, &post.Body, &post.Visibility, &post.PriceMinor,
			&post.Currency, &post.Status, &post.PublishedAt, &post.CreatedAt); err != nil {
			return nil, fmt.Errorf("creator: scan post: %w", err)
		}
		post.Media, err = s.postMedia(ctx, post.ID)
		if err != nil {
			return nil, err
		}
		if post.Visibility != VisibilityPublic {
			decision, err := s.AuthorizePost(ctx, p, post.ID)
			if err != nil {
				return nil, err
			}
			post.Locked = !decision.Allowed
		}
		out = append(out, post)
	}
	return out, rows.Err()
}

// AuthorizePost is the content-layer decision for one post. It repeats the age
// gate and the entitlement checks, and is called both when a delivery link is
// issued and again when it is redeemed.
func (s *Service) AuthorizePost(ctx context.Context, viewer *identity.Principal, postID string) (Access, error) {
	post, err := s.postByID(ctx, postID)
	if err != nil {
		return Access{}, err
	}
	if post.Status == PostRemoved {
		return Access{}, ErrNotFound
	}
	isOwner := viewer != nil && viewer.UserID == post.CreatorID
	isAdmin := viewer != nil && identity.HasPermission(viewer.Roles, identity.PermAdminAll)
	if post.Status != PostPublished && !isOwner && !isAdmin {
		return Access{}, ErrNotFound
	}
	if isAdmin {
		return Access{Allowed: true, Scope: post.Visibility, Reason: "admin"}, nil
	}
	if post.Visibility == VisibilityPublic {
		return Access{Allowed: true, Scope: post.Visibility, Reason: "public"}, nil
	}
	if viewer == nil {
		return Access{Allowed: false, Scope: post.Visibility, Reason: "authentication_required"}, nil
	}
	if s.adult == nil {
		return Access{Allowed: false, Scope: post.Visibility, Reason: ReasonAgeRequired}, nil
	}
	adult, err := s.adult.AllowAdult(ctx, viewer.UserID)
	if err != nil {
		return Access{}, fmt.Errorf("creator: age verification check: %w", err)
	}
	if !adult {
		return Access{Allowed: false, Scope: post.Visibility, Reason: ReasonAgeRequired}, nil
	}
	if isOwner {
		return Access{Allowed: true, Scope: post.Visibility, Reason: "owner"}, nil
	}

	now := s.now().UTC()
	if post.Visibility == VisibilitySubscriber {
		return s.paidAccess(ctx, viewer.UserID, post.CreatorID, ScopeSubscriber, now)
	}

	// Pay-per-view: a live unlock for this post, or a creator-wide ppv grant.
	unlocked, err := s.unlockLive(ctx, post.ID, viewer.UserID, now)
	if err != nil {
		return Access{}, err
	}
	if unlocked {
		return Access{Allowed: true, Scope: post.Visibility, Reason: "unlock"}, nil
	}
	ent, err := s.paidAccess(ctx, viewer.UserID, post.CreatorID, ScopePPV, now)
	if err != nil {
		return Access{}, err
	}
	if ent.Allowed {
		return ent, nil
	}
	return Access{Allowed: false, Scope: post.Visibility, Reason: "purchase_required"}, nil
}

func (s *Service) unlockLive(ctx context.Context, postID, fanID string, now time.Time) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM creator.post_unlocks
			WHERE post_id = $1::uuid AND fan_id = $2::uuid AND revoked_at IS NULL
			  AND (expires_at IS NULL OR expires_at > $3)
		)`, postID, fanID, now).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("creator: check unlock: %w", err)
	}
	return exists, nil
}

// UnlockPost is the pay-per-view purchase entry point. Without a payment
// provider it refuses with ErrPaymentRequired instead of granting content.
func (s *Service) UnlockPost(ctx context.Context, p identity.Principal, postID, requestID string) (Unlock, error) {
	post, err := s.postByID(ctx, postID)
	if err != nil {
		return Unlock{}, err
	}
	if post.Visibility != VisibilityPPV || post.Status != PostPublished {
		return Unlock{}, fmt.Errorf("%w: this post is not for sale", ErrInvalidInput)
	}
	if s.adult == nil {
		return Unlock{}, ErrAgeRequired
	}
	allowed, err := s.adult.AllowAdult(ctx, p.UserID)
	if err != nil {
		return Unlock{}, fmt.Errorf("creator: age verification check: %w", err)
	}
	if !allowed {
		return Unlock{}, ErrAgeRequired
	}
	if !s.fakePayments {
		return Unlock{}, ErrPaymentRequired
	}
	return s.grantUnlock(ctx, p.UserID, post.ID, p.UserID, "purchase", 0, requestID)
}

// GrantPostUnlock gives a fan access to one post, used by admin tooling.
func (s *Service) GrantPostUnlock(ctx context.Context, actor identity.Principal, postID, fanID string, ttl time.Duration, requestID string) (Unlock, error) {
	if _, err := s.postByID(ctx, postID); err != nil {
		return Unlock{}, err
	}
	source := "grant"
	if identity.HasPermission(actor.Roles, identity.PermAdminAll) {
		source = "admin"
	}
	return s.grantUnlock(ctx, actor.UserID, postID, fanID, source, ttl, requestID)
}

func (s *Service) grantUnlock(ctx context.Context, actorID, postID, fanID, source string, ttl time.Duration, requestID string) (Unlock, error) {
	var expires *time.Time
	if ttl > 0 {
		t := s.now().UTC().Add(ttl)
		expires = &t
	}
	var u Unlock
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.post_unlocks (post_id, fan_id, source, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		ON CONFLICT (post_id, fan_id) WHERE revoked_at IS NULL
		DO UPDATE SET expires_at = EXCLUDED.expires_at, granted_at = now()
		RETURNING id::text, post_id::text, source, granted_at, expires_at`,
		postID, fanID, source, expires).
		Scan(&u.ID, &u.PostID, &u.Source, &u.GrantedAt, &u.ExpiresAt)
	if err != nil {
		return Unlock{}, fmt.Errorf("creator: grant unlock: %w", err)
	}
	s.audit(ctx, actorID, "creator.post_unlocked", "creator_post", postID, requestID)
	return u, nil
}

// PostHasMedia reports whether a media item is attached to a post, so a delivery
// link cannot be issued for unrelated media paired with a public post.
func (s *Service) PostHasMedia(ctx context.Context, postID, mediaID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM creator.post_media WHERE post_id = $1::uuid AND media_id = $2::uuid)`,
		postID, mediaID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("creator: check post media: %w", err)
	}
	return ok, nil
}

// PostsByID returns a post to a viewer, with Locked set instead of an error when
// the viewer may not open it. Media links are issued separately and enforced at
// delivery, so a locked post reveals nothing playable.
func (s *Service) PostsByID(ctx context.Context, viewer *identity.Principal, postID string) (Post, error) {
	post, err := s.postByID(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	isOwner := viewer != nil && viewer.UserID == post.CreatorID
	isAdmin := viewer != nil && identity.HasPermission(viewer.Roles, identity.PermAdminAll)
	if post.Status != PostPublished && !isOwner && !isAdmin {
		return Post{}, ErrNotFound
	}
	if post.Visibility != VisibilityPublic {
		decision, err := s.AuthorizePost(ctx, viewer, postID)
		if err != nil {
			return Post{}, err
		}
		post.Locked = !decision.Allowed
	}
	return post, nil
}
