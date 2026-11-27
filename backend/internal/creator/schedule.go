package creator

import (
	"context"
	"fmt"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// MyPosts returns every non-removed post for the caller, including drafts and
// scheduled posts, newest first. This is the studio/vault view.
func (s *Service) MyPosts(ctx context.Context, p identity.Principal) ([]Post, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, creator_id::text, body, visibility, price_minor, COALESCE(currency, ''),
		       status, published_at, scheduled_at, created_at
		FROM creator.posts
		WHERE creator_id = $1::uuid AND status <> 'removed'
		ORDER BY created_at DESC
		LIMIT 100`, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("creator: list my posts: %w", err)
	}
	defer rows.Close()
	out := make([]Post, 0, 20)
	for rows.Next() {
		var post Post
		if err := rows.Scan(&post.ID, &post.CreatorID, &post.Body, &post.Visibility, &post.PriceMinor,
			&post.Currency, &post.Status, &post.PublishedAt, &post.ScheduledAt, &post.CreatedAt); err != nil {
			return nil, fmt.Errorf("creator: scan my post: %w", err)
		}
		post.Media, _ = s.postMedia(ctx, post.ID)
		if err := s.decoratePost(ctx, &post, p.UserID); err != nil {
			return nil, err
		}
		out = append(out, post)
	}
	return out, rows.Err()
}

// BindScheduledMedia attaches owned media to a draft or scheduled post.
func (s *Service) BindScheduledMedia(ctx context.Context, p identity.Principal, postID string, mediaIDs []string) error {
	var status string
	if err := s.pool.QueryRow(ctx, `
		SELECT status FROM creator.posts WHERE id = $1::uuid AND creator_id = $2::uuid`,
		postID, p.UserID).Scan(&status); err != nil {
		return ErrNotFound
	}
	if status != PostDraft && status != PostScheduled {
		return fmt.Errorf("%w: only drafts and scheduled posts can be edited here", ErrInvalidInput)
	}
	if err := s.assertMediaOwned(ctx, p.UserID, mediaIDs); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM creator.post_media WHERE post_id = $1::uuid`, postID); err != nil {
		return err
	}
	if err := attachMedia(ctx, tx, postID, mediaIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PublishDueScheduled moves due posts into the public feed. The UPDATE is
// idempotent: a second worker cannot publish the same post twice.
func (s *Service) PublishDueScheduled(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE creator.posts
		SET status = 'published', published_at = now(), scheduled_at = NULL, updated_at = now()
		WHERE status = 'scheduled' AND scheduled_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("creator: publish scheduled posts: %w", err)
	}
	return tag.RowsAffected(), nil
}
