package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const testPassword = "a-very-long-password"

type allowGate struct{}

func (allowGate) AllowAdult(context.Context, string) (bool, error) { return true, nil }

type harness struct {
	svc      *Service
	ids      *identity.Service
	creators *creator.Service
	// pool is the harness's connection. Calling dbtest.Pool again inside a test would
	// re-run its cleanup and delete the users the test just created.
	pool interface {
		Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := dbtest.Pool(t)
	return &harness{
		svc:      New(pool),
		ids:      identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams()))),
		creators: creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(allowGate{})),
		pool:     pool,
	}
}

func uniqueEmail(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(buf) + "@example.test"
}

func (h *harness) account(t *testing.T) identity.Principal {
	t.Helper()
	res, err := h.ids.Register(context.Background(), uniqueEmail("dm"), testPassword,
		identity.DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal
}

func (h *harness) creatorAccount(t *testing.T) identity.Principal {
	t.Helper()
	p := h.account(t)
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	if _, err := h.creators.CreateProfile(context.Background(), p, creator.ProfileInput{
		Handle:      "studio" + hex.EncodeToString(buf),
		DisplayName: "Mira",
		Category:    "art",
	}, ""); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return p
}

// grantEntitlement writes the row the access check reads. Using SQL directly keeps
// these tests about messaging rather than about the purchase flow, which has its own
// tests in package creator.
func (h *harness) grantEntitlement(t *testing.T, fanID, creatorID string) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, granted_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'grant', now())`, fanID, creatorID); err != nil {
		t.Fatalf("grant entitlement: %v", err)
	}
}

func TestNormalizeBody(t *testing.T) {
	if got, err := NormalizeBody("  hello\nworld  "); err != nil || got != "hello\nworld" {
		t.Fatalf("normalize: %q %v", got, err)
	}
	if _, err := NormalizeBody("   "); err == nil {
		t.Fatal("empty body must be refused")
	}
	if _, err := NormalizeBody(strings.Repeat("a", MaxBodyLength+1)); !errors.Is(err, ErrBodyTooLong) {
		t.Fatalf("oversized body: want ErrBodyTooLong, got %v", err)
	}
	// Control characters are dropped, newlines kept: they are layout, not content.
	if got, _ := NormalizeBody("a\x00b\x07c\td"); got != "abc\td" {
		t.Fatalf("control characters survived: %q", got)
	}
}

func TestOpenRequiresEntitlementAndIsIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)

	if _, err := h.svc.Open(ctx, fan, creatorAccount.UserID); !errors.Is(err, ErrNotEntitled) {
		t.Fatalf("opens without entitlement: want ErrNotEntitled, got %v", err)
	}
	if _, err := h.svc.Open(ctx, fan, fan.UserID); !errors.Is(err, ErrSelf) {
		t.Fatalf("self conversation: want ErrSelf, got %v", err)
	}

	h.grantEntitlement(t, fan.UserID, creatorAccount.UserID)
	first, err := h.svc.Open(ctx, fan, creatorAccount.UserID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	second, err := h.svc.Open(ctx, fan, creatorAccount.UserID)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("opening twice forked a conversation: %s vs %s", first.ID, second.ID)
	}
	if first.PeerName != "Mira" {
		t.Fatalf("peer name = %q, want the creator display name", first.PeerName)
	}
	if !first.CanReply {
		t.Fatal("an entitled fan must be able to reply")
	}
}

func TestSendAndThreadAreOrderedAndAuthorized(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)
	h.grantEntitlement(t, fan.UserID, creatorAccount.UserID)

	conv, err := h.svc.Open(ctx, fan, creatorAccount.UserID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := h.svc.Send(ctx, fan, conv.ID, "hello there"); err != nil {
		t.Fatalf("fan send: %v", err)
	}
	if _, err := h.svc.Send(ctx, creatorAccount, conv.ID, "hi, thanks for subscribing"); err != nil {
		t.Fatalf("creator send: %v", err)
	}

	fanThread, err := h.svc.Thread(ctx, fan, conv.ID)
	if err != nil {
		t.Fatalf("fan thread: %v", err)
	}
	if len(fanThread) != 2 {
		t.Fatalf("thread length = %d, want 2", len(fanThread))
	}
	if fanThread[0].Body != "hello there" || !fanThread[0].Mine {
		t.Fatalf("first message wrong: %+v", fanThread[0])
	}
	if fanThread[1].Mine {
		t.Fatal("the creator's message must not be marked as the fan's own")
	}
	// The creator read the thread, so the fan's message is no longer unread.
	if fanThread[0].ReadAt != nil {
		t.Fatalf("read_at set before the creator opened the thread: %+v", fanThread[0])
	}
	creatorThread, err := h.svc.Thread(ctx, creatorAccount, conv.ID)
	if err != nil {
		t.Fatalf("creator thread: %v", err)
	}
	if len(creatorThread) != 2 || creatorThread[1].Mine != true {
		t.Fatalf("creator view wrong: %+v", creatorThread)
	}

	outsider := h.account(t)
	if _, err := h.svc.Thread(ctx, outsider, conv.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider reads thread: want ErrForbidden, got %v", err)
	}
	if _, err := h.svc.Send(ctx, outsider, conv.ID, "let me in"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider writes: want ErrForbidden, got %v", err)
	}
}

func TestLapsedEntitlementStopsNewMessagesButKeepsHistory(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)
	h.grantEntitlement(t, fan.UserID, creatorAccount.UserID)

	conv, err := h.svc.Open(ctx, fan, creatorAccount.UserID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := h.svc.Send(ctx, fan, conv.ID, "before the lapse"); err != nil {
		t.Fatalf("send: %v", err)
	}

	if _, err := h.pool.Exec(ctx, `
		UPDATE creator.entitlements SET revoked_at = now()
		WHERE fan_id = $1::uuid AND creator_id = $2::uuid`, fan.UserID, creatorAccount.UserID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := h.svc.Send(ctx, fan, conv.ID, "after the lapse"); !errors.Is(err, ErrNotEntitled) {
		t.Fatalf("send after revoke: want ErrNotEntitled, got %v", err)
	}
	thread, err := h.svc.Thread(ctx, fan, conv.ID)
	if err != nil {
		t.Fatalf("history must stay readable: %v", err)
	}
	if len(thread) != 1 || thread[0].Body != "before the lapse" {
		t.Fatalf("history was lost: %+v", thread)
	}

	list, err := h.svc.List(ctx, fan)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].CanReply {
		t.Fatalf("a lapsed thread must be listed as not repliable: %+v", list)
	}
}

// The inbox has to render without opening every thread: one preview line, an unread
// count and the reply flag must all travel in the list response.
func TestListCarriesPreviewAndUnreadCount(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)
	h.grantEntitlement(t, fan.UserID, creatorAccount.UserID)

	conv, err := h.svc.Open(ctx, fan, creatorAccount.UserID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := h.svc.Send(ctx, fan, conv.ID, "first line\nsecond line"); err != nil {
		t.Fatalf("send: %v", err)
	}

	// The creator's two replies are unread for the fan until the fan opens the thread.
	for _, body := range []string{"reply one", "reply two"} {
		if _, err := h.svc.Send(ctx, creatorAccount, conv.ID, body); err != nil {
			t.Fatalf("creator send: %v", err)
		}
	}

	list, err := h.svc.List(ctx, fan)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	got := list[0]
	if got.LastMessageBody != "reply two" {
		t.Fatalf("preview = %q, want the newest body", got.LastMessageBody)
	}
	if got.UnreadCount != 2 {
		t.Fatalf("unread = %d, want 2", got.UnreadCount)
	}
	if !got.CanReply {
		t.Fatal("an entitled fan must be listed as repliable")
	}

	// Opening the thread clears the fan's unread count but not the creator's.
	if _, err := h.svc.Thread(ctx, fan, conv.ID); err != nil {
		t.Fatalf("thread: %v", err)
	}
	list, err = h.svc.List(ctx, fan)
	if err != nil {
		t.Fatalf("list after read: %v", err)
	}
	if list[0].UnreadCount != 0 {
		t.Fatalf("unread after opening = %d, want 0", list[0].UnreadCount)
	}

	creatorList, err := h.svc.List(ctx, creatorAccount)
	if err != nil {
		t.Fatalf("creator list: %v", err)
	}
	if len(creatorList) != 1 || creatorList[0].UnreadCount != 1 {
		t.Fatalf("creator inbox wrong: %+v", creatorList)
	}
	// A multi-line body must not stretch an inbox row.
	if creatorList[0].LastMessageBody != "reply two" {
		t.Fatalf("creator preview = %q", creatorList[0].LastMessageBody)
	}
}

func TestPreviewFlattensNewlinesAndTruncates(t *testing.T) {
	if got := preview("a\nb\n\nc"); got != "a b c" {
		t.Fatalf("preview flatten = %q", got)
	}
	long := strings.Repeat("x", PreviewLength+40)
	got := []rune(preview(long))
	if len(got) != PreviewLength {
		t.Fatalf("preview length = %d, want %d", len(got), PreviewLength)
	}
	if got[len(got)-1] != '\u2026' {
		t.Fatalf("a cut preview must end in an ellipsis, got %q", got[len(got)-1])
	}
	// Cutting must not split a multi-byte rune.
	if out := preview(strings.Repeat("\u2603", PreviewLength+5)); !strings.HasSuffix(out, "\u2026") || len([]rune(out)) != PreviewLength {
		t.Fatalf("multibyte preview wrong: %q", out)
	}
}

func TestMessagesCannotBeEditedOrDeleted(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorAccount := h.creatorAccount(t)
	h.grantEntitlement(t, fan.UserID, creatorAccount.UserID)

	conv, err := h.svc.Open(ctx, fan, creatorAccount.UserID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	m, err := h.svc.Send(ctx, fan, conv.ID, "original")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if _, err := h.pool.Exec(ctx, `UPDATE messaging.messages SET body = 'rewritten' WHERE id = $1::uuid`, m.ID); err == nil {
		t.Fatal("the database accepted an edit to a message")
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM messaging.messages WHERE id = $1::uuid`, m.ID); err == nil {
		t.Fatal("the database accepted a delete of a message")
	}
}

func TestAttachmentAndBroadcast(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := h.creatorAccount(t)
	fan := h.account(t)
	h.grantEntitlement(t, fan.UserID, owner.UserID)
	conv, err := h.svc.Open(ctx, fan, owner.UserID)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("b", 64)
	var mediaID string
	if err := h.pool.(*pgxpool.Pool).QueryRow(ctx, `
		INSERT INTO media.objects (owner_id, kind, storage_key, content_type, byte_size, sha256)
		VALUES ($1::uuid, 'image', $2, 'image/png', 12, $2) RETURNING id::text`, fan.UserID, key).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	m, err := h.svc.SendWithMedia(ctx, fan, conv.ID, "look", mediaID)
	if err != nil || m.MediaID != mediaID {
		t.Fatalf("attachment: %+v err=%v", m, err)
	}
	if got, err := h.svc.MessageMedia(ctx, owner, m.ID); err != nil || got != mediaID {
		t.Fatalf("media read: %q err=%v", got, err)
	}
	if _, err := h.svc.MessageMedia(ctx, h.account(t), m.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger media = %v", err)
	}
	sent, err := h.svc.Broadcast(ctx, owner, "new set is live")
	if err != nil || sent != 1 {
		t.Fatalf("broadcast sent=%d err=%v", sent, err)
	}
}

func TestPaidMessageRedactionAndRequestLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := h.creatorAccount(t)
	fan := h.account(t)
	h.grantEntitlement(t, fan.UserID, owner.UserID)
	conv, err := h.svc.Open(ctx, fan, owner.UserID)
	if err != nil {
		t.Fatal(err)
	}
	price := int64(750)
	paid, err := h.svc.SendPaid(ctx, owner, conv.ID, "secret paid body", "", &price)
	if err != nil || paid.PriceMinor == nil || *paid.PriceMinor != price {
		t.Fatalf("paid send: %+v %v", paid, err)
	}
	thread, err := h.svc.Thread(ctx, fan, conv.ID)
	if err != nil || len(thread) == 0 {
		t.Fatalf("fan thread: %+v %v", thread, err)
	}
	last := thread[len(thread)-1]
	if !last.Locked || last.Body != "" || last.MediaID != "" || last.PriceMinor == nil {
		t.Fatalf("locked payload leaked: %+v", last)
	}
	creatorThread, err := h.svc.Thread(ctx, owner, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if creatorThread[len(creatorThread)-1].Body == "" {
		t.Fatal("creator cannot read own paid message")
	}

	// A non-entitled fan may request only when policy is everyone.
	other := h.account(t)
	if _, _, err := h.svc.Request(ctx, other, owner.UserID, "hello", ""); !errors.Is(err, ErrRequestPolicy) {
		t.Fatalf("default policy request = %v", err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE creator.profiles SET dm_request_policy='everyone' WHERE user_id=$1::uuid`, owner.UserID); err != nil {
		t.Fatal(err)
	}
	reqConv, reqMsg, err := h.svc.Request(ctx, other, owner.UserID, "request body", "")
	if err != nil {
		t.Fatal(err)
	}
	if reqConv.Status != "requested" || reqMsg.ID == "" {
		t.Fatalf("request = %+v %+v", reqConv, reqMsg)
	}
	if _, _, err := h.svc.Request(ctx, other, owner.UserID, "second", ""); !errors.Is(err, ErrRequestPending) {
		t.Fatalf("second request = %v", err)
	}
	accepted, err := h.svc.AcceptRequest(ctx, owner, reqConv.ID)
	if err != nil || accepted.Status != "accepted" {
		t.Fatalf("accept = %+v %v", accepted, err)
	}
	// Accepted request grants messaging without a subscription.
	if _, err := h.svc.Send(ctx, other, reqConv.ID, "thanks"); err != nil {
		t.Fatalf("accepted request send: %v", err)
	}
}

func TestRejectedRequestAttachmentPurgeMarker(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := h.creatorAccount(t)
	fan := h.account(t)
	if _, err := h.pool.Exec(ctx, `UPDATE creator.profiles SET dm_request_policy='everyone' WHERE user_id=$1::uuid`, owner.UserID); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("d", 64)
	var mediaID string
	if err := h.pool.(*pgxpool.Pool).QueryRow(ctx, `
		INSERT INTO media.objects (owner_id, kind, storage_key, content_type, byte_size, sha256)
		VALUES ($1::uuid, 'image', $2, 'image/png', 5, $2) RETURNING id::text`, fan.UserID, key).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	conv, msg, err := h.svc.Request(ctx, fan, owner.UserID, "look", mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.MediaID != "" {
		t.Fatal("attachment leaked before acceptance")
	}
	if _, err := h.svc.MessageMedia(ctx, owner, msg.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("pre-accept media = %v", err)
	}
	if _, err := h.svc.RejectRequest(ctx, owner, conv.ID); err != nil {
		t.Fatal(err)
	}
	var purge *string
	if err := h.pool.(*pgxpool.Pool).QueryRow(ctx, `
		SELECT purge_after::text FROM messaging.request_attachments WHERE message_id=$1::uuid`, msg.ID).Scan(&purge); err != nil {
		t.Fatal(err)
	}
	if purge == nil {
		t.Fatal("rejected attachment has no purge deadline")
	}
}

func TestMessagingSettingsValidationAndAudit(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := h.creatorAccount(t)
	if _, err := h.svc.UpdateSettings(ctx, owner, Settings{RequestPolicy: "everyone", PaidMessages: true, DefaultPriceMinor: func() *int64 { v := int64(500); return &v }(), Currency: "usd"}, ""); err != nil {
		t.Fatal(err)
	}
	got, err := h.svc.Settings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestPolicy != "everyone" || !got.PaidMessages || got.Currency != "USD" || got.DefaultPriceMinor == nil || *got.DefaultPriceMinor != 500 {
		t.Fatalf("settings = %+v", got)
	}
	bad := int64(50)
	if _, err := h.svc.UpdateSettings(ctx, owner, Settings{RequestPolicy: "everyone", DefaultPriceMinor: &bad, Currency: "USD"}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("low price = %v", err)
	}
}
