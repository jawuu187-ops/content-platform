package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store posts and reads ledger transactions.
type Store struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	now    func() time.Time
}

// Option configures the store.
type Option func(*Store)

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Store) { s.logger = l } }

// WithClock overrides the clock for tests.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// New builds the store.
func New(pool *pgxpool.Pool, opts ...Option) *Store {
	s := &Store{pool: pool, logger: slog.Default(), now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// validatePosting checks everything that can be checked without touching the
// database. It is pure, so it runs once at the edge of Post and again inside
// PostTx, where a caller-supplied transaction may have skipped it.
func validatePosting(p Posting) error {
	if len(p.Entries) < 2 {
		return fmt.Errorf("%w: a transaction needs at least two legs", ErrInvalid)
	}
	var sum int64
	for _, e := range p.Entries {
		if e.AccountCode == "" {
			return fmt.Errorf("%w: an entry has no account", ErrInvalid)
		}
		if e.AmountMinor == 0 {
			return fmt.Errorf("%w: an entry has a zero amount", ErrInvalid)
		}
		sum += e.AmountMinor
	}
	if sum != 0 {
		return fmt.Errorf("%w: entries sum to %d, not zero", ErrInvalid, sum)
	}
	if p.Kind == "" || p.Reference == "" || len(p.Currency) != 3 {
		return fmt.Errorf("%w: kind, reference and currency are required", ErrInvalid)
	}
	return nil
}

// Post records a balanced transaction. It is idempotent on (kind, reference):
// posting the same settlement twice is a no-op that reports ErrDuplicate, so a
// caller can tell "already applied" from "applied now" without counting rows.
func (s *Store) Post(ctx context.Context, p Posting) (string, error) {
	if err := validatePosting(p); err != nil {
		return "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("ledger: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txnID, err := s.PostTx(ctx, tx, p)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("ledger: commit: %w", err)
	}
	return txnID, nil
}

// PostTx records a balanced transaction inside a transaction the caller already
// owns, so money can move in the same unit of work as the state change it pays
// for: a payout row and its ledger legs commit together or not at all.
//
// It does not commit or roll back. The caller decides. The deferred balance
// trigger in the database still refuses an unbalanced transaction at commit, so
// a PostTx that is rolled back after an error leaves nothing behind.
func (s *Store) PostTx(ctx context.Context, tx pgx.Tx, p Posting) (string, error) {
	if err := validatePosting(p); err != nil {
		return "", err
	}

	var txnID string
	err := tx.QueryRow(ctx, `
		INSERT INTO ledger.transactions (kind, reference, currency, memo, simulated)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (kind, reference) DO NOTHING
		RETURNING id::text`, p.Kind, p.Reference, p.Currency, p.Memo, p.Simulated).Scan(&txnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDuplicate
	}
	if err != nil {
		return "", fmt.Errorf("ledger: insert transaction: %w", err)
	}

	for _, e := range p.Entries {
		accountID, err := ensureAccount(ctx, tx, e.AccountCode, p.Currency)
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger.entries (transaction_id, account_id, amount_minor)
			VALUES ($1::uuid, $2::uuid, $3)`, txnID, accountID, e.AmountMinor); err != nil {
			return "", fmt.Errorf("ledger: insert entry: %w", err)
		}
	}
	return txnID, nil
}

// ensureAccount creates the account on first use. The code carries its kind and
// owner so a posting reads unambiguously in the statement.
func ensureAccount(ctx context.Context, tx pgx.Tx, code, currency string) (string, error) {
	kind, ownerType, ownerID := classify(code)
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO ledger.accounts (code, kind, owner_type, owner_id, currency)
		VALUES ($1, $2, $3, $4::uuid, $5)
		ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code
		RETURNING id::text`, code, kind, ownerType, ownerID, currency).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("ledger: ensure account %q: %w", code, err)
	}
	return id, nil
}

// classify derives account metadata from a code such as
// "creator:<uuid>:payable" or "platform:revenue".
func classify(code string) (kind, ownerType string, ownerID *string) {
	switch {
	case hasPrefix(code, "platform:provider_clearing"):
		return KindAsset, OwnerPlatform, nil
	case hasPrefix(code, "platform:revenue"):
		return KindRevenue, OwnerPlatform, nil
	case hasPrefix(code, "platform:refund_expense"):
		return KindExpense, OwnerPlatform, nil
	case hasPrefix(code, "platform:payout_clearing"):
		// Money approved for withdrawal is still owed, so it is a liability and
		// not an asset even though it sits next to the clearing account.
		return KindLiability, OwnerPlatform, nil
	case hasPrefix(code, "creator:"):
		if parts := splitN(code, ':', 3); len(parts) == 3 {
			id := parts[1]
			return KindLiability, OwnerCreator, &id
		}
		return KindLiability, OwnerCreator, nil
	case hasPrefix(code, "user:"):
		if parts := splitN(code, ':', 3); len(parts) == 3 {
			id := parts[1]
			return KindLiability, OwnerUser, &id
		}
		return KindLiability, OwnerUser, nil
	default:
		return KindAsset, OwnerPlatform, nil
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func splitN(s string, sep byte, n int) []string {
	out := make([]string, 0, n)
	start := 0
	for i := 0; i < len(s) && len(out) < n-1; i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// Balances returns the summed balance per account code for a currency.
func (s *Store) Balances(ctx context.Context, currency string) ([]Balance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.code, a.currency, COALESCE(sum(e.amount_minor), 0)::bigint
		FROM ledger.accounts a
		LEFT JOIN ledger.entries e ON e.account_id = a.id
		WHERE a.currency = $1
		GROUP BY a.code, a.currency
		ORDER BY a.code`, currency)
	if err != nil {
		return nil, fmt.Errorf("ledger: balances: %w", err)
	}
	defer rows.Close()
	var out []Balance
	for rows.Next() {
		var b Balance
		if err := rows.Scan(&b.Code, &b.Currency, &b.AmountMinor); err != nil {
			return nil, fmt.Errorf("ledger: scan balance: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// querier is the slice of pgx that a balance read needs, so the same query runs
// on the pool and inside a caller's transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Balance returns one account's balance in minor units with credits positive.
//
// Entries are stored debit-positive and credit-negative, which reads naturally
// inside a transaction but reports a liability such as a creator's payable
// account as a negative number. Balance negates the sum, so a positive result
// always means "this is what the account holds or is owed": a creator's payable
// balance is the money available to pay out, and a zero or negative result is a
// reason to refuse a payout rather than a number to interpret.
//
// includeSimulated must be passed explicitly, because the same account holds
// real and test money (D-070). A payout, a tax export and any figure that could
// move money pass false; only a creator-facing dashboard that labels the test
// data passes true. A code that has never been posted to returns 0, not an
// error, so a new creator has a balance of nothing rather than a 500.
func (s *Store) Balance(ctx context.Context, code, currency string, includeSimulated bool) (int64, error) {
	return balanceOf(ctx, s.pool, code, currency, includeSimulated)
}

// BalanceTx is Balance inside a caller's transaction: a payout reads the balance
// it is about to spend in the same transaction that marks the request approved,
// so two concurrent approvals cannot both see the same money.
func (s *Store) BalanceTx(ctx context.Context, tx pgx.Tx, code, currency string, includeSimulated bool) (int64, error) {
	return balanceOf(ctx, tx, code, currency, includeSimulated)
}

func balanceOf(ctx context.Context, q querier, code, currency string, includeSimulated bool) (int64, error) {
	var sum int64
	err := q.QueryRow(ctx, `
		SELECT COALESCE(sum(e.amount_minor), 0)::bigint
		FROM ledger.accounts a
		JOIN ledger.entries e ON e.account_id = a.id
		JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE a.code = $1 AND a.currency = $2 AND ($3 OR NOT t.simulated)`,
		code, currency, includeSimulated).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("ledger: balance of %q: %w", code, err)
	}
	return -sum, nil
}

// TrialBalance reports whether the whole ledger nets to zero and the total
// volume posted. A non-zero net is a defect, not a business state.
func (s *Store) TrialBalance(ctx context.Context, currency string) (net int64, volume int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(e.amount_minor), 0)::bigint,
		       COALESCE(sum(CASE WHEN e.amount_minor > 0 THEN e.amount_minor ELSE 0 END), 0)::bigint
		FROM ledger.entries e JOIN ledger.transactions t ON t.id = e.transaction_id
		WHERE t.currency = $1`, currency).Scan(&net, &volume)
	if err != nil {
		return 0, 0, fmt.Errorf("ledger: trial balance: %w", err)
	}
	return net, volume, nil
}

// IsUniqueViolation reports whether an error is a PostgreSQL unique violation.
// Exported so callers can treat "already exists" as success where that is right.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
