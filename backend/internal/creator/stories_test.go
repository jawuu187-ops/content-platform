package creator

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStoriesRequireEntitlementAndExpire(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, _ := h.creator(t)
	key := strings.Repeat("c", 64)
	var mediaID string
	if err := h.svc.pool.QueryRow(ctx, `
		INSERT INTO media.objects (owner_id, kind, storage_key, content_type, byte_size, sha256)
		VALUES ($1::uuid, 'image', $2, 'image/png', 5, $2) RETURNING id::text`, owner.UserID, key).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	story, err := h.svc.CreateStory(ctx, owner, mediaID, "today")
	if err != nil {
		t.Fatal(err)
	}
	own, err := h.svc.Stories(ctx, owner, profile.Handle)
	if err != nil || len(own) != 1 {
		t.Fatalf("owner stories = %+v err=%v", own, err)
	}
	if _, err := h.svc.StoryMedia(ctx, owner, story.ID); err != nil {
		t.Fatal(err)
	}
	fan := h.account(t)
	if _, err := h.svc.Stories(ctx, fan, profile.Handle); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unentitled stories = %v", err)
	}
	if _, err := h.svc.StoryMedia(ctx, fan, story.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unentitled media = %v", err)
	}
}
