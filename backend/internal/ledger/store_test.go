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

	balances, err := store.Balances(ctx, "USD")
	if err != nil {
		t.Fatalf("balances: %v", err)
	}
	seen := map[string]int64{}
	for _, b := range balances {
		seen[b.Code] = b.AmountMinor
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
