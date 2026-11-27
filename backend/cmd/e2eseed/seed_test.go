package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/earnings"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/payouts"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const seedPassword = "a-very-long-password"

func containsRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

// TestSeedMatchesTheLedger is the seed's own contract: the numbers it hands the
// device tests must be the numbers the services actually return, and the fixture
// must be refusable in every direction a payout could go wrong.
func TestSeedMatchesTheLedger(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	fixture, err := Seed(ctx, pool, Options{Password: seedPassword, BaseURL: "http://10.0.2.2:8080"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	led := ledger.New(pool)
	payoutSvc := payouts.New(pool, led)
	earner := earnings.New(pool)

	// 0. The seeded accounts hold the roles the fixture promises. Creating a profile
	// through the service does not grant the creator role — the API does that — so a
	// seed that skipped it would leave the creator a fan at every creator route.
	roles, err := identity.New(pool).RolesOf(ctx, fixture.CreatorID)
	if err != nil {
		t.Fatalf("read creator roles: %v", err)
	}
	if !containsRole(roles, identity.RoleCreator) {
		t.Fatalf("the seeded creator's roles are %v, want creator", roles)
	}
	financeRoles, err := identity.New(pool).RolesOf(ctx, fixture.FinanceID)
	if err != nil {
		t.Fatalf("read finance roles: %v", err)
	}
	if !containsRole(financeRoles, identity.RoleFinance) {
		t.Fatalf("the seeded finance account's roles are %v, want finance", financeRoles)
	}

	// 1. The balance the fixture reports is the balance the payout service reports.
	bal, err := payoutSvc.Balance(ctx, fixture.CreatorID, fixture.Currency)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if bal.AvailableMinor != fixture.AvailableMinor {
		t.Fatalf("service available = %d, fixture says %d", bal.AvailableMinor, fixture.AvailableMinor)
	}
	if bal.PendingMinor != fixture.PendingMinor || bal.MinimumMinor != payouts.MinAmountMinor {
		t.Fatalf("unexpected balance: %+v", bal)
	}

	// 2. The ledger agrees, and test money sits outside the payable figure.
	live, err := led.Balance(ctx, payableCode(fixture.CreatorID), fixture.Currency, false)
	if err != nil {
		t.Fatalf("ledger live balance: %v", err)
	}
	if live != fixture.AvailableMinor {
		t.Fatalf("ledger live = %d, fixture says %d", live, fixture.AvailableMinor)
	}
	withTest, err := led.Balance(ctx, payableCode(fixture.CreatorID), fixture.Currency, true)
	if err != nil {
		t.Fatalf("ledger balance with test money: %v", err)
	}
	if withTest-live != fixture.SimulatedEarningsMinor {
		t.Fatalf("test money = %d, fixture says %d", withTest-live, fixture.SimulatedEarningsMinor)
	}
	if fixture.SimulatedEarningsMinor <= 0 {
		t.Fatal("the fixture did not create any simulated money")
	}

	// 3. The reservation is one balanced ledger transaction of the approved amount.
	var reserved int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(-SUM(e.amount_minor), 0)::bigint
		FROM ledger.entries e
		JOIN ledger.accounts a ON a.id = e.account_id
		JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE t.reference = $1 AND a.code = $2`,
		"payout:"+fixture.PayoutRequestID, ledger.AccountPayoutClearing).Scan(&reserved); err != nil {
		t.Fatalf("read the reservation: %v", err)
	}
	if reserved != fixture.ApprovedPayoutMinor {
		t.Fatalf("reserved %d, fixture says %d", reserved, fixture.ApprovedPayoutMinor)
	}

	// 4. The approved request left its trail behind, and no decision is open.
	detail, err := payoutSvc.Detail(ctx, identity.Principal{UserID: fixture.CreatorID},
		fixture.PayoutRequestID, false)
	if err != nil {
		t.Fatalf("read the seeded payout: %v", err)
	}
	if detail.Status != payouts.StatusApproved {
		t.Fatalf("seeded payout status = %q, want approved", detail.Status)
	}
	if len(detail.Events) != 2 || detail.Events[0].Action != payouts.ActionRequest ||
		detail.Events[1].Action != payouts.ActionApprove {
		t.Fatalf("unexpected payout trail: %+v", detail.Events)
	}

	// 5. The statement matches: two live rows (the tip and the reservation), test
	// money excluded and counted as excluded.
	now := time.Now().UTC()
	window := earnings.ExportQuery{
		CreatorID: fixture.CreatorID,
		Currency:  fixture.Currency,
		From:      time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC),
		To:        now.Add(time.Minute),
	}
	exp, err := earner.Export(ctx, window)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(exp.Rows) != fixture.ExportRows || exp.NetMinor != fixture.ExportNetMinor ||
		exp.GrossMinor != fixture.ExportGrossMinor {
		t.Fatalf("export does not match the fixture: %+v", exp)
	}
	if exp.ExcludedSimulated != 1 {
		t.Fatalf("excluded simulated rows = %d, want 1", exp.ExcludedSimulated)
	}
	for _, row := range exp.Rows {
		if row.Simulated {
			t.Fatalf("the default export carried a test row: %+v", row)
		}
	}

	// The same export asked for test money includes it and stops excluding it.
	window.IncludeSimulated = true
	withTestExp, err := earner.Export(ctx, window)
	if err != nil {
		t.Fatalf("export with test money: %v", err)
	}
	if len(withTestExp.Rows) != len(exp.Rows)+1 || withTestExp.ExcludedSimulated != 0 {
		t.Fatalf("export including test money is wrong: %+v", withTestExp)
	}
	if withTestExp.NetMinor != exp.NetMinor+fixture.SimulatedEarningsMinor {
		t.Fatalf("test money was not added to the net: %d", withTestExp.NetMinor)
	}

	// Currency consistency: a second currency has no rows, and converting the USD
	// balance into it is not something the export will do.
	eur := window
	eur.Currency = "EUR"
	eur.IncludeSimulated = false
	eurExp, err := earner.Export(ctx, eur)
	if err != nil {
		t.Fatalf("eur export: %v", err)
	}
	if len(eurExp.Rows) != 0 || eurExp.NetMinor != 0 {
		t.Fatalf("the EUR export carried USD money: %+v", eurExp)
	}

	// 6. Authorization, one refusal at a time. None of these may move money.
	creatorP := identity.Principal{UserID: fixture.CreatorID}
	fanP := identity.Principal{UserID: fixture.FanID}
	financeP := identity.Principal{UserID: fixture.FinanceID, Roles: []string{identity.RoleFinance}}

	// A fan has no creator profile, so there is no payable account to spend.
	if _, err := payoutSvc.Request(ctx, fanP, payouts.Input{AmountMinor: 1000, Currency: "USD"}); !errors.Is(err, payouts.ErrForbidden) {
		t.Fatalf("fan request: want ErrForbidden, got %v", err)
	}
	// A creator cannot decide their own payout.
	if _, err := payoutSvc.Approve(ctx, creatorP, fixture.PayoutRequestID, "", ""); !errors.Is(err, payouts.ErrForbidden) {
		t.Fatalf("self approval: want ErrForbidden, got %v", err)
	}
	// The seeded request is already approved, so a second decision is a conflict.
	if _, err := payoutSvc.Approve(ctx, financeP, fixture.PayoutRequestID, "", ""); !errors.Is(err, payouts.ErrConflict) {
		t.Fatalf("second approval: want ErrConflict, got %v", err)
	}
	// More than the live balance is refused, and so is spending a USD balance in EUR.
	if _, err := payoutSvc.Request(ctx, creatorP, payouts.Input{
		AmountMinor: fixture.AvailableMinor + 1000, Currency: "USD",
	}); !errors.Is(err, payouts.ErrInsufficientBalance) {
		t.Fatalf("over-balance request: want ErrInsufficientBalance, got %v", err)
	}
	if _, err := payoutSvc.Request(ctx, creatorP, payouts.Input{
		AmountMinor: 1000, Currency: "EUR",
	}); !errors.Is(err, payouts.ErrInsufficientBalance) {
		t.Fatalf("EUR request against a USD balance: want ErrInsufficientBalance, got %v", err)
	}
	// Someone else's statement is not readable by another creator.
	if _, err := payoutSvc.Detail(ctx, identity.Principal{UserID: fixture.FanID},
		fixture.PayoutRequestID, false); !errors.Is(err, payouts.ErrForbidden) {
		t.Fatalf("stranger detail: want ErrForbidden, got %v", err)
	}
}

// TestSeededRequestAndApprovalReserveExactlyOnce drives a second, real request all
// the way to approval, so the seeded reservation is not the only evidence that the
// path works.
func TestSeededRequestAndApprovalReserveExactlyOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	fixture, err := Seed(ctx, pool, Options{Password: seedPassword})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	led := ledger.New(pool)
	payoutSvc := payouts.New(pool, led)

	const amount = int64(1500)
	req, err := payoutSvc.Request(ctx, identity.Principal{UserID: fixture.CreatorID}, payouts.Input{
		AmountMinor: amount, Currency: fixture.Currency, Method: payouts.MethodWallet,
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	financeP := identity.Principal{UserID: fixture.FinanceID, Roles: []string{identity.RoleFinance}}
	if _, err := payoutSvc.Approve(ctx, financeP, req.ID, "checked", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}

	after, err := payoutSvc.Balance(ctx, fixture.CreatorID, fixture.Currency)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if after.AvailableMinor != fixture.AvailableMinor-amount {
		t.Fatalf("available = %d, want %d", after.AvailableMinor, fixture.AvailableMinor-amount)
	}
	if after.PendingMinor != 0 {
		t.Fatalf("pending after approval = %d, want 0", after.PendingMinor)
	}

	// The second reservation is its own ledger transaction, and replaying the same
	// approval does not reserve again.
	var reserved int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(-SUM(e.amount_minor), 0)::bigint
		FROM ledger.entries e
		JOIN ledger.accounts a ON a.id = e.account_id
		JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE t.reference = $1 AND a.code = $2`,
		"payout:"+req.ID, ledger.AccountPayoutClearing).Scan(&reserved); err != nil {
		t.Fatalf("read reservation: %v", err)
	}
	if reserved != amount {
		t.Fatalf("reserved %d, want %d", reserved, amount)
	}
	if _, err := payoutSvc.Approve(ctx, financeP, req.ID, "", ""); !errors.Is(err, payouts.ErrConflict) {
		t.Fatalf("replay: want ErrConflict, got %v", err)
	}
	again, err := payoutSvc.Balance(ctx, fixture.CreatorID, fixture.Currency)
	if err != nil {
		t.Fatalf("balance after replay: %v", err)
	}
	if again.AvailableMinor != after.AvailableMinor {
		t.Fatalf("a refused replay moved money: %d -> %d", after.AvailableMinor, again.AvailableMinor)
	}

	// The statement now carries the second reservation as a third live row.
	now := time.Now().UTC()
	exp, err := earnings.New(pool).Export(ctx, earnings.ExportQuery{
		CreatorID: fixture.CreatorID, Currency: fixture.Currency,
		From: time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC), To: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(exp.Rows) != fixture.ExportRows+1 || exp.NetMinor != fixture.ExportNetMinor-amount {
		t.Fatalf("export after the second payout is wrong: %+v", exp)
	}
}
