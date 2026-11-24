package moderation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/messaging"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

// A report is only useful if it is trustworthy: it must not be forgeable against content
// the reporter cannot see, it must not multiply when submitted twice, and its history must
// be impossible to rewrite. These tests cover exactly those three properties.

type modGate struct{}

func (modGate) AllowAdult(context.Context, string) (bool, error) { return true, nil }

type modHarness struct {
	reports *Service
	ident   *identity.Service
	creators *creator.Service
	dm      *messaging.Service
	pool    *pgxpool.Pool
}

func newModHarness(t *testing.T) *modHarness {
	t.Helper()
	pool := dbtest.Pool(t)
	return &modHarness{
		reports:  New(pool),
		ident:    identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams()))),
		creators: creator.New(pool, creator.WithAgeGate(modGate{}), creator.WithFakePayments(true)),
		dm:       messaging.New(pool),
		pool:     pool,
	}
}

func suffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(buf)
}

func (h *modHarness) account(t *testing.T) identity.Principal {
	t.Helper()
	res, err := h.ident.Register(context.Background(),
		"mod-"+suffix(t)+"@example.test", "a-very-long-password",
		identity.DeviceInfo{Fingerprint: "mod-device"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal
}

func (h *modHarness) creatorWithPost(t *testing.T) (identity.Principal, string) {
	t.Helper()
	p := h.account(t)
	ctx := context.Background()
	if _, err := h.creators.CreateProfile(ctx, p, creator.ProfileInput{
		Handle: "mod" + suffix(t), DisplayName: "Mod Subject", Category: "art",
	}, ""); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	publish := true
	post, err := h.creators.CreatePost(ctx, p, creator.PostInput{
		Body: "a public post", Visibility: "public", Publish: &publish,
	}, "")
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	return p, post.ID
}

func TestReportLifecycleIsAuditedAndCannotBeRewritten(t *testing.T) {
	h := newModHarness(t)
	ctx := context.Background()
	reporter := h.account(t)
	_, postID := h.creatorWithPost(t)

	rep, err := h.reports.Submit(ctx, reporter, Input{
		SubjectType: SubjectPost,
		SubjectID:   postID,
		Reason:      ReasonNonConsensual,
		Details:     "this was posted without permission",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if rep.Status != StatusOpen || rep.Duplicate {
		t.Fatalf("unexpected new report: %+v", rep)
	}

	// The same complaint twice is one row, and the caller is told so.
	again, err := h.reports.Submit(ctx, reporter, Input{
		SubjectType: SubjectPost, SubjectID: postID, Reason: ReasonNonConsensual,
	})
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if !again.Duplicate || again.ID != rep.ID {
		t.Fatalf("duplicate submission made a new report: %+v", again)
	}

	moderator := h.account(t)
	reviewed, err := h.reports.Transition(ctx, moderator, rep.ID, ActionStartReview, "")
	if err != nil {
		t.Fatalf("start review: %v", err)
	}
	if reviewed.Status != StatusReviewing {
		t.Fatalf("status after start_review: %s", reviewed.Status)
	}

	closed, err := h.reports.Transition(ctx, moderator, rep.ID, ActionAction,
		"removed and the uploader was warned")
	if err != nil {
		t.Fatalf("action: %v", err)
	}
	if closed.Status != StatusActioned || closed.ResolvedAt == nil {
		t.Fatalf("closing a report must stamp it: %+v", closed)
	}
	if closed.ResolvedBy != moderator.UserID {
		t.Fatalf("resolved_by should name the moderator, got %q", closed.ResolvedBy)
	}

	// Every step is on the record, oldest first, with who and why.
	want := []struct{ action, from, to string }{
		{"", "", StatusOpen},
		{ActionStartReview, StatusOpen, StatusReviewing},
		{ActionAction, StatusReviewing, StatusActioned},
	}
	if len(closed.Events) != len(want) {
		t.Fatalf("expected %d events, got %d: %+v", len(want), len(closed.Events), closed.Events)
	}
	for i, w := range want {
		got := closed.Events[i]
		if w.action != "" && got.Action != w.action {
			t.Fatalf("event %d action: want %q, got %q", i, w.action, got.Action)
		}
		if got.FromStatus != w.from || got.ToStatus != w.to {
			t.Fatalf("event %d transition: want %q->%q, got %q->%q", i, w.from, w.to,
				got.FromStatus, got.ToStatus)
		}
	}

	// An appeal moves it back to review and clears the closure stamp.
	reopened, err := h.reports.Transition(ctx, moderator, rep.ID, ActionReopen, "the uploader appealed")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Status != StatusReviewing || reopened.ResolvedAt != nil {
		t.Fatalf("reopen did not clear the closure: %+v", reopened)
	}

	// The database refuses to alter the trail, so "who decided this" stays answerable.
	if _, err := h.pool.Exec(ctx,
		`UPDATE moderation.report_events SET note = 'rewritten' WHERE report_id = $1::uuid`,
		rep.ID); err == nil {
		t.Fatal("report events were editable; the audit trail is not evidence")
	}
	if _, err := h.pool.Exec(ctx,
		`DELETE FROM moderation.report_events WHERE report_id = $1::uuid`, rep.ID); err == nil {
		t.Fatal("report events were deletable; the audit trail is not evidence")
	}
}

func TestIllegalTransitionsAndNotesAreRefused(t *testing.T) {
	h := newModHarness(t)
	ctx := context.Background()
	reporter := h.account(t)
	_, postID := h.creatorWithPost(t)
	rep, err := h.reports.Submit(ctx, reporter, Input{
		SubjectType: SubjectPost, SubjectID: postID, Reason: ReasonSpam,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	moderator := h.account(t)

	// A decision without a reason is not a decision.
	if _, err := h.reports.Transition(ctx, moderator, rep.ID, ActionDismiss, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dismiss without a note: want ErrInvalidInput, got %v", err)
	}
	// Reviewing twice makes no sense.
	if _, err := h.reports.Transition(ctx, moderator, rep.ID, ActionStartReview, ""); err != nil {
		t.Fatalf("start review: %v", err)
	}
	if _, err := h.reports.Transition(ctx, moderator, rep.ID, ActionStartReview, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("second start_review: want ErrConflict, got %v", err)
	}
	// Unknown verbs are refused rather than silently ignored.
	if _, err := h.reports.Transition(ctx, moderator, rep.ID, "delete_everything", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("unknown action: want ErrConflict, got %v", err)
	}
}

func TestOnlyParticipantsCanReportAMessage(t *testing.T) {
	h := newModHarness(t)
	ctx := context.Background()
	fan := h.account(t)
	creatorPrincipal := h.account(t)
	if _, err := h.creators.CreateProfile(ctx, creatorPrincipal, creator.ProfileInput{
		Handle: "dm" + suffix(t), DisplayName: "DM Subject", Category: "art",
	}, ""); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	// Messaging requires a live membership; grant one the way support does.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'admin', now() + interval '1 hour')`,
		fan.UserID, creatorPrincipal.UserID); err != nil {
		t.Fatalf("grant membership: %v", err)
	}
	conv, err := h.dm.Open(ctx, fan, creatorPrincipal.UserID)
	if err != nil {
		t.Fatalf("open conversation: %v", err)
	}
	msg, err := h.dm.Send(ctx, creatorPrincipal, conv.ID, "a message between two people")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	outsider := h.account(t)
	if _, err := h.reports.Submit(ctx, outsider, Input{
		SubjectType: SubjectMessage, SubjectID: msg.ID, Reason: ReasonHarassment,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider reporting a private message: want ErrNotFound, got %v", err)
	}

	// Both participants may report it.
	for _, p := range []identity.Principal{fan, creatorPrincipal} {
		if _, err := h.reports.Submit(ctx, p, Input{
			SubjectType: SubjectMessage, SubjectID: msg.ID, Reason: ReasonHarassment,
		}); err != nil {
			t.Fatalf("participant report: %v", err)
		}
	}
}

func TestReportsAreScopedToTheirReporter(t *testing.T) {
	h := newModHarness(t)
	ctx := context.Background()
	reporter := h.account(t)
	other := h.account(t)
	_, postID := h.creatorWithPost(t)
	rep, err := h.reports.Submit(ctx, reporter, Input{
		SubjectType: SubjectPost, SubjectID: postID, Reason: ReasonOther, Details: "private words",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	if _, err := h.reports.Detail(ctx, other, rep.ID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("another person read the report: %v", err)
	}
	mine, err := h.reports.Mine(ctx, reporter)
	if err != nil || len(mine) == 0 {
		t.Fatalf("own reports: %v %+v", err, mine)
	}
	if theirs, err := h.reports.Mine(ctx, other); err != nil || len(theirs) != 0 {
		t.Fatalf("other person's list should be empty: %v %+v", err, theirs)
	}
}
