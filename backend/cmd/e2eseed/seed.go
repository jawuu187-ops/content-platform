// Fixture creation for the device tests.
//
// This file is deliberately separate from main so the fixture can be built and
// asserted by a Go test against a disposable PostgreSQL database, instead of only
// being exercised by the emulator. main.go owns flags, environment checks and the
// fixture file; this file owns the data.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/earnings"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/messaging"
	"github.com/jawuu187-ops/content-platform/backend/internal/payouts"
)

const (
	fanEmail       = "fan@e2e.test"
	creatorEmail   = "creator@e2e.test"
	moderatorEmail = "moderator@e2e.test"
	financeEmail   = "finance@e2e.test"
	creatorHandle  = "mira_e2e"
	creatorName    = "Mira Vale"

	seedCurrency = "USD"

	// The two earnings the fixture records. The live one is what a payout may spend;
	// the test one is what the fake provider produces and what no payout may touch.
	seedLiveTipMinor   = int64(5000)
	seedTestTipMinor   = int64(900)
	seedApprovedPayout = int64(1000)

	// defaultCommissionBPS mirrors the deployment default in platform config, so the
	// seeded ledger split matches what the running API would compute.
	defaultCommissionBPS = int64(2000)
)

// Options is what the CLI collected from the environment.
type Options struct {
	BaseURL  string
	Password string
}

// Fixture is the file the device tests and the contract checks read.
type Fixture struct {
	BaseURL        string `json:"base_url"`
	FanEmail       string `json:"fan_email"`
	ModeratorEmail string `json:"moderator_email"`
	FinanceEmail   string `json:"finance_email"`
	CreatorID      string `json:"creator_id"`
	CreatorName    string `json:"creator_name"`
	ConversationID string `json:"conversation_id"`
	FanID          string `json:"fan_id"`
	FinanceID      string `json:"finance_id"`

	// PayoutRequestID is an approved request the fixture left behind, so the device
	// and contract checks can read a real reservation without creating one first.
	PayoutRequestID string `json:"payout_request_id"`
	Currency        string `json:"currency"`

	// Money as the ledger holds it, in minor units. LiveEarningsMinor is the live
	// payable balance before the reservation; SimulatedEarningsMinor is test money
	// and is never part of AvailableMinor.
	LiveEarningsMinor      int64 `json:"live_earnings_minor"`
	SimulatedEarningsMinor int64 `json:"simulated_earnings_minor"`
	ApprovedPayoutMinor    int64 `json:"approved_payout_minor"`
	AvailableMinor         int64 `json:"available_minor"`
	PendingMinor           int64 `json:"pending_minor"`

	// Export figures for the current calendar year, simulated money excluded.
	ExportRows       int   `json:"export_rows"`
	ExportNetMinor   int64 `json:"export_net_minor"`
	ExportGrossMinor int64 `json:"export_gross_minor"`
}

type allowAll struct{}

func (allowAll) AllowAdult(context.Context, string) (bool, error) { return true, nil }

// Seed creates the whole fixture. It refuses to invent money: the two ledger
// settlements it posts are marked live or simulated exactly as the platform marks
// them, and the numbers it reports are read back from the ledger and from the
// payout service, not assumed.
//
// The live settlement is fixture money in a disposable demo database — there is no
// provider behind it and no real card anywhere (D-079). The caller has already
// established APP_ENV=demo|local and PAY_PROVIDER=fake, which is the same guard
// that lets this tool grant roles by hand.
func Seed(ctx context.Context, pool *pgxpool.Pool, opts Options) (Fixture, error) {
	identitySvc := identity.New(pool)
	creatorSvc := creator.New(pool, creator.WithAgeGate(allowAll{}), creator.WithFakePayments(true))
	dm := messaging.New(pool)
	led := ledger.New(pool)
	payoutSvc := payouts.New(pool, led)
	earner := earnings.New(pool)

	fan, fanPrincipal, err := account(ctx, identitySvc, opts.Password, fanEmail)
	if err != nil {
		return Fixture{}, fmt.Errorf("seed fan: %w", err)
	}
	creatorUser, creatorPrincipal, err := account(ctx, identitySvc, opts.Password, creatorEmail)
	if err != nil {
		return Fixture{}, fmt.Errorf("seed creator: %w", err)
	}

	// Profile and tier are created through the service so the same validation the
	// API applies is applied here.
	if _, err := creatorSvc.CreateProfile(ctx, creatorPrincipal, creator.ProfileInput{
		Handle:      creatorHandle,
		DisplayName: creatorName,
		Bio:         "Studio artist. New work every week.",
		Category:    "art",
		Publish:     ptr(true),
	}, "e2eseed-profile"); err != nil {
		return Fixture{}, fmt.Errorf("create profile: %w", err)
	}
	// The API grants the creator role when the profile route is called; a seed that
	// only used the service would leave the account a fan at the API, and every
	// creator-only request would answer 403. Self-service, so the audit names the
	// account and not a synthetic actor.
	if _, err := identitySvc.BecomeCreator(ctx, creatorPrincipal, "e2eseed-creator-role"); err != nil {
		return Fixture{}, fmt.Errorf("grant creator role: %w", err)
	}
	price := int64(900)
	period := 30
	if _, err := creatorSvc.CreateTier(ctx, creatorPrincipal, creator.TierInput{
		Name:        "Studio",
		Description: "Every post and the monthly set.",
		PriceMinor:  &price,
		Currency:    seedCurrency,
		PeriodDays:  &period,
		Perks:       []string{"All paid posts", "Direct messages"},
	}, "e2eseed-tier"); err != nil {
		return Fixture{}, fmt.Errorf("create tier: %w", err)
	}

	// A live membership granted by support: source=admin, no subscription row, so
	// no money and no provider record are involved.
	if _, err := pool.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'admin', now() + interval '2 hours')`,
		fan, creatorUser); err != nil {
		return Fixture{}, fmt.Errorf("grant membership: %w", err)
	}

	// A moderator account for the safety queue and a finance account for the payout
	// queue. Roles are granted here rather than through the API because this tool
	// only runs against a disposable demo database.
	modUser, _, err := account(ctx, identitySvc, opts.Password, moderatorEmail)
	if err != nil {
		return Fixture{}, fmt.Errorf("seed moderator: %w", err)
	}
	if _, err := identitySvc.GrantRole(ctx, creatorPrincipal, modUser, identity.RoleAdmin, "e2eseed-moderator"); err != nil {
		return Fixture{}, fmt.Errorf("grant moderator role: %w", err)
	}
	financeUser, financePrincipal, err := account(ctx, identitySvc, opts.Password, financeEmail)
	if err != nil {
		return Fixture{}, fmt.Errorf("seed finance: %w", err)
	}
	if _, err := identitySvc.GrantRole(ctx, creatorPrincipal, financeUser, identity.RoleFinance, "e2eseed-finance"); err != nil {
		return Fixture{}, fmt.Errorf("grant finance role: %w", err)
	}

	// Earnings: one settled live tip and one settled test tip, each with the intent
	// row that carries the gross figure, and each ledger transaction shaped exactly
	// like the payments service writes it (D-069, D-070).
	if _, err := seedSettlement(ctx, pool, led, fan, creatorUser, seedLiveTipMinor, false, "fake"); err != nil {
		return Fixture{}, err
	}
	if _, err := seedSettlement(ctx, pool, led, fan, creatorUser, seedTestTipMinor, true, "fake"); err != nil {
		return Fixture{}, err
	}

	// Read the live balance before the payout, so the reservation that follows can
	// be checked against a measured number rather than an assumed one.
	before, err := payoutSvc.Balance(ctx, creatorUser, seedCurrency)
	if err != nil {
		return Fixture{}, fmt.Errorf("read balance before payout: %w", err)
	}

	// A payout with a real history: the creator asks, finance approves, and the
	// approval reserves the money in the ledger.
	req, err := payoutSvc.Request(ctx, creatorPrincipal, payouts.Input{
		AmountMinor: seedApprovedPayout,
		Currency:    seedCurrency,
		Method:      payouts.MethodBank,
		Note:        "first withdrawal",
	})
	if err != nil {
		return Fixture{}, fmt.Errorf("request payout: %w", err)
	}
	if _, err := payoutSvc.Approve(ctx, financePrincipal, req.ID, "seeded for the device fixture", "e2eseed-payout"); err != nil {
		return Fixture{}, fmt.Errorf("approve payout: %w", err)
	}

	after, err := payoutSvc.Balance(ctx, creatorUser, seedCurrency)
	if err != nil {
		return Fixture{}, fmt.Errorf("read balance after payout: %w", err)
	}

	// Test money is reported by subtracting it from the only figure that can see it.
	withTest, err := led.Balance(ctx, payableCode(creatorUser), seedCurrency, true)
	if err != nil {
		return Fixture{}, fmt.Errorf("read balance including test money: %w", err)
	}
	// Both figures are read after the reservation, so the difference is test money
	// and nothing else.
	simulated := withTest - after.AvailableMinor

	now := time.Now().UTC()
	exportFrom := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	exp, err := earner.Export(ctx, earnings.ExportQuery{
		CreatorID: creatorUser,
		Currency:  seedCurrency,
		From:      exportFrom,
		To:        now.Add(time.Minute),
	})
	if err != nil {
		return Fixture{}, fmt.Errorf("export seeded earnings: %w", err)
	}

	conv, err := dm.Open(ctx, fanPrincipal, creatorUser)
	if err != nil {
		return Fixture{}, fmt.Errorf("open conversation: %w", err)
	}
	if _, err := dm.Send(ctx, creatorPrincipal, conv.ID,
		"Welcome in. The new set is up — tell me what you think."); err != nil {
		return Fixture{}, fmt.Errorf("seed message: %w", err)
	}

	fixture := Fixture{
		BaseURL:                opts.BaseURL,
		FanEmail:               fanEmail,
		ModeratorEmail:         moderatorEmail,
		FinanceEmail:           financeEmail,
		CreatorID:              creatorUser,
		CreatorName:            creatorName,
		ConversationID:         conv.ID,
		FanID:                  fan,
		FinanceID:              financeUser,
		PayoutRequestID:        req.ID,
		Currency:               seedCurrency,
		LiveEarningsMinor:      before.AvailableMinor,
		SimulatedEarningsMinor: simulated,
		ApprovedPayoutMinor:    seedApprovedPayout,
		AvailableMinor:         after.AvailableMinor,
		PendingMinor:           after.PendingMinor,
		ExportRows:             len(exp.Rows),
		ExportNetMinor:         exp.NetMinor,
		ExportGrossMinor:       exp.GrossMinor,
	}
	if err := fixture.check(); err != nil {
		return Fixture{}, err
	}
	return fixture, nil
}

// check states the invariants the device tests rely on. A fixture that quietly
// disagreed with the ledger would make every later assertion meaningless, so the
// seed fails instead.
func (f Fixture) check() error {
	if f.PendingMinor != 0 {
		return fmt.Errorf("fixture invariant: pending = %d, want 0", f.PendingMinor)
	}
	if f.AvailableMinor != f.LiveEarningsMinor-f.ApprovedPayoutMinor {
		return fmt.Errorf("fixture invariant: available %d != live earnings %d - payout %d",
			f.AvailableMinor, f.LiveEarningsMinor, f.ApprovedPayoutMinor)
	}
	if f.SimulatedEarningsMinor <= 0 {
		return fmt.Errorf("fixture invariant: no simulated earnings were seeded (%d)", f.SimulatedEarningsMinor)
	}
	if f.ExportNetMinor != f.AvailableMinor {
		return fmt.Errorf("fixture invariant: export net %d != available %d", f.ExportNetMinor, f.AvailableMinor)
	}
	if f.ExportGrossMinor != seedLiveTipMinor {
		// Gross is what the fan was charged; net is the creator's share after the
		// platform commission, so the two differ by exactly that commission.
		return fmt.Errorf("fixture invariant: export gross %d != seeded charge %d",
			f.ExportGrossMinor, seedLiveTipMinor)
	}
	if f.ExportRows != 2 {
		// One live settlement and one payout reservation. A different count means the
		// export is counting something the fixture did not create.
		return fmt.Errorf("fixture invariant: export rows = %d, want 2", f.ExportRows)
	}
	return nil
}

// seedSettlement posts one settled tip: the intent that carries the gross, and the
// balanced ledger transaction that carries the creator's net.
func seedSettlement(ctx context.Context, pool *pgxpool.Pool, led *ledger.Store,
	buyerID, creatorID string, amount int64, simulated bool, provider string) (string, error) {
	var intentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO payments.intents
		    (user_id, purpose, creator_id, amount_minor, currency, provider, status,
		     idempotency_key, simulated, settled_at)
		VALUES ($1::uuid, 'tip', $2::uuid, $3, $4, $5, 'succeeded', $6, $7, now())
		RETURNING id::text`,
		buyerID, creatorID, amount, seedCurrency, provider, "e2eseed-"+provider+"-"+randomSuffix(simulated),
		simulated).Scan(&intentID); err != nil {
		return "", fmt.Errorf("insert seeded intent: %w", err)
	}
	creatorShare := amount * (10_000 - commissionBPS()) / 10_000
	if _, err := led.Post(ctx, ledger.Posting{
		Kind:      ledger.TxnTip,
		Reference: intentID,
		Currency:  seedCurrency,
		Memo:      "seeded tip",
		Simulated: simulated,
		Entries: []ledger.Entry{
			{AccountCode: ledger.AccountProviderClearing, AmountMinor: amount},
			{AccountCode: payableCode(creatorID), AmountMinor: -creatorShare},
			{AccountCode: ledger.AccountRevenue, AmountMinor: -(amount - creatorShare)},
		},
	}); err != nil {
		return "", fmt.Errorf("post seeded settlement: %w", err)
	}
	return intentID, nil
}

// ExpireMembership ends the seeded membership the way support does, leaving the
// history in place. The expired-membership run depends on exactly this.
func ExpireMembership(ctx context.Context, pool *pgxpool.Pool, opts Options) error {
	identitySvc := identity.New(pool)
	fan, _, err := account(ctx, identitySvc, opts.Password, fanEmail)
	if err != nil {
		return fmt.Errorf("find fan: %w", err)
	}
	creatorUser, _, err := account(ctx, identitySvc, opts.Password, creatorEmail)
	if err != nil {
		return fmt.Errorf("find creator: %w", err)
	}
	tag, err := pool.Exec(ctx, `
		UPDATE creator.entitlements
		   SET expires_at = now() - interval '1 minute'
		 WHERE fan_id = $1::uuid AND creator_id = $2::uuid`, fan, creatorUser)
	if err != nil {
		return fmt.Errorf("expire membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("expire membership: no entitlement found; seed first")
	}
	return nil
}

// account registers the user, or signs in when it already exists, so a re-run
// against the same database is not an error.
func account(ctx context.Context, svc *identity.Service, password, email string) (string, identity.Principal, error) {
	res, err := svc.Register(ctx, email, password, identity.DeviceInfo{
		Fingerprint: "e2eseed", Platform: "ci", Name: "seed",
	}, "e2eseed-"+email)
	if err == nil {
		return res.User.ID, res.Principal, nil
	}
	if !errors.Is(err, identity.ErrEmailTaken) {
		return "", identity.Principal{}, err
	}
	res, err = svc.Login(ctx, email, password, "", identity.DeviceInfo{
		Fingerprint: "e2eseed", Platform: "ci", Name: "seed",
	}, "e2eseed-login-"+email)
	if err != nil {
		return "", identity.Principal{}, err
	}
	return res.User.ID, res.Principal, nil
}

func payableCode(creatorID string) string { return "creator:" + creatorID + ":payable" }

// commissionBPS is read the same way the API configuration reads it, so a workflow
// that changes the split changes the seeded rows too.
func commissionBPS() int64 {
	if raw := os.Getenv("PAY_COMMISSION_BPS"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v >= 0 && v <= 5000 {
			return v
		}
	}
	return defaultCommissionBPS
}

func randomSuffix(simulated bool) string {
	// Deterministic, readable keys: the seed runs once per disposable database.
	if simulated {
		return "test-tip"
	}
	return "live-tip"
}

func ptr[T any](v T) *T { return &v }
