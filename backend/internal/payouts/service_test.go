package payouts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const goodPassword = "a-very-long-password"

type fixture struct {
	pool   *pgxpool.Pool
	svc    *Service
	ledger *ledger.Store
	ids    *identity.Service
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := dbtest.Pool(t)
	led := ledger.New(pool)
	f := &fixture{
		pool:   pool,
		ledger: led,
		ids:    identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams()))),
		now:    time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	f.svc = New(pool, led, WithClock(func() time.Time { return f.now }))
	return f
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func (f *fixture) account(t *testing.T) identity.Principal {
	t.Helper()
	res, err := f.ids.Register(context.Background(), "fan-"+randomHex(6)+"@example.test",
		goodPassword, identity.DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal
}

// creator registers an account and gives it a creator profile, which is what the
// payable account is keyed by.
func (f *fixture) creator(t *testing.T) identity.Principal {
	t.Helper()
	p := f.account(t)
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO creator.profiles (user_id, handle, display_name, bio, category, is_published)
		VALUES ($1::uuid, $2, 'Mira', 'Abstract work', 'art', true)`,
		p.UserID, "studio"+randomHex(4)); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return p
}

// credit posts money into the creator's payable account exactly as a settlement
// does, so the balance under test is the real one.
func (f *fixture) credit(t *testing.T, creatorID string, amount int64, simulated bool) {
	t.Helper()
	posting := ledger.Posting{
		Kind:      ledger.TxnSubscription,
		Reference: "test-credit-" + randomHex(8),
		Currency:  "USD",
		Simulated: simulated,
		Entries: []ledger.Entry{
			{AccountCode: ledger.AccountProviderClearing, AmountMinor: amount},
			{AccountCode: payableCode(creatorID), AmountMinor: -amount},
		},
	}
	if _, err := f.ledger.Post(context.Background(), posting); err != nil {
		t.Fatalf("credit: %v", err)
	}
}

func (f *fixture) balance(t *testing.T, creatorID string) Balance {
	t.Helper()
	b, err := f.svc.Balance(context.Background(), creatorID, "USD")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	return b
}

// reservedBy reports how much one ledger transaction moved into the payout
// clearing account. Balances are shared across the test database, so a payout
// test asserts on its own reference instead of a global total.
func (f *fixture) reservedBy(t *testing.T, reference string) int64 {
	t.Helper()
	var net int64
	if err := f.pool.QueryRow(context.Background(), `
		SELECT COALESCE(-SUM(e.amount_minor), 0)::bigint
		FROM ledger.entries e
		JOIN ledger.accounts a ON a.id = e.account_id
		JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE t.reference = $1 AND a.code = $2`,
		reference, ledger.AccountPayoutClearing).Scan(&net); err != nil {
		t.Fatalf("reserved by %q: %v", reference, err)
	}
	return net
}

func TestBalanceCountsTheLedgerAndKeepsTestMoneyOut(t *testing.T) {
	f := newFixture(t)
	creator := f.creator(t)

	if b := f.balance(t, creator.UserID); b.AvailableMinor != 0 || b.PendingMinor != 0 {
		t.Fatalf("new creator balance = %+v, want zeros", b)
	}

	f.credit(t, creator.UserID, 2500, false)
	f.credit(t, creator.UserID, 7000, true)

	b := f.balance(t, creator.UserID)
	if b.AvailableMinor != 2500 {
		t.Fatalf("available = %d, want 2500 (test money must not be payable)", b.AvailableMinor)
	}
	if b.MinimumMinor != MinAmountMinor {
		t.Fatalf("minimum = %d, want %d", b.MinimumMinor, MinAmountMinor)
	}
}

func TestRequestRefusesMoreThanTheBalanceAndWrongInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	f.credit(t, creator.UserID, 2500, false)

	if _, err := f.svc.Request(ctx, creator, Input{AmountMinor: 5000, Currency: "USD"}); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("over-balance request: want ErrInsufficientBalance, got %v", err)
	}
	if _, err := f.svc.Request(ctx, creator, Input{AmountMinor: 10, Currency: "USD"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("below-minimum request: want ErrInvalidInput, got %v", err)
	}
	if _, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2500, Currency: "EGP"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported currency: want ErrInvalidInput, got %v", err)
	}
	if _, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2500, Currency: "USD", Method: "carrier-pigeon"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown method: want ErrInvalidInput, got %v", err)
	}

	// A fan with no creator profile cannot create a payable balance by asking.
	fan := f.account(t)
	if _, err := f.svc.Request(ctx, fan, Input{AmountMinor: 2500, Currency: "USD"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("fan request: want ErrForbidden, got %v", err)
	}

	req, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD", Note: "rent"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.Status != StatusRequested || req.CreatorID != creator.UserID || req.AmountMinor != 2000 {
		t.Fatalf("unexpected request: %+v", req)
	}
	// Requesting does not reserve money: only approval moves the ledger.
	if b := f.balance(t, creator.UserID); b.AvailableMinor != 2500 || b.PendingMinor != 2000 {
		t.Fatalf("balance after request = %+v, want available 2500 and pending 2000", b)
	}

	// One open request per creator and currency, enforced by the database.
	if _, err := f.svc.Request(ctx, creator, Input{AmountMinor: 1000, Currency: "USD"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second open request: want ErrConflict, got %v", err)
	}
}

func TestApproveReservesTheMoneyOnceAndCannotBeRepeated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	finance := f.account(t)
	f.credit(t, creator.UserID, 2500, false)

	req, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	approved, err := f.svc.Approve(ctx, finance, req.ID, "checked", "")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != StatusApproved || approved.DecidedBy != finance.UserID {
		t.Fatalf("unexpected approval: %+v", approved)
	}

	// The reservation is a real ledger movement: the creator's payable drops and
	// the same amount waits in the payout clearing liability.
	available, err := f.ledger.Balance(ctx, payableCode(creator.UserID), "USD", false)
	if err != nil {
		t.Fatalf("payable balance: %v", err)
	}
	if available != 500 {
		t.Fatalf("payable after approval = %d, want 500", available)
	}
	if reserved := f.reservedBy(t, "payout:"+req.ID); reserved != 2000 {
		t.Fatalf("this payout reserved %d, want 2000", reserved)
	}

	// A retry of the same approval must not reserve the money twice.
	if _, err := f.svc.Approve(ctx, finance, req.ID, "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("second approval: want ErrConflict, got %v", err)
	}

	detail, err := f.svc.Detail(ctx, creator, req.ID, false)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(detail.Events) != 2 {
		t.Fatalf("events = %d, want 2 (request, approve)", len(detail.Events))
	}
	if detail.Events[1].Action != ActionApprove || detail.Events[1].ToStatus != StatusApproved {
		t.Fatalf("unexpected approval event: %+v", detail.Events[1])
	}
}

func TestApprovalFailsWhenTheBalanceWentAway(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	finance := f.account(t)
	f.credit(t, creator.UserID, 2500, false)

	req, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	// A refund lands after the request and before the decision.
	if _, err := f.ledger.Post(ctx, ledger.Posting{
		Kind:      ledger.TxnRefund,
		Reference: "test-refund-" + randomHex(8),
		Currency:  "USD",
		Entries: []ledger.Entry{
			{AccountCode: payableCode(creator.UserID), AmountMinor: 1800},
			{AccountCode: ledger.AccountProviderClearing, AmountMinor: -1800},
		},
	}); err != nil {
		t.Fatalf("refund: %v", err)
	}

	if _, err := f.svc.Approve(ctx, finance, req.ID, "", ""); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("approval after refund: want ErrInsufficientBalance, got %v", err)
	}
	// The refusal must leave nothing behind: the request is still requestable and
	// no reservation was posted.
	still, err := f.svc.Detail(ctx, creator, req.ID, false)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if still.Status != StatusRequested {
		t.Fatalf("status after refused approval = %q, want requested", still.Status)
	}
	if reserved := f.reservedBy(t, "payout:"+req.ID); reserved != 0 {
		t.Fatalf("a refused approval reserved %d", reserved)
	}
}

func TestRejectNeedsAReasonAndReversesAnApprovedReservation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	finance := f.account(t)
	f.credit(t, creator.UserID, 2500, false)

	first, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := f.svc.Reject(ctx, finance, first.ID, "no", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short note: want ErrInvalidInput, got %v", err)
	}
	if _, err := f.svc.Reject(ctx, finance, first.ID, "details were wrong", ""); err != nil {
		t.Fatalf("reject requested: %v", err)
	}

	// A rejected request is closed; the creator can ask again.
	second, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("second request after rejection: %v", err)
	}
	if _, err := f.svc.Approve(ctx, finance, second.ID, "", ""); err != nil {
		t.Fatalf("approve second: %v", err)
	}
	rejected, err := f.svc.Reject(ctx, finance, second.ID, "bank details were wrong", "")
	if err != nil {
		t.Fatalf("reject approved: %v", err)
	}
	if rejected.Status != StatusRejected {
		t.Fatalf("status = %q, want rejected", rejected.Status)
	}

	// The reversal is a compensating transaction, so the creator's money is back
	// and nothing is left in clearing.
	available, err := f.ledger.Balance(ctx, payableCode(creator.UserID), "USD", false)
	if err != nil {
		t.Fatalf("payable balance: %v", err)
	}
	if available != 2500 {
		t.Fatalf("payable after reversal = %d, want 2500", available)
	}
	// The reservation and its reversal net to zero for this request alone. A
	// database-wide clearing balance would include other tests' payouts.
	if net := f.reservedBy(t, "payout:"+second.ID) + f.reservedBy(t, "payout-reversal:"+second.ID); net != 0 {
		t.Fatalf("reservation and reversal net to %d, want 0", net)
	}
}

func TestCreatorCannotDecideTheirOwnPayout(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	f.credit(t, creator.UserID, 2500, false)

	req, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := f.svc.Approve(ctx, creator, req.ID, "", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self approval: want ErrForbidden, got %v", err)
	}
	if _, err := f.svc.Reject(ctx, creator, req.ID, "I changed my mind", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self rejection: want ErrForbidden, got %v", err)
	}
}

func TestCancelOnlyWorksBeforeADecisionAndOnlyForTheOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	other := f.creator(t)
	f.credit(t, creator.UserID, 2500, false)

	req, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := f.svc.Cancel(ctx, other, req.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cancel by another creator: want ErrForbidden, got %v", err)
	}
	cancelled, err := f.svc.Cancel(ctx, creator, req.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	if _, err := f.svc.Cancel(ctx, creator, req.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("second cancel: want ErrConflict, got %v", err)
	}
	if b := f.balance(t, creator.UserID); b.PendingMinor != 0 {
		t.Fatalf("pending after cancel = %d, want 0", b.PendingMinor)
	}
}

func TestPayoutTrailIsAppendOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	f.credit(t, creator.UserID, 2500, false)

	req, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `
		UPDATE payouts.request_events SET to_status = 'approved' WHERE request_id = $1::uuid`,
		req.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("event update: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		DELETE FROM payouts.request_events WHERE request_id = $1::uuid`, req.ID); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Fatalf("event delete: %v", err)
	}
}

func TestQueueShowsOpenRequestsAndDetailIsOwnerOrFinance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	stranger := f.creator(t)
	finance := f.account(t)
	f.credit(t, creator.UserID, 2500, false)

	req, err := f.svc.Request(ctx, creator, Input{AmountMinor: 2000, Currency: "USD"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	queue, err := f.svc.Queue(ctx, "", 0)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	found := false
	for _, q := range queue {
		if q.ID == req.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the open request is missing from the finance queue")
	}

	if _, err := f.svc.Detail(ctx, stranger, req.ID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger detail: want ErrForbidden, got %v", err)
	}
	if _, err := f.svc.Detail(ctx, finance, req.ID, true); err != nil {
		t.Fatalf("finance detail: %v", err)
	}

	// A decided request leaves the default queue but is still readable.
	if _, err := f.svc.Reject(ctx, finance, req.ID, "sanctions review", ""); err != nil {
		t.Fatalf("reject: %v", err)
	}
	queue, err = f.svc.Queue(ctx, "", 0)
	if err != nil {
		t.Fatalf("queue after decision: %v", err)
	}
	for _, q := range queue {
		if q.ID == req.ID {
			t.Fatal("a decided request is still in the open queue")
		}
	}
}
