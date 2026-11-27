package creator

import (
	"context"
	"testing"
	"time"
)

func TestScheduledPostBecomesPublishedOnceDue(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, _ := h.creator(t)
	at := time.Now().UTC().Add(2 * time.Hour)
	post, err := h.svc.CreatePost(ctx, owner, PostInput{
		Body: "Later", Visibility: VisibilityPublic, ScheduledAt: &at,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if post.Status != PostScheduled || post.ScheduledAt == nil || post.PublishedAt != nil {
		t.Fatalf("scheduled post = %+v", post)
	}
	feed, err := h.svc.Posts(ctx, &owner, profile.Handle)
	if err != nil || len(feed) != 0 {
		t.Fatalf("scheduled leaked into feed: %+v err=%v", feed, err)
	}
	if _, err := h.svc.pool.Exec(ctx, `UPDATE creator.posts SET scheduled_at = now() - interval '1 second' WHERE id=$1::uuid`, post.ID); err != nil {
		t.Fatal(err)
	}
	n, err := h.svc.PublishDueScheduled(ctx)
	if err != nil || n != 1 {
		t.Fatalf("publish due n=%d err=%v", n, err)
	}
	feed, err = h.svc.Posts(ctx, &owner, profile.Handle)
	if err != nil || len(feed) != 1 || feed[0].Status != PostPublished {
		t.Fatalf("feed after due = %+v err=%v", feed, err)
	}
	mine, err := h.svc.MyPosts(ctx, owner)
	if err != nil || len(mine) != 1 || mine[0].Status != PostPublished {
		t.Fatalf("my posts = %+v err=%v", mine, err)
	}
}
