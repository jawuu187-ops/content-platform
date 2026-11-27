package payouts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/money"
)

// Service reads creator balances and moves payout requests through their states.
type Service struct {
	pool    *pgxpool.Pool
	ledger  *ledger.Store
	logger  *slog.Logger
	now     func() time.Time
	minimum int64
}

// Option configures the service.
type Option func(*Service)

// WithLogger sets the logger used for the operational trail.
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.logger = l
		}
	}
}

// WithClock overrides the clock for deterministic tests.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithMinimumPayout raises (never lowers) the smallest request the service
// accepts. Configuration can be stricter than the schema, not looser.
func WithMinimumPayout(minor int64) Option {
	return func(s *Service) {
		if minor > s.minimum {
			s.minimum = minor
		}
	}
}

// New builds the service on a pool and the ledger store it posts through.
func New(pool *pgxpool.Pool, led *ledger.Store, opts ...Option) *Service {
	s := &Service{
		pool:    pool,
		ledger:  led,
		logger:  slog.Default(),
		now:     time.Now,
		minimum: MinAmountMinor,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// payableCode is the creator's ledger account. It is the same code the settlement
// path credits and the earnings report reads, so a balance here and a net figure
// there cannot disagree.
func payableCode(creatorID string) string { return "creator:" + creatorID + ":payable" }

// Balance reports what a creator may request.
//
// The ledger figure is the truth and already excludes approved payouts, because
// approval debits the payable account. A request that is still open is not yet in
// the ledger, so it is reported separately instead of being folded in and hidden.
func (s *Service) Balance(ctx context.Context, creatorID, currency string) (Balance, error) {
	if !validCurrency(currency) {
		return Balance{}, fmt.Errorf("%w: currency must be one of USD, EUR, GBP", ErrInvalidInput)
	}
	out := Balance{CreatorID: creatorID, Currency: currency, MinimumMinor: s.minimum}

	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_minor), 0)::bigint
		FROM payouts.requests
		WHERE creator_id = $1::uuid AND currency = $2 AND status = 'requested'`,
		creatorID, currency).Scan(&out.PendingMinor); err != nil {
		// An empty table is a zero balance, not an error: a creator who has never
		// requested anything must still see a number.
		if !errors.Is(err, pgx.ErrNoRows) {
			return Balance{}, fmt.Errorf("payouts: pending total: %w", err)
		}
	}

	available, err := s.ledger.Balance(ctx, payableCode(creatorID), currency, false)
	if err != nil {
		return Balance{}, err
	}
	out.AvailableMinor = available
	return out, nil
}

// Request records a creator's withdrawal request. The amount is bounded here and
// again by the schema, and it can never exceed the live payable balance.
func (s *Service) Request(ctx context.Context, p identity.Principal, in Input) (Request, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Method = strings.TrimSpace(in.Method)
	in.Note = strings.TrimSpace(in.Note)

	if in.Method == "" {
		in.Method = MethodManual
	}
	if !validCurrency(in.Currency) {
		return Request{}, fmt.Errorf("%w: currency must be one of USD, EUR, GBP", ErrInvalidInput)
	}
	if !validMethod(in.Method) {
		return Request{}, fmt.Errorf("%w: method must be one of %s", ErrInvalidInput,
			strings.Join(Methods(), ", "))
	}
	if in.AmountMinor < s.minimum {
		return Request{}, fmt.Errorf("%w: the smallest payout is %d minor units", ErrInvalidInput, s.minimum)
	}
	if in.AmountMinor > MaxAmountMinor {
		return Request{}, fmt.Errorf("%w: the largest payout is %d minor units", ErrInvalidInput, MaxAmountMinor)
	}
	if len([]rune(in.Note)) > MaxNoteLength {
		return Request{}, fmt.Errorf("%w: the note is limited to %d characters", ErrInvalidInput, MaxNoteLength)
	}

	// Only a creator with a profile can be paid: the payable account is keyed by
	// that user id, and a fan must not create one by asking.
	var handle string
	err := s.pool.QueryRow(ctx, `SELECT handle::text FROM creator.profiles WHERE user_id = $1::uuid`,
		p.UserID).Scan(&handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, fmt.Errorf("%w: a creator profile is required before requesting a payout", ErrForbidden)
	}
	if err != nil {
		return Request{}, fmt.Errorf("payouts: load creator profile: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("payouts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	available, err := s.ledger.BalanceTx(ctx, tx, payableCode(p.UserID), in.Currency, false)
	if err != nil {
		return Request{}, err
	}
	if available < in.AmountMinor {
		return Request{}, fmt.Errorf("%w: asked for %d, %d is available", ErrInsufficientBalance,
			in.AmountMinor, available)
	}

	now := s.now().UTC()
	var req Request
	err = tx.QueryRow(ctx, `
		INSERT INTO payouts.requests (creator_id, amount_minor, currency, method, status, requested_by, note, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, 'requested', $1::uuid, $5, $6, $6)
		RETURNING `+requestColumns, p.UserID, in.AmountMinor, in.Currency, in.Method, in.Note, now,
	).Scan(requestScan(&req)...)
	if err != nil {
		if ledger.IsUniqueViolation(err) {
			// requests_one_open_per_creator: the database, not a read-then-write
			// check, is what refuses a second open request.
			return Request{}, fmt.Errorf("%w: a payout request in %s is already open",
				ErrConflict, in.Currency)
		}
		return Request{}, fmt.Errorf("payouts: insert request: %w", err)
	}
	if err := insertEvent(ctx, tx, req.ID, p.UserID, ActionRequest, "", StatusRequested, in.Note, now); err != nil {
		return Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("payouts: commit: %w", err)
	}
	s.logger.InfoContext(ctx, "payout requested",
		slog.String("request_id", req.ID),
		slog.String("creator_id", req.CreatorID),
		slog.Int64("amount_minor", req.AmountMinor),
		slog.String("currency", req.Currency))
	return req, nil
}

// Cancel lets a creator withdraw a request they have not been decided on. An
// approved request is money already reserved by finance, so only finance can
// reverse that, with a reason, through Reject.
func (s *Service) Cancel(ctx context.Context, p identity.Principal, id string) (Request, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("payouts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	req, err := lockRequest(ctx, tx, id)
	if err != nil {
		return Request{}, err
	}
	if req.CreatorID != p.UserID {
		return Request{}, ErrForbidden
	}
	if req.Status != StatusRequested {
		return Request{}, fmt.Errorf("%w: only a request that has not been decided can be cancelled", ErrConflict)
	}
	now := s.now().UTC()
	req, err = updateStatus(ctx, tx, req.ID, StatusCancelled, "", "", "", now)
	if err != nil {
		return Request{}, err
	}
	if err := insertEvent(ctx, tx, req.ID, p.UserID, ActionCancel, StatusRequested, StatusCancelled, "", now); err != nil {
		return Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("payouts: commit: %w", err)
	}
	return req, nil
}

// Approve reserves the money: one transaction reads the live balance, posts the
// payout movement and marks the request approved, so an approval cannot reserve
// money that a refund removed a moment earlier.
func (s *Service) Approve(ctx context.Context, p identity.Principal, id, note, requestID string) (Request, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > MaxDecisionNoteLength {
		return Request{}, fmt.Errorf("%w: the note is limited to %d characters", ErrInvalidInput, MaxDecisionNoteLength)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("payouts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	req, err := lockRequest(ctx, tx, id)
	if err != nil {
		return Request{}, err
	}
	if req.CreatorID == p.UserID {
		// RBAC already refuses payouts.approve to a creator; this keeps the rule
		// true even if a future grant is wider than it is today.
		return Request{}, fmt.Errorf("%w: a creator cannot approve their own payout", ErrForbidden)
	}
	if req.Status != StatusRequested {
		return Request{}, fmt.Errorf("%w: only a requested payout can be approved", ErrConflict)
	}

	available, err := s.ledger.BalanceTx(ctx, tx, payableCode(req.CreatorID), req.Currency, false)
	if err != nil {
		return Request{}, err
	}
	if available < req.AmountMinor {
		return Request{}, fmt.Errorf("%w: %d available against a request for %d",
			ErrInsufficientBalance, available, req.AmountMinor)
	}

	posting := ledger.Posting{
		Kind:      ledger.TxnPayout,
		Reference: "payout:" + req.ID,
		Currency:  req.Currency,
		Memo:      fmt.Sprintf("payout %s reserved for %s", req.ID, req.CreatorID),
		Entries: []ledger.Entry{
			// The liability to the creator is discharged and the same amount
			// becomes a liability to pay out, so the ledger stays balanced and the
			// money is visibly in flight rather than still owed as earnings.
			{AccountCode: payableCode(req.CreatorID), AmountMinor: req.AmountMinor},
			{AccountCode: ledger.AccountPayoutClearing, AmountMinor: -req.AmountMinor},
		},
	}
	if _, err := s.ledger.PostTx(ctx, tx, posting); err != nil {
		if errors.Is(err, ledger.ErrDuplicate) {
			// The reservation exists but the row still says requested: the two
			// disagree, and guessing which is right would move money twice.
			return Request{}, fmt.Errorf("%w: this payout is already reserved in the ledger", ErrConflict)
		}
		return Request{}, err
	}

	now := s.now().UTC()
	req, err = updateStatus(ctx, tx, req.ID, StatusApproved, p.UserID, note, "", now)
	if err != nil {
		return Request{}, err
	}
	if err := insertEvent(ctx, tx, req.ID, p.UserID, ActionApprove, StatusRequested, StatusApproved, note, now); err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `
		SELECT ops.append_audit('user', $1, 'payout.approve', 'payout_request', $2, $3, '{}'::jsonb, NULLIF($4, ''))`,
		p.UserID, req.ID, note, requestID); err != nil {
		return Request{}, fmt.Errorf("payouts: append audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("payouts: commit: %w", err)
	}
	s.logger.InfoContext(ctx, "payout approved",
		slog.String("request_id", req.ID),
		slog.String("decided_by", p.UserID),
		slog.Int64("amount_minor", req.AmountMinor))
	return req, nil
}

// Reject refuses a request. A request that was never approved is closed outright;
// one whose money is already reserved is reversed with a compensating ledger
// transaction, because history is corrected by adding an entry, never by editing
// one.
func (s *Service) Reject(ctx context.Context, p identity.Principal, id, note, requestID string) (Request, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) < MinDecisionNoteLength {
		return Request{}, fmt.Errorf("%w: a decision note of at least %d characters is required",
			ErrInvalidInput, MinDecisionNoteLength)
	}
	if len([]rune(note)) > MaxDecisionNoteLength {
		return Request{}, fmt.Errorf("%w: the note is limited to %d characters", ErrInvalidInput, MaxDecisionNoteLength)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("payouts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	req, err := lockRequest(ctx, tx, id)
	if err != nil {
		return Request{}, err
	}
	if req.CreatorID == p.UserID {
		return Request{}, fmt.Errorf("%w: a creator cannot decide their own payout", ErrForbidden)
	}
	if req.Status != StatusRequested && req.Status != StatusApproved {
		return Request{}, fmt.Errorf("%w: only an open payout can be rejected", ErrConflict)
	}

	if req.Status == StatusApproved {
		posting := ledger.Posting{
			Kind:      ledger.TxnPayout,
			Reference: "payout-reversal:" + req.ID,
			Currency:  req.Currency,
			Memo:      fmt.Sprintf("payout %s rejected after approval", req.ID),
			Entries: []ledger.Entry{
				{AccountCode: ledger.AccountPayoutClearing, AmountMinor: req.AmountMinor},
				{AccountCode: payableCode(req.CreatorID), AmountMinor: -req.AmountMinor},
			},
		}
		if _, err := s.ledger.PostTx(ctx, tx, posting); err != nil {
			if errors.Is(err, ledger.ErrDuplicate) {
				return Request{}, fmt.Errorf("%w: this payout was already reversed", ErrConflict)
			}
			return Request{}, err
		}
	}

	now := s.now().UTC()
	from := req.Status
	req, err = updateStatus(ctx, tx, req.ID, StatusRejected, p.UserID, note, "", now)
	if err != nil {
		return Request{}, err
	}
	if err := insertEvent(ctx, tx, req.ID, p.UserID, ActionReject, from, StatusRejected, note, now); err != nil {
		return Request{}, err
	}
	if _, err := tx.Exec(ctx, `
		SELECT ops.append_audit('user', $1, 'payout.reject', 'payout_request', $2, $3, '{}'::jsonb, NULLIF($4, ''))`,
		p.UserID, req.ID, note, requestID); err != nil {
		return Request{}, fmt.Errorf("payouts: append audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("payouts: commit: %w", err)
	}
	s.logger.InfoContext(ctx, "payout rejected",
		slog.String("request_id", req.ID),
		slog.String("decided_by", p.UserID),
		slog.Int64("amount_minor", req.AmountMinor))
	return req, nil
}

// List returns a creator's own requests, newest first.
func (s *Service) List(ctx context.Context, creatorID string) ([]Request, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+requestColumns+`
		FROM payouts.requests WHERE creator_id = $1::uuid
		ORDER BY created_at DESC LIMIT $2`, creatorID, MaxListSize)
	if err != nil {
		return nil, fmt.Errorf("payouts: list: %w", err)
	}
	defer rows.Close()
	return scanRequests(rows)
}

// Queue returns requests for a finance operator. An empty status means the open
// ones; "all" means everything.
func (s *Service) Queue(ctx context.Context, status string, limit int) ([]Request, error) {
	status = strings.TrimSpace(status)
	if limit <= 0 || limit > MaxListSize {
		limit = MaxListSize
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+requestColumns+`
		FROM payouts.requests
		WHERE ($1 = '' AND status IN ('requested', 'approved'))
		   OR ($1 = 'all')
		   OR ($1 <> '' AND $1 <> 'all' AND status = $1)
		ORDER BY CASE status WHEN 'requested' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END,
		         created_at ASC
		LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("payouts: queue: %w", err)
	}
	defer rows.Close()
	return scanRequests(rows)
}

// Detail returns one request with its trail. Its owner may read it; any caller
// holding payouts.approve may read any.
func (s *Service) Detail(ctx context.Context, p identity.Principal, id string, anyRequest bool) (Request, error) {
	var req Request
	err := s.pool.QueryRow(ctx, `SELECT `+requestColumns+` FROM payouts.requests WHERE id = $1::uuid`, id).
		Scan(requestScan(&req)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("payouts: detail: %w", err)
	}
	if !anyRequest && req.CreatorID != p.UserID {
		return Request{}, ErrForbidden
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, action, COALESCE(from_status, ''), to_status, note, COALESCE(actor_id::text, ''), created_at
		FROM payouts.request_events WHERE request_id = $1::uuid ORDER BY id`, id)
	if err != nil {
		return Request{}, fmt.Errorf("payouts: list events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Action, &e.FromStatus, &e.ToStatus, &e.Note, &e.ActorID, &e.CreatedAt); err != nil {
			return Request{}, fmt.Errorf("payouts: scan event: %w", err)
		}
		req.Events = append(req.Events, e)
	}
	if err := rows.Err(); err != nil {
		return Request{}, fmt.Errorf("payouts: iterate events: %w", err)
	}
	return req, nil
}

// requestColumns is the projection every read shares, so a new column cannot be
// added to one query and forgotten in another.
const requestColumns = `
	id::text, creator_id::text, amount_minor, currency, method, status,
	requested_by::text, COALESCE(decided_by::text, ''), note, decision_note, failure_reason,
	decided_at, paid_at, created_at, updated_at`

// requestScan returns the scan destinations for requestColumns in order.
func requestScan(r *Request) []any {
	return []any{
		&r.ID, &r.CreatorID, &r.AmountMinor, &r.Currency, &r.Method, &r.Status,
		&r.RequestedBy, &r.DecidedBy, &r.Note, &r.DecisionNote, &r.FailureReason,
		&r.DecidedAt, &r.PaidAt, &r.CreatedAt, &r.UpdatedAt,
	}
}

func scanRequests(rows pgx.Rows) ([]Request, error) {
	out := make([]Request, 0, 8)
	for rows.Next() {
		var r Request
		if err := rows.Scan(requestScan(&r)...); err != nil {
			return nil, fmt.Errorf("payouts: scan request: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// lockRequest reads one request FOR UPDATE so two decisions on the same row
// serialise instead of both acting on the status they read.
func lockRequest(ctx context.Context, tx pgx.Tx, id string) (Request, error) {
	var req Request
	err := tx.QueryRow(ctx, `SELECT `+requestColumns+` FROM payouts.requests WHERE id = $1::uuid FOR UPDATE`, id).
		Scan(requestScan(&req)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("payouts: lock request: %w", err)
	}
	return req, nil
}

// updateStatus writes the decision fields the schema requires and returns the
// row as the database now holds it.
func updateStatus(ctx context.Context, tx pgx.Tx, id, status, decidedBy, note, failureReason string, now time.Time) (Request, error) {
	var req Request
	err := tx.QueryRow(ctx, `
		UPDATE payouts.requests
		SET status = $2,
		    decided_by = CASE WHEN $3 = '' THEN decided_by ELSE $3::uuid END,
		    decided_at = CASE WHEN $3 = '' THEN decided_at ELSE $4 END,
		    decision_note = $5,
		    failure_reason = $6,
		    updated_at = $4
		WHERE id = $1::uuid
		RETURNING `+requestColumns, id, status, decidedBy, now, note, failureReason,
	).Scan(requestScan(&req)...)
	if err != nil {
		return Request{}, fmt.Errorf("payouts: update status: %w", err)
	}
	return req, nil
}

func insertEvent(ctx context.Context, tx pgx.Tx, requestID, actorID, action, from, to, note string, now time.Time) error {
	var fromArg any
	if from != "" {
		fromArg = from
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payouts.request_events (request_id, actor_id, action, from_status, to_status, note, created_at)
		VALUES ($1::uuid, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7)`,
		requestID, actorID, action, fromArg, to, note, now); err != nil {
		return fmt.Errorf("payouts: insert event: %w", err)
	}
	return nil
}

func validCurrency(c string) bool {
	_, ok := money.AllowedCurrencies[money.Currency(c)]
	return ok
}

func validMethod(m string) bool {
	for _, known := range Methods() {
		if m == known {
			return true
		}
	}
	return false
}
