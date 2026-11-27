package creator

import (
	"context"
	"fmt"
	"strings"
)

// ListPublished returns published creator profiles for discovery. The query is
// an optional case-insensitive match on handle or display name; there is no
// ranking signal yet, so ordering is deterministic and recent-first.
func (s *Service) ListPublished(ctx context.Context, query string, limit int) ([]Profile, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	query = strings.TrimSpace(query)
	rows, err := s.pool.Query(ctx, `
		SELECT user_id::text, handle::text, display_name, bio, category,
		       COALESCE(avatar_url, ''), COALESCE(cover_url, ''), is_published, created_at, updated_at
		FROM creator.profiles
		WHERE is_published
		  AND ($1 = '' OR handle::text ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%')
		ORDER BY updated_at DESC
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("creator: list profiles: %w", err)
	}
	defer rows.Close()
	out := make([]Profile, 0, limit)
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.Category,
			&p.AvatarURL, &p.CoverURL, &p.Published, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("creator: scan profile: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MyProfile returns the caller's own profile and every tier, including inactive
// ones, because the owner manages them.
func (s *Service) MyProfile(ctx context.Context, userID string) (Profile, []Tier, error) {
	p, err := s.ProfileByUserID(ctx, userID)
	if err != nil {
		return Profile{}, nil, err
	}
	tiers, err := s.tiersOf(ctx, p.UserID, false)
	if err != nil {
		return Profile{}, nil, err
	}
	return p, tiers, nil
}
