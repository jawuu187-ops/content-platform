package creator

import (
	"context"
	"errors"
	"testing"
)

func TestProfileMediaOwnershipAndVisibility(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, _ := h.creator(t)
	var mediaID string
	key := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := h.svc.pool.QueryRow(ctx, `
		INSERT INTO media.objects (owner_id, kind, storage_key, content_type, byte_size, sha256)
		VALUES ($1::uuid, 'image', $2, 'image/png', 10, $3) RETURNING id::text`,
		owner.UserID, key, key).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.SetProfileMedia(ctx, owner, "avatar", mediaID, "req"); err != nil {
		t.Fatal(err)
	}
	got, err := h.svc.ProfileMedia(ctx, owner, profile.Handle, "avatar")
	if err != nil || got != mediaID {
		t.Fatalf("avatar = %q err=%v", got, err)
	}
	stranger := h.account(t)
	if _, err := h.svc.ProfileMedia(ctx, stranger, profile.Handle, "avatar"); err != nil {
		t.Fatalf("published avatar must be readable: %v", err)
	}
	// Another creator cannot attach someone else's image.
	other, otherProfile, _ := h.creator(t)
	if _, err := h.svc.SetProfileMedia(ctx, other, "cover", mediaID, "req"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign media = %v, want ErrForbidden", err)
	}
	if _, err := h.svc.ProfileMedia(ctx, stranger, otherProfile.Handle, "cover"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing cover = %v, want ErrNotFound", err)
	}
}
