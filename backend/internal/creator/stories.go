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

type Story struct {
	ID        string    `json:"id"`
	CreatorID string    `json:"creator_id"`
	MediaID   string    `json:"media_id"`
	Caption   string    `json:"caption,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) CreateStory(ctx context.Context, p identity.Principal, mediaID, caption string) (Story, error) {
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return Story{}, fmt.Errorf("%w: media is required", ErrInvalidInput)
	}
	caption = strings.TrimSpace(caption)
	if len([]rune(caption)) > 200 {
		return Story{}, fmt.Errorf("%w: caption is limited to 200 characters", ErrInvalidInput)
	}
	var ok bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM media.objects
		WHERE id = $1::uuid AND owner_id = $2::uuid AND kind IN ('image','video')
		  AND status = 'ready')`, mediaID, p.UserID).Scan(&ok); err != nil {
		return Story{}, err
	}
	if !ok {
		return Story{}, ErrNotFound
	}
	now := s.now().UTC()
	var st Story
	err := s.pool.QueryRow(ctx, `
		INSERT INTO creator.stories (creator_id, media_id, caption, created_at, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		RETURNING id::text, creator_id::text, media_id::text, caption, created_at, expires_at`,
		p.UserID, mediaID, caption, now, now.Add(24*time.Hour)).
		Scan(&st.ID, &st.CreatorID, &st.MediaID, &st.Caption, &st.CreatedAt, &st.ExpiresAt)
	if err != nil {
		return Story{}, fmt.Errorf("creator: create story: %w", err)
	}
	return st, nil
}

// Stories returns active stories when the viewer is entitled, the owner or an
// administrator. Locked stories are omitted rather than shown as placeholders.
func (s *Service) Stories(ctx context.Context, p identity.Principal, handle string) ([]Story, error) {
	profile, err := s.profileByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return nil, err
	}
	if p.UserID != profile.UserID && !identity.HasPermission(p.Roles, identity.PermAdminAll) {
		decision, err := s.CheckAccess(ctx, &p, handle, ScopeSubscriber)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			return nil, ErrForbidden
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, creator_id::text, media_id::text, caption, created_at, expires_at
		FROM creator.stories
		WHERE creator_id = $1::uuid AND expires_at > $2
		ORDER BY created_at DESC LIMIT 30`, profile.UserID, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("creator: list stories: %w", err)
	}
	defer rows.Close()
	out := []Story{}
	for rows.Next() {
		var st Story
		if err := rows.Scan(&st.ID, &st.CreatorID, &st.MediaID, &st.Caption, &st.CreatedAt, &st.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Service) StoryMedia(ctx context.Context, p identity.Principal, storyID string) (string, error) {
	var mediaID, creatorID string
	err := s.pool.QueryRow(ctx, `
		SELECT media_id::text, creator_id::text FROM creator.stories
		WHERE id = $1::uuid AND expires_at > $2`, storyID, s.now().UTC()).Scan(&mediaID, &creatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if p.UserID != creatorID && !identity.HasPermission(p.Roles, identity.PermAdminAll) {
		var handle string
		if err := s.pool.QueryRow(ctx, `SELECT handle::text FROM creator.profiles WHERE user_id=$1::uuid`, creatorID).Scan(&handle); err != nil {
			return "", ErrNotFound
		}
		decision, err := s.CheckAccess(ctx, &p, handle, ScopeSubscriber)
		if err != nil {
			return "", err
		}
		if !decision.Allowed {
			return "", ErrForbidden
		}
	}
	return mediaID, nil
}
