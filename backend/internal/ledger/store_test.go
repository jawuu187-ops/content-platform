package ledger

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func balancedPost(ref string) Posting {
	return Posting{
		Kind:      TxnSubscription,
		Reference: ref,
		Currency:  "USD",
		Memo:      "test",
		Entries: []Entry{
			{AccountCode: AccountProviderClearing, AmountMinor: 1200},
			{AccountCode: AccountRevenue, AmountMinor: -240},
			{AccountCode: "creator:00000000-0000-4000-8000-000000000042:payable", AmountMinor: -960},
		},
	}
}

func TestPostIsBalancedAndIdempotent(t *testing.T) {
	pool := dbtest.Pool(t)
	store := New(pool)
	ctx := context.Background()

	if _, err := store.Post(ctx, balancedPost("intent-1")); err != nil {
		t.Fatalf("post: %v", err)
	}
	// The same settlement again must not double-post.
	if _, err := store.Post(ctx, balancedPost("intent-1")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate post: want ErrDuplicate, got %v", err)
	}

	net, _, err := store.TrialBalance(ctx, "USD")
	if err != nil {
		t.Fatalf("trial balance: %v", err)
	}
	if net != 0 {
		t.Fatalf("ledger is not balanced: net %d", net)
	}
	// Exactly three legs, summing to zero. The trial balance itself is shared with
	// other packages running against the same database, so its volume is not
	// asserted here.
	var legs int
	var sum int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(e.amount_minor), 0)
		FROM ledger.entries e JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE t.reference = 'intent-1'`).Scan(&legs, &sum); err != nil {
		t.Fatalf("count legs: %v", err)
	}
	if legs != 3 || sum != 0 {
		t.Fatalf("legs=%d sum=%d", legs, sum)
	}

	// Balances are shared across the database, so the per-transaction split is
	// what this test asserts. The aggregate reads are covered by the service tests.
	rows, err := pool.Query(ctx, `
		SELECT a.code, sum(e.amount_minor)::bigint
		FROM ledger.entries e
		JOIN ledger.accounts a ON a.id = e.account_id
		JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE t.reference = 'intent-1'
		GROUP BY a.code`)
	if err != nil {
		t.Fatalf("per-reference balances: %v", err)
	}
	defer rows.Close()
	seen := map[string]int64{}
	for rows.Next() {
		var code string
		var amount int64
		if err := rows.Scan(&code, &amount); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen[code] = amount
	}
	if seen[AccountProviderClearing] != 1200 || seen[AccountRevenue] != -240 {
		t.Fatalf("unexpected balances: %+v", seen)
	}
}

func TestUnbalancedPostingIsRefused(t *testing.T) {
	store := New(dbtest.Pool(t))
	bad := balancedPost("intent-2")
	bad.Entries[1].AmountMinor = -1000 // no longer sums to zero

	if _, err := store.Post(context.Background(), bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestDatabaseRejectsUnbalancedAndMutatedLedgerRows(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	// A single-leg transaction inserted directly: the deferred constraint trigger
	// must refuse it at commit.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	var txnID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO ledger.transactions (kind, reference, currency)
		VALUES ('adjustment', 'raw-unbalanced', 'USD') RETURNING id::text`).Scan(&txnID); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	var accountID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO ledger.accounts (code, kind, owner_type, currency)
		VALUES ('platform:raw_test', 'asset', 'platform', 'USD')
		ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code RETURNING id::text`).Scan(&accountID); err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger.entries (transaction_id, account_id, amount_minor) VALUES ($1::uuid, $2::uuid, 500)`,
		txnID, accountID); err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("an unbalanced transaction was committed")
	} else if !strings.Contains(err.Error(), "unbalanced") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now a real, balanced transaction, then attempts to rewrite history.
	store := New(pool)
	if _, err := store.Post(ctx, balancedPost("intent-3")); err != nil {
		t.Fatalf("post: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE ledger.entries SET amount_minor = amount_minor + 1`); err == nil {
		t.Fatal("an entry was updated")
	} else if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("unexpected update error: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ledger.transactions`); err == nil {
		t.Fatal("a transaction was deleted")
	} else if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("unexpected delete error: %v", err)
	}
}

// TestPostTxMovesOnlyWhenTheCallerCommits pins the contract a payout depends on:
// the ledger legs and the row they pay for share one transaction, so a rollback
// leaves neither behind and a commit leaves both.
func TestPostTxMovesOnlyWhenTheCallerCommits(t *testing.T) {
	pool := dbtest.Pool(t)
	store := New(pool)
	ctx := context.Background()
	posting := balancedPost("tx-owned-1")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := store.PostTx(ctx, tx, posting); err != nil {
		t.Fatalf("post tx: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var rolledBack int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int FROM ledger.transactions WHERE reference = 'tx-owned-1'`).Scan(&rolledBack); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	// The insert is atomic: a rolled-back transaction cannot leave a transaction
	// row whose entries never arrived.
	if rolledBack != 0 {
		t.Fatalf("rolled-back posting survived: %d rows", rolledBack)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	txnID, err := store.PostTx(ctx, tx, posting)
	if err != nil {
		t.Fatalf("post tx: %v", err)
	}
	// The same reference inside the same transaction is still caught by the
	// database's unique constraint, not by the caller remembering to check.
	if _, err := store.PostTx(ctx, tx, posting); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate inside tx: want ErrDuplicate, got %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var legs int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int FROM ledger.entries WHERE transaction_id = $1::uuid`, txnID).Scan(&legs); err != nil {
		t.Fatalf("count legs: %v", err)
	}
	if legs != 3 {
		t.Fatalf("legs=%d, want 3", legs)
	}
}

func TestPostTxRefusesAnInvalidPostingBeforeTouchingTheDatabase(t *testing.T) {
	store := New(dbtest.Pool(t))
	ctx := context.Background()
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bad := balancedPost("tx-invalid-1")
	bad.Entries[0].AmountMinor = 0 // a zero leg

	if _, err := store.PostTx(ctx, tx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

// TestBalanceCountsCreditsPositiveAndKeepsTestMoneyApart pins the two properties
// a payout relies on: the sign convention (owed money reads positive) and the
// data-class split (test money is never available to pay out).
func TestBalanceCountsCreditsPositiveAndKeepsTestMoneyApart(t *testing.T) {
	pool := dbtest.Pool(t)
	store := New(pool)
	ctx := context.Background()

	const code = "creator:00000000-0000-4000-8000-0000000000b1:payable"

	zero, err := store.Balance(ctx, code, "USD", false)
	if err != nil {
		t.Fatalf("balance of an unused account: %v", err)
	}
	if zero != 0 {
		t.Fatalf("unused account balance = %d, want 0", zero)
	}

	live := balancedPost("balance-live-1")
	live.Entries = []Entry{
		{AccountCode: AccountProviderClearing, AmountMinor: 1200},
		{AccountCode: code, AmountMinor: -960},
		{AccountCode: AccountRevenue, AmountMinor: -240},
	}
	if _, err := store.Post(ctx, live); err != nil {
		t.Fatalf("post live: %v", err)
	}

	got, err := store.Balance(ctx, code, "USD", false)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	// The entry is a credit of 960, stored as -960, and the balance reports it
	// as +960: what the platform owes the creator.
	if got != 960 {
		t.Fatalf("live balance = %d, want 960", got)
	}

	simulated := live
	simulated.Reference = "balance-simulated-1"
	simulated.Simulated = true
	if _, err := store.Post(ctx, simulated); err != nil {
		t.Fatalf("post simulated: %v", err)
	}

	liveOnly, err := store.Balance(ctx, code, "USD", false)
	if err != nil {
		t.Fatalf("balance live only: %v", err)
	}
	if liveOnly != 960 {
		t.Fatalf("balance excluding test money = %d, want 960", liveOnly)
	}
	withTest, err := store.Balance(ctx, code, "USD", true)
	if err != nil {
		t.Fatalf("balance with test data: %v", err)
	}
	if withTest != 1920 {
		t.Fatalf("balance including test money = %d, want 1920", withTest)
	}

	// A debit outweighing the credit reads negative rather than wrapping: the
	// arithmetic a payout refusal depends on is plain integer arithmetic.
	refunded := live
	refunded.Kind = TxnRefund
	refunded.Reference = "balance-refund-1"
	refunded.Entries = []Entry{
		{AccountCode: code, AmountMinor: 2000},
		{AccountCode: AccountProviderClearing, AmountMinor: -2000},
	}
	if _, err := store.Post(ctx, refunded); err != nil {
		t.Fatalf("post refund: %v", err)
	}
	afterRefund, err := store.Balance(ctx, code, "USD", false)
	if err != nil {
		t.Fatalf("balance after refund: %v", err)
	}
	if afterRefund != -1040 {
		t.Fatalf("balance after over-refund = %d, want -1040", afterRefund)
	}
}

// TestBalanceTxSeesUncommittedEntries proves the in-transaction read used before
// a payout posts. A separate connection must not see the pending entry, so the
// same-transaction call is what makes the check and the posting one decision.
func TestBalanceTxSeesUncommittedEntries(t *testing.T) {
	pool := dbtest.Pool(t)
	store := New(pool)
	ctx := context.Background()
	const code = "creator:00000000-0000-4000-8000-0000000000b2:payable"

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p := balancedPost("balance-tx-1")
	p.Entries = []Entry{
		{AccountCode: AccountProviderClearing, AmountMinor: 500},
		{AccountCode: code, AmountMinor: -500},
	}
	if _, err := store.PostTx(ctx, tx, p); err != nil {
		t.Fatalf("post tx: %v", err)
	}

	inTx, err := store.BalanceTx(ctx, tx, code, "USD", false)
	if err != nil {
		t.Fatalf("balance tx: %v", err)
	}
	if inTx != 500 {
		t.Fatalf("in-transaction balance = %d, want 500", inTx)
	}
	outsideTx, err := store.Balance(ctx, code, "USD", false)
	if err != nil {
		t.Fatalf("balance outside tx: %v", err)
	}
	if outsideTx != 0 {
		t.Fatalf("uncommitted money was visible to another connection: %d", outsideTx)
	}
}
