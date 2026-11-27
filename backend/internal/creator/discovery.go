package creator

import (
	"context"
	"fmt"
	"strings"
)

// DiscoveryQuery is a bounded, deterministic page of published creators.
type DiscoveryQuery struct {
	Text     string
	Category string
	Limit    int
	Offset   int
}

var discoveryCategories = map[string]bool{
	"art": true, "music": true, "fitness": true, "lifestyle": true,
	"photography": true, "writing": true, "education": true, "other": true,
}

// ListPublished returns the first page for callers that do not paginate.
func (s *Service) ListPublished(ctx context.Context, query string, limit int) ([]Profile, error) {
	page, _, err := s.ListPublishedPage(ctx, DiscoveryQuery{Text: query, Limit: limit})
	return page, err
}

// ListPublishedPage returns a page plus the offset to request next, or -1 when
// there are no more rows. Category filtering is applied only for the known set;
// an unknown category is ignored rather than echoed into SQL.
func (s *Service) ListPublishedPage(ctx context.Context, q DiscoveryQuery) ([]Profile, int, error) {
	if q.Limit <= 0 || q.Limit > 50 {
		q.Limit = 24
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	q.Text = strings.TrimSpace(q.Text)
	if q.Category != "" && !discoveryCategories[q.Category] {
		q.Category = ""
	}
	rows, err := s.pool.Query(ctx, `
		SELECT user_id::text, handle::text, display_name, bio, category,
		       COALESCE(avatar_url, ''), COALESCE(cover_url, ''), is_published, created_at, updated_at
		FROM creator.profiles
		WHERE is_published
		  AND ($1 = '' OR handle::text ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR category = $2)
		ORDER BY updated_at DESC, user_id ASC
		LIMIT $3 OFFSET $4`, q.Text, q.Category, q.Limit+1, q.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("creator: list profiles: %w", err)
	}
	defer rows.Close()
	out := make([]Profile, 0, q.Limit+1)
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.Category,
			&p.AvatarURL, &p.CoverURL, &p.Published, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("creator: scan profile: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	next := -1
	if len(out) > q.Limit {
		out = out[:q.Limit]
		next = q.Offset + q.Limit
	}
	return out, next, nil
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
