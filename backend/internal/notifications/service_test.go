package notifications

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func user(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO identity.users (email, password_hash) VALUES ($1, 'x') RETURNING id::text`,
		email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOutboxProcessesEngagementAndHonoursPreferences(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	svc := New(pool)
	creatorID := user(t, pool, "notify-creator@example.test")
	fanID := user(t, pool, "notify-fan@example.test")
	if _, err := pool.Exec(ctx, `INSERT INTO creator.profiles (user_id, handle, display_name, category, is_published)
		VALUES ($1::uuid, 'notify_studio', 'Notify Studio', 'art', true)`, creatorID); err != nil {
		t.Fatal(err)
	}
	var postID string
	if err := pool.QueryRow(ctx, `INSERT INTO creator.posts (creator_id, body, visibility, status, published_at)
		VALUES ($1::uuid, 'hello', 'public', 'published', now()) RETURNING id::text`, creatorID).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO creator.post_comments (post_id, user_id, body) VALUES ($1::uuid, $2::uuid, 'nice post')`, postID, fanID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.ProcessBatch(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("process: n=%d err=%v", n, err)
	}
	items, unread, err := svc.List(ctx, creatorID, 10)
	if err != nil || len(items) != 1 || unread != 1 {
		t.Fatalf("items=%+v unread=%d err=%v", items, unread, err)
	}
	if items[0].Title != "New comment" || items[0].Link != "/posts/"+postID {
		t.Fatalf("unexpected item %+v", items[0])
	}
	if err := svc.MarkRead(ctx, creatorID, items[0].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Unread(ctx, creatorID); got != 0 {
		t.Fatalf("unread after read = %d", got)
	}

	// Engagement disabled: the event is consumed but creates no item.
	if _, err := svc.SavePreferences(ctx, creatorID, Preferences{
		InApp: true, Messages: true, Engagement: false, Payments: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO creator.post_likes (post_id, user_id) VALUES ($1::uuid, $2::uuid)`, postID, fanID); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ProcessBatch(ctx, 10); err != nil || n != 1 {
		t.Fatalf("second process: n=%d err=%v", n, err)
	}
	items, _, _ = svc.List(ctx, creatorID, 10)
	if len(items) != 1 {
		t.Fatalf("disabled engagement created an item: %+v", items)
	}
}

func TestMessageAndPaymentTriggersWriteEvents(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	svc := New(pool)
	creatorID := user(t, pool, "msg-creator@example.test")
	fanID := user(t, pool, "msg-fan@example.test")
	if _, err := pool.Exec(ctx, `INSERT INTO creator.profiles (user_id, handle, display_name, category)
		VALUES ($1::uuid, 'msg_studio', 'Msg Studio', 'art')`, creatorID); err != nil {
		t.Fatal(err)
	}
	var conv string
	if err := pool.QueryRow(ctx, `INSERT INTO messaging.conversations (creator_id, fan_id)
		VALUES ($1::uuid, $2::uuid) RETURNING id::text`, creatorID, fanID).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messaging.messages (conversation_id, sender_id, body)
		VALUES ($1::uuid, $2::uuid, 'hello creator')`, conv, fanID); err != nil {
		t.Fatal(err)
	}
	// A topup payment has no creator, so only the buyer is notified.
	if _, err := pool.Exec(ctx, `INSERT INTO payments.intents
		(user_id, purpose, amount_minor, currency, provider, status, idempotency_key, settled_at)
		VALUES ($1::uuid, 'topup', 500, 'USD', 'fake', 'succeeded', 'notify-topup-1', now())`, fanID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.ProcessBatch(ctx, 10)
	if err != nil || n != 2 {
		t.Fatalf("process: n=%d err=%v", n, err)
	}
	creatorItems, _, _ := svc.List(ctx, creatorID, 10)
	if len(creatorItems) != 1 || creatorItems[0].Category != "messages" {
		t.Fatalf("creator items = %+v", creatorItems)
	}
	fanItems, _, _ := svc.List(ctx, fanID, 10)
	if len(fanItems) != 1 || fanItems[0].Title != "Payment successful" {
		t.Fatalf("fan items = %+v", fanItems)
	}
}
