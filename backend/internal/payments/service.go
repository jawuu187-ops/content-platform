package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
)

// Service creates intents, applies provider events and posts the ledger.
type Service struct {
	pool          *pgxpool.Pool
	provider      Provider
	ledger        *ledger.Store
	creators      *creator.Service
	commissionBPS int64
	now           func() time.Time
	logger        *slog.Logger
}

// Option configures the service.
type Option func(*Service)

// WithProvider installs the payment provider.
func WithProvider(p Provider) Option { return func(s *Service) { s.provider = p } }

// WithLedger installs the ledger store.
func WithLedger(l *ledger.Store) Option { return func(s *Service) { s.ledger = l } }

// WithCreators installs the creator service used to grant what was bought.
func WithCreators(c *creator.Service) Option { return func(s *Service) { s.creators = c } }

// WithCommission sets the platform share in basis points (2000 = 20%).
func WithCommission(bps int64) Option { return func(s *Service) { s.commissionBPS = bps } }

// WithClock overrides the clock for tests.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.logger = l } }

// New builds the service. The default provider is Unavailable, which fails closed.
func New(pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{
		pool:          pool,
		provider:      Unavailable{},
		ledger:        ledger.New(pool),
		commissionBPS: 2000,
		now:           time.Now,
		logger:        slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Configured reports whether a real (or fake) provider is installed.
func (s *Service) Configured() bool {
	return s.provider != nil && s.provider.Name() != "unconfigured"
}

// Capabilities reports what the installed provider can do.
func (s *Service) Capabilities() Capabilities {
	if s.provider == nil {
		return Capabilities{}
	}
	return s.provider.Capabilities()
}

// ProviderName is the installed provider, for /v1/meta and the admin surface.
func (s *Service) ProviderName() string {
	if s.provider == nil {
		return "unconfigured"
	}
	return s.provider.Name()
}

// CreateIntent opens a purchase. The price is read from the creator's records;
// the request can only choose what to buy.
func (s *Service) CreateIntent(ctx context.Context, p identity.Principal, in CreateInput, requestID string) (Intent, error) {
	if !s.Configured() {
		return Intent{}, ErrUnavailable
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if len(key) < 8 || len(key) > 128 {
		return Intent{}, fmt.Errorf("%w: idempotency_key must be 8-128 characters", ErrInvalidInput)
	}

	intent := Intent{UserID: p.UserID, IdempotencyKey: key, Provider: s.provider.Name()}
	switch in.Purpose {
	case PurposeSubscription:
		if in.TierID == "" {
			return Intent{}, fmt.Errorf("%w: tier_id is required", ErrInvalidInput)
		}
		tier, err := s.creators.TierForPurchase(ctx, in.TierID)
		if err != nil {
			return Intent{}, err
		}
		if tier.CreatorID == p.UserID {
			return Intent{}, fmt.Errorf("%w: you cannot subscribe to yourself", ErrForbidden)
		}
		intent.Purpose = PurposeSubscription
		intent.CreatorID, intent.TierID = tier.CreatorID, tier.ID
		intent.AmountMinor, intent.Currency = tier.PriceMinor, tier.Currency
	case PurposePPV:
		if in.PostID == "" {
			return Intent{}, fmt.Errorf("%w: post_id is required", ErrInvalidInput)
		}
		post, err := s.creators.PostForPurchase(ctx, in.PostID)
		if err != nil {
			return Intent{}, err
		}
		if post.CreatorID == p.UserID {
			return Intent{}, fmt.Errorf("%w: this post is yours", ErrForbidden)
		}
		intent.Purpose = PurposePPV
		intent.CreatorID, intent.PostID = post.CreatorID, post.ID
		intent.AmountMinor, intent.Currency = *post.PriceMinor, post.Currency
	default:
		return Intent{}, fmt.Errorf("%w: purpose must be subscription or ppv", ErrInvalidInput)
	}

	row, created, err := s.insertIntent(ctx, intent)
	if err != nil {
		return Intent{}, err
	}
	if !created {
		// Same idempotency key: the first attempt's intent is the answer.
		return row, nil
	}

	opened, err := s.provider.CreateIntent(ctx, row)
	if err != nil {
		_, _ = s.pool.Exec(ctx, `
			UPDATE payments.intents SET status = 'failed', failure_reason = $2, updated_at = now()
			WHERE id = $1::uuid`, row.ID, "provider could not open the payment")
		s.logger.ErrorContext(ctx, "payment provider failed to open an intent",
			slog.String("intent_id", row.ID), slog.Any("error", err))
		return Intent{}, fmt.Errorf("%w: provider did not open the payment", ErrUnavailable)
	}
	if opened.ProviderRef != "" || opened.Status != "" {
		if _, err := s.pool.Exec(ctx, `
			UPDATE payments.intents
			SET provider_ref = NULLIF($2, ''), checkout_url = NULLIF($3, ''),
			    status = CASE WHEN status = 'created' THEN $4 ELSE status END, updated_at = now()
			WHERE id = $1::uuid`,
			row.ID, opened.ProviderRef, opened.CheckoutURL, nonEmpty(opened.Status, StatusPending)); err != nil {
			return Intent{}, fmt.Errorf("payments: record provider intent: %w", err)
		}
	}
	row.ProviderRef = opened.ProviderRef
	row.CheckoutURL = opened.CheckoutURL
	if opened.Status != "" {
		row.Status = opened.Status
	} else {
		row.Status = StatusPending
	}
	s.audit(ctx, p.UserID, "payment.intent_created", "payment_intent", row.ID, requestID,
		map[string]any{"purpose": row.Purpose, "amount_minor": row.AmountMinor, "currency": row.Currency})
	return row, nil
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Service) insertIntent(ctx context.Context, in Intent) (Intent, bool, error) {
	var out Intent
	var settled *time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO payments.intents
		    (user_id, purpose, creator_id, tier_id, post_id, amount_minor, currency, provider, idempotency_key)
		VALUES ($1::uuid, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, $6, $7, $8, $9)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING id::text, user_id::text, purpose, COALESCE(creator_id::text, ''), COALESCE(tier_id::text, ''),
		          COALESCE(post_id::text, ''), amount_minor, currency, provider, COALESCE(provider_ref, ''),
		          status, checkout_url, failure_reason, created_at, settled_at`,
		in.UserID, in.Purpose, in.CreatorID, in.TierID, in.PostID, in.AmountMinor, in.Currency,
		in.Provider, in.IdempotencyKey).
		Scan(&out.ID, &out.UserID, &out.Purpose, &out.CreatorID, &out.TierID, &out.PostID, &out.AmountMinor,
			&out.Currency, &out.Provider, &out.ProviderRef, &out.Status, &out.CheckoutURL, &out.FailureReason,
			&out.CreatedAt, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := s.intentByIdempotency(ctx, in.UserID, in.IdempotencyKey)
		return existing, false, err
	}
	if err != nil {
		return Intent{}, false, fmt.Errorf("payments: insert intent: %w", err)
	}
	out.SettledAt = settled
	return out, true, nil
}

func (s *Service) scanIntent(row pgx.Row) (Intent, error) {
	var out Intent
	var settled *time.Time
	err := row.Scan(&out.ID, &out.UserID, &out.Purpose, &out.CreatorID, &out.TierID, &out.PostID,
		&out.AmountMinor, &out.Currency, &out.Provider, &out.ProviderRef, &out.Status, &out.CheckoutURL,
		&out.FailureReason, &out.CreatedAt, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, ErrNotFound
	}
	if err != nil {
		return Intent{}, fmt.Errorf("payments: scan intent: %w", err)
	}
	out.SettledAt = settled
	return out, nil
}

const intentColumns = `id::text, user_id::text, purpose, COALESCE(creator_id::text, ''), COALESCE(tier_id::text, ''),
	COALESCE(post_id::text, ''), amount_minor, currency, provider, COALESCE(provider_ref, ''),
	status, checkout_url, failure_reason, created_at, settled_at`

func (s *Service) intentByIdempotency(ctx context.Context, userID, key string) (Intent, error) {
	return s.scanIntent(s.pool.QueryRow(ctx,
		`SELECT `+intentColumns+` FROM payments.intents WHERE user_id = $1::uuid AND idempotency_key = $2`,
		userID, key))
}

// Get returns an intent to its owner or to an admin.
func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (Intent, error) {
	in, err := s.scanIntent(s.pool.QueryRow(ctx,
		`SELECT `+intentColumns+` FROM payments.intents WHERE id = $1::uuid`, id))
	if err != nil {
		return Intent{}, err
	}
	if in.UserID != p.UserID && !identity.HasPermission(p.Roles, identity.PermAdminAll) {
		// Another account's payment is reported as missing, not forbidden.
		return Intent{}, ErrNotFound
	}
	return in, nil
}

// ListMine lists the caller's own payment history.
func (s *Service) ListMine(ctx context.Context, p identity.Principal) ([]Intent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+intentColumns+` FROM payments.intents WHERE user_id = $1::uuid ORDER BY created_at DESC LIMIT 50`,
		p.UserID)
	if err != nil {
		return nil, fmt.Errorf("payments: list intents: %w", err)
	}
	defer rows.Close()
	var out []Intent
	for rows.Next() {
		in, err := s.scanIntent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// HandleEvent applies one provider callback exactly once. The event row is
// inserted first: a replayed event fails the primary key and is reported as a
// duplicate without touching the ledger or the entitlements.
func (s *Service) HandleEvent(ctx context.Context, header http.Header, body []byte) (Result, error) {
	if !s.Configured() {
		return Result{}, ErrUnavailable
	}
	event, err := s.provider.ParseEvent(ctx, header, body)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(body)
	var intentID *string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO payments.events (provider, event_id, kind, intent_id, payload_hash)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5)
		ON CONFLICT (provider, event_id) DO NOTHING
		RETURNING intent_id::text`,
		s.provider.Name(), event.ID, event.Kind, event.IntentID, hex.EncodeToString(sum[:])).Scan(&intentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Applied: false, Duplicate: true, IntentID: event.IntentID}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("payments: record event: %w", err)
	}
	intent, err := s.scanIntent(s.pool.QueryRow(ctx,
		`SELECT `+intentColumns+` FROM payments.intents WHERE id = $1::uuid`, event.IntentID))
	if err != nil {
		return Result{}, err
	}
	if event.AmountMinor != intent.AmountMinor || !strings.EqualFold(event.Currency, intent.Currency) {
		return Result{}, fmt.Errorf("%w: reported %d %s, expected %d %s", ErrAmountMismatch,
			event.AmountMinor, event.Currency, intent.AmountMinor, intent.Currency)
	}

	switch event.Kind {
	case EventSucceeded:
		if err := s.settle(ctx, intent); err != nil {
			return Result{}, err
		}
	case EventFailed:
		if _, err := s.pool.Exec(ctx, `
			UPDATE payments.intents SET status = 'failed', failure_reason = $2, updated_at = now()
			WHERE id = $1::uuid AND status NOT IN ('succeeded', 'refunded')`,
			intent.ID, truncate(event.Reason, 200)); err != nil {
			return Result{}, fmt.Errorf("payments: mark failed: %w", err)
		}
	case EventRefunded:
		if err := s.applyRefund(ctx, intent); err != nil {
			return Result{}, err
		}
	default:
		return Result{}, fmt.Errorf("%w: unsupported event kind", ErrInvalidInput)
	}

	if _, err := s.pool.Exec(ctx, `UPDATE payments.events SET applied_at = now()
		WHERE provider = $1 AND event_id = $2`, s.provider.Name(), event.ID); err != nil {
		return Result{}, fmt.Errorf("payments: mark event applied: %w", err)
	}
	status, _ := s.intentStatus(ctx, intent.ID)
	return Result{Applied: true, IntentID: intent.ID, Status: status}, nil
}

func (s *Service) intentStatus(ctx context.Context, id string) (string, error) {
	var st string
	err := s.pool.QueryRow(ctx, `SELECT status FROM payments.intents WHERE id = $1::uuid`, id).Scan(&st)
	if err != nil {
		return "", fmt.Errorf("payments: read status: %w", err)
	}
	return st, nil
}

// settle marks the intent paid, posts the ledger and grants what was bought.
// Every step is idempotent so a retried delivery of the same settlement is safe.
func (s *Service) settle(ctx context.Context, intent Intent) error {
	current, err := s.intentStatus(ctx, intent.ID)
	if err != nil {
		return err
	}
	if current == StatusRefunded {
		// A settlement that arrives after a refund must not re-grant anything.
		return fmt.Errorf("%w: intent is already refunded", ErrInvalidState)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE payments.intents SET status = 'succeeded', settled_at = COALESCE(settled_at, now()),
		       updated_at = now()
		WHERE id = $1::uuid AND status NOT IN ('refunded')`, intent.ID); err != nil {
		return fmt.Errorf("payments: mark succeeded: %w", err)
	}

	creatorShare := intent.AmountMinor * (10_000 - s.commissionBPS) / 10_000
	platformShare := intent.AmountMinor - creatorShare
	kind := ledger.TxnPPV
	if intent.Purpose == PurposeSubscription {
		kind = ledger.TxnSubscription
	}
	posting := ledger.Posting{
		Kind:      kind,
		Reference: intent.ID,
		Currency:  intent.Currency,
		Memo:      fmt.Sprintf("%s %s", intent.Purpose, intent.ID),
		Entries: []ledger.Entry{
			{AccountCode: ledger.AccountProviderClearing, AmountMinor: intent.AmountMinor},
			{AccountCode: creatorPayableCode(intent.CreatorID), AmountMinor: -creatorShare},
			{AccountCode: ledger.AccountRevenue, AmountMinor: -platformShare},
		},
	}
	if _, err := s.ledger.Post(ctx, posting); err != nil && !errors.Is(err, ledger.ErrDuplicate) {
		return err
	}

	switch intent.Purpose {
	case PurposeSubscription:
		if _, err := s.creators.ActivatePaidSubscription(ctx, intent.UserID, intent.CreatorID, intent.TierID, intent.ID); err != nil {
			return err
		}
	case PurposePPV:
		if _, err := s.creators.UnlockPostPaid(ctx, intent.UserID, intent.PostID, intent.ID); err != nil {
			return err
		}
	}
	return nil
}

// applyRefund reverses a settlement: compensating ledger transaction, then the
// withdrawal of the entitlement it paid for.
func (s *Service) applyRefund(ctx context.Context, intent Intent) error {
	creatorShare := intent.AmountMinor * (10_000 - s.commissionBPS) / 10_000
	platformShare := intent.AmountMinor - creatorShare
	posting := ledger.Posting{
		Kind:      ledger.TxnRefund,
		Reference: "refund:" + intent.ID,
		Currency:  intent.Currency,
		Memo:      "refund " + intent.ID,
		Entries: []ledger.Entry{
			{AccountCode: creatorPayableCode(intent.CreatorID), AmountMinor: creatorShare},
			{AccountCode: ledger.AccountRevenue, AmountMinor: platformShare},
			{AccountCode: ledger.AccountProviderClearing, AmountMinor: -intent.AmountMinor},
		},
	}
	if _, err := s.ledger.Post(ctx, posting); err != nil && !errors.Is(err, ledger.ErrDuplicate) {
		return err
	}
	if err := s.creators.RevokePaidAccess(ctx, intent.ID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE payments.intents SET status = 'refunded', updated_at = now()
		WHERE id = $1::uuid AND status <> 'refunded'`, intent.ID); err != nil {
		return fmt.Errorf("payments: mark refunded: %w", err)
	}
	return nil
}

// Refund issues a refund through the provider and applies it locally. Partial
// refunds are refused rather than approximated: an entitlement is all or nothing.
func (s *Service) Refund(ctx context.Context, p identity.Principal, intentID string, amountMinor int64, reason, requestID string) (Intent, error) {
	if !identity.HasPermission(p.Roles, identity.PermAdminAll) {
		return Intent{}, ErrForbidden
	}
	if !s.Configured() {
		return Intent{}, ErrUnavailable
	}
	if !s.provider.Capabilities().Refunds {
		return Intent{}, ErrRefundUnsupported
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 200 {
		return Intent{}, fmt.Errorf("%w: a reason of 3-200 characters is required", ErrInvalidInput)
	}
	intent, err := s.scanIntent(s.pool.QueryRow(ctx,
		`SELECT `+intentColumns+` FROM payments.intents WHERE id = $1::uuid`, intentID))
	if err != nil {
		return Intent{}, err
	}
	if intent.Status == StatusRefunded {
		return intent, nil
	}
	if intent.Status != StatusSucceeded {
		return Intent{}, fmt.Errorf("%w: only a succeeded payment can be refunded", ErrInvalidState)
	}
	if amountMinor != intent.AmountMinor {
		return Intent{}, fmt.Errorf("%w: only full refunds are supported", ErrInvalidInput)
	}
	ref, err := s.provider.Refund(ctx, intent, amountMinor, reason)
	if err != nil {
		return Intent{}, err
	}
	var refundID string
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO payments.refunds (intent_id, amount_minor, reason, actor_id, provider_ref)
		VALUES ($1::uuid, $2, $3, $4::uuid, $5)
		RETURNING id::text`, intent.ID, amountMinor, reason, p.UserID, ref).Scan(&refundID); err != nil {
		return Intent{}, fmt.Errorf("payments: record refund: %w", err)
	}
	if err := s.applyRefund(ctx, intent); err != nil {
		return Intent{}, err
	}
	s.audit(ctx, p.UserID, "payment.refunded", "payment_intent", intent.ID, requestID,
		map[string]any{"amount_minor": amountMinor, "reason": reason, "refund_id": refundID})
	updated, err := s.scanIntent(s.pool.QueryRow(ctx,
		`SELECT `+intentColumns+` FROM payments.intents WHERE id = $1::uuid`, intent.ID))
	if err != nil {
		return Intent{}, err
	}
	return updated, nil
}

// TrialBalance exposes the ledger's own arithmetic to the admin surface.
func (s *Service) TrialBalance(ctx context.Context, currency string) (net int64, volume int64, err error) {
	return s.ledger.TrialBalance(ctx, currency)
}

// Balances lists account balances for an operator statement.
func (s *Service) Balances(ctx context.Context, currency string) ([]ledger.Balance, error) {
	return s.ledger.Balances(ctx, currency)
}

func creatorPayableCode(creatorID string) string {
	if creatorID == "" {
		return "platform:creator_payable_unassigned"
	}
	return "creator:" + creatorID + ":payable"
}

func (s *Service) audit(ctx context.Context, actorID, action, targetType, targetID, requestID string, meta map[string]any) {
	if _, err := s.pool.Exec(ctx, `
		SELECT ops.append_audit('user', $1, $2, $3, $4, NULL, $5::jsonb, NULLIF($6, ''))`,
		actorID, action, targetType, targetID, meta, requestID); err != nil {
		s.logger.WarnContext(ctx, "audit write failed", slog.String("action", action), slog.Any("error", err))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
