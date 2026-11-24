package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

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
