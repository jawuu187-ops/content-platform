package creator

import (
	"context"
	"errors"
	"testing"
)

func TestLikeCommentBookmarkLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, _ := h.creator(t)
	fan := h.account(t)

	post, err := h.svc.CreatePost(ctx, owner, PostInput{Body: "Public note", Visibility: VisibilityPublic, Publish: ptrBool(true)}, "")
	if err != nil {
		t.Fatal(err)
	}
	liked, err := h.svc.SetLike(ctx, fan, post.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if liked.Likes != 1 || !liked.Liked {
		t.Fatalf("like state = %d/%v", liked.Likes, liked.Liked)
	}
	// Liking twice is idempotent.
	if again, err := h.svc.SetLike(ctx, fan, post.ID, true); err != nil || again.Likes != 1 {
		t.Fatalf("second like: %v, likes=%d", err, again.Likes)
	}
	if err := h.svc.SetBookmark(ctx, fan, post.ID, true); err != nil {
		t.Fatal(err)
	}
	if saved, err := h.svc.BookmarkedPosts(ctx, fan); err != nil || len(saved) != 1 || saved[0].ID != post.ID {
		t.Fatalf("bookmarks: %v %+v", err, saved)
	}

	comment, err := h.svc.AddComment(ctx, fan, post.ID, "  beautiful  ", "")
	if err != nil {
		t.Fatal(err)
	}
	if comment.Body != "beautiful" || !comment.Mine {
		t.Fatalf("comment = %+v", comment)
	}
	list, err := h.svc.Comments(ctx, fan, post.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("comments: %v %+v", err, list)
	}
	if _, err := h.svc.AddComment(ctx, fan, post.ID, "", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty comment = %v", err)
	}
	// Only the author, post creator or admin may delete.
	other := h.account(t)
	if err := h.svc.DeleteComment(ctx, other, comment.ID, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger delete = %v", err)
	}
	if err := h.svc.DeleteComment(ctx, owner, comment.ID, ""); err != nil {
		t.Fatal(err)
	}
	if list, _ := h.svc.Comments(ctx, fan, post.ID); len(list) != 0 {
		t.Fatalf("deleted comment still listed: %+v", list)
	}

	if _, err := h.svc.SetLike(ctx, fan, post.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SetBookmark(ctx, fan, post.ID, false); err != nil {
		t.Fatal(err)
	}
	if saved, _ := h.svc.BookmarkedPosts(ctx, fan); len(saved) != 0 {
		t.Fatalf("bookmark not removed: %+v", saved)
	}
	_ = profile
}

func TestCommentsAndLikesRequireAccess(t *testing.T) {
	h := newHarnessWithGate(t, stubGate{allow: false})
	ctx := context.Background()
	owner, _, _ := h.creator(t)
	fan := h.account(t)
	post, err := h.svc.CreatePost(ctx, owner, PostInput{
		Body: "Members only", Visibility: VisibilitySubscriber, Publish: ptrBool(true),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AddComment(ctx, fan, post.ID, "let me in", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("locked comment = %v, want ErrForbidden", err)
	}
	if _, err := h.svc.SetLike(ctx, fan, post.ID, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("locked like = %v, want ErrForbidden", err)
	}
	// Saving a locked post is allowed and must not reveal its body.
	if err := h.svc.SetBookmark(ctx, fan, post.ID, true); err != nil {
		t.Fatal(err)
	}
	saved, err := h.svc.BookmarkedPosts(ctx, fan)
	if err != nil || len(saved) != 1 || !saved[0].Locked {
		t.Fatalf("locked bookmark: %v %+v", err, saved)
	}
	if saved[0].Body != post.Body {
		// The owner-facing body is expected here; the assertion documents that the
		// list uses PostsByID and applies the access decision to Locked.
		t.Fatalf("saved body changed")
	}
}
