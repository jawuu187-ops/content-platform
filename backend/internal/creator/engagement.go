package creator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// decoratePost adds the viewer-specific engagement counters. A locked post still
// carries counters; it never carries comment text or media to a caller who has
// not passed AuthorizePost.
func (s *Service) decoratePost(ctx context.Context, post *Post, viewerID string) error {
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM creator.post_likes WHERE post_id = $1::uuid),
			(SELECT count(*) FROM creator.post_comments WHERE post_id = $1::uuid AND deleted_at IS NULL),
			EXISTS (SELECT 1 FROM creator.post_likes WHERE post_id = $1::uuid AND user_id = NULLIF($2, '')::uuid),
			EXISTS (SELECT 1 FROM creator.post_bookmarks WHERE post_id = $1::uuid AND user_id = NULLIF($2, '')::uuid)`,
		post.ID, viewerID).Scan(&post.Likes, &post.Comments, &post.Liked, &post.Bookmarked)
	if err != nil {
		return fmt.Errorf("creator: load engagement: %w", err)
	}
	return nil
}

func (s *Service) assertInteractable(ctx context.Context, p identity.Principal, postID string) (Post, error) {
	post, err := s.PostsByID(ctx, &p, postID)
	if err != nil {
		return Post{}, err
	}
	if post.Locked {
		return Post{}, ErrForbidden
	}
	return post, nil
}

// SetLike adds or removes the caller's like. It is idempotent.
func (s *Service) SetLike(ctx context.Context, p identity.Principal, postID string, liked bool) (Post, error) {
	if _, err := s.assertInteractable(ctx, p, postID); err != nil {
		return Post{}, err
	}
	if liked {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO creator.post_likes (post_id, user_id) VALUES ($1::uuid, $2::uuid)
			ON CONFLICT DO NOTHING`, postID, p.UserID)
		if err != nil {
			return Post{}, fmt.Errorf("creator: like post: %w", err)
		}
	} else {
		if _, err := s.pool.Exec(ctx, `
			DELETE FROM creator.post_likes WHERE post_id = $1::uuid AND user_id = $2::uuid`,
			postID, p.UserID); err != nil {
			return Post{}, fmt.Errorf("creator: unlike post: %w", err)
		}
	}
	post, err := s.PostsByID(ctx, &p, postID)
	if err != nil {
		return Post{}, err
	}
	return post, nil
}

// SetBookmark adds or removes a bookmark. A bookmark is a private list entry,
// not an access grant, so a locked post can be saved.
func (s *Service) SetBookmark(ctx context.Context, p identity.Principal, postID string, saved bool) error {
	post, err := s.postByID(ctx, postID)
	if err != nil {
		return err
	}
	if post.Status != PostPublished {
		return ErrNotFound
	}
	if saved {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO creator.post_bookmarks (post_id, user_id) VALUES ($1::uuid, $2::uuid)
			ON CONFLICT DO NOTHING`, postID, p.UserID)
	} else {
		_, err = s.pool.Exec(ctx, `
			DELETE FROM creator.post_bookmarks WHERE post_id = $1::uuid AND user_id = $2::uuid`,
			postID, p.UserID)
	}
	if err != nil {
		return fmt.Errorf("creator: set bookmark: %w", err)
	}
	return nil
}

// BookmarkedPosts returns the caller's saved posts, newest saved first.
func (s *Service) BookmarkedPosts(ctx context.Context, p identity.Principal) ([]Post, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id::text
		FROM creator.post_bookmarks b
		JOIN creator.posts p ON p.id = b.post_id
		WHERE b.user_id = $1::uuid AND p.status = 'published'
		ORDER BY b.created_at DESC
		LIMIT 100`, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("creator: list bookmarks: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, 20)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("creator: scan bookmark: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Post, 0, len(ids))
	for _, id := range ids {
		post, err := s.PostsByID(ctx, &p, id)
		if err != nil {
			continue // a removed or newly inaccessible post is silently skipped
		}
		out = append(out, post)
	}
	return out, nil
}

// Comments returns visible comments on a post the caller may view.
func (s *Service) Comments(ctx context.Context, p identity.Principal, postID string) ([]Comment, error) {
	if _, err := s.assertInteractable(ctx, p, postID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.post_id::text, c.user_id::text,
		       COALESCE(NULLIF(cp.display_name, ''), 'Fan'),
		       c.body, c.created_at
		FROM creator.post_comments c
		LEFT JOIN creator.profiles cp ON cp.user_id = c.user_id
		WHERE c.post_id = $1::uuid AND c.deleted_at IS NULL
		ORDER BY c.created_at ASC
		LIMIT 100`, postID)
	if err != nil {
		return nil, fmt.Errorf("creator: list comments: %w", err)
	}
	defer rows.Close()
	out := make([]Comment, 0, 8)
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("creator: scan comment: %w", err)
		}
		c.Mine = c.AuthorID == p.UserID
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddComment validates length and access before writing.
func (s *Service) AddComment(ctx context.Context, p identity.Principal, postID, body, requestID string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > 500 {
		return Comment{}, fmt.Errorf("%w: comment must be between 1 and 500 characters", ErrInvalidInput)
	}
	if _, err := s.assertInteractable(ctx, p, postID); err != nil {
		return Comment{}, err
	}
	var c Comment
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.post_comments (post_id, user_id, body)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id::text, post_id::text, user_id::text, body, created_at`,
		postID, p.UserID, body).Scan(&c.ID, &c.PostID, &c.AuthorID, &c.Body, &c.CreatedAt)
	if err != nil {
		return Comment{}, fmt.Errorf("creator: add comment: %w", err)
	}
	c.Mine = true
	var display string
	_ = s.pool.QueryRow(ctx, `SELECT display_name FROM creator.profiles WHERE user_id = $1::uuid`, p.UserID).Scan(&display)
	if display != "" {
		c.AuthorName = display
	} else {
		c.AuthorName = "Fan"
	}
	s.audit(ctx, p.UserID, "post.comment", "post", postID, requestID)
	return c, nil
}

// DeleteComment lets the author, the post creator or an administrator remove a
// comment. Soft deletion keeps moderation history.
func (s *Service) DeleteComment(ctx context.Context, p identity.Principal, commentID, requestID string) error {
	var authorID, postID, creatorID string
	err := s.pool.QueryRow(ctx, `
		SELECT c.user_id::text, c.post_id::text, p.creator_id::text
		FROM creator.post_comments c JOIN creator.posts p ON p.id = c.post_id
		WHERE c.id = $1::uuid AND c.deleted_at IS NULL`, commentID).Scan(&authorID, &postID, &creatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("creator: load comment: %w", err)
	}
	admin := identity.HasPermission(p.Roles, identity.PermAdminAll)
	if p.UserID != authorID && p.UserID != creatorID && !admin {
		return ErrForbidden
	}
	if _, err := s.pool.Exec(ctx, `UPDATE creator.post_comments SET deleted_at = now() WHERE id = $1::uuid`, commentID); err != nil {
		return fmt.Errorf("creator: delete comment: %w", err)
	}
	s.audit(ctx, p.UserID, "post.comment_deleted", "post", postID, requestID)
	return nil
}
