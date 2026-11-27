package earnings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const goodPassword = "a-very-long-password"

type exportFixture struct {
	pool   *pgxpool.Pool
	ledger *ledger.Store
	ids    *identity.Service
	svc    *Service
}

func newExportFixture(t *testing.T, opts ...Option) *exportFixture {
	t.Helper()
	pool := dbtest.Pool(t)
	return &exportFixture{
		pool:   pool,
		ledger: ledger.New(pool),
		ids:    identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams()))),
		svc:    New(pool, opts...),
	}
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

// creator registers an account and gives it a creator profile, which is the id
// the payable account is keyed by.
func (f *exportFixture) creator(t *testing.T) identity.Principal {
	t.Helper()
	p := f.account(t, "creator-")
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO creator.profiles (user_id, handle, display_name, bio, category, is_published)
		VALUES ($1::uuid, $2, 'Mira', 'Abstract work', 'art', true)`,
		p.UserID, "studio"+randomHex(4)); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return p
}

func (f *exportFixture) account(t *testing.T, prefix string) identity.Principal {
	t.Helper()
	res, err := f.ids.Register(context.Background(), prefix+randomHex(6)+"@example.test",
		goodPassword, identity.DeviceInfo{Fingerprint: "d1"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal
}

// intent inserts a settled tip intent, which is what supplies the gross figure on
// an income row. Tips need no tier or post, so the fixture stays small.
func (f *exportFixture) intent(t *testing.T, fanID, creatorID string, amount int64, simulated bool) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO payments.intents
		    (user_id, purpose, creator_id, amount_minor, currency, provider, status,
		     idempotency_key, simulated, settled_at)
		VALUES ($1::uuid, 'tip', $2::uuid, $3, 'USD', 'fake', 'succeeded', $4, $5, now())
		RETURNING id::text`,
		fanID, creatorID, amount, "export-"+randomHex(8), simulated).Scan(&id); err != nil {
		t.Fatalf("insert intent: %v", err)
	}
	return id
}

// income posts a settlement the way the payments service does: 20% platform share.
func (f *exportFixture) income(t *testing.T, creatorID, intentID string, amount int64, simulated bool) {
	t.Helper()
	share := amount - amount*80/100
	if _, err := f.ledger.Post(context.Background(), ledger.Posting{
		Kind:      ledger.TxnTip,
		Reference: intentID,
		Currency:  "USD",
		Simulated: simulated,
		Entries: []ledger.Entry{
			{AccountCode: ledger.AccountProviderClearing, AmountMinor: amount},
			{AccountCode: "creator:" + creatorID + ":payable", AmountMinor: -(amount - share)},
			{AccountCode: ledger.AccountRevenue, AmountMinor: -share},
		},
	}); err != nil {
		t.Fatalf("post income: %v", err)
	}
}

// refund reverses an income posting, as a compensating transaction.
func (f *exportFixture) refund(t *testing.T, creatorID, intentID string, amount int64) {
	t.Helper()
	share := amount - amount*80/100
	if _, err := f.ledger.Post(context.Background(), ledger.Posting{
		Kind:      ledger.TxnRefund,
		Reference: "refund:" + intentID,
		Currency:  "USD",
		Entries: []ledger.Entry{
			{AccountCode: "creator:" + creatorID + ":payable", AmountMinor: amount - share},
			{AccountCode: ledger.AccountRevenue, AmountMinor: share},
			{AccountCode: ledger.AccountProviderClearing, AmountMinor: -amount},
		},
	}); err != nil {
		t.Fatalf("post refund: %v", err)
	}
}

func (f *exportFixture) payout(t *testing.T, creatorID string, amount int64) string {
	t.Helper()
	ref := "payout:" + randomHex(16)
	if _, err := f.ledger.Post(context.Background(), ledger.Posting{
		Kind:      ledger.TxnPayout,
		Reference: ref,
		Currency:  "USD",
		Entries: []ledger.Entry{
			{AccountCode: "creator:" + creatorID + ":payable", AmountMinor: amount},
			{AccountCode: ledger.AccountPayoutClearing, AmountMinor: -amount},
		},
	}); err != nil {
		t.Fatalf("post payout: %v", err)
	}
	return ref
}

func (f *exportFixture) window(t *testing.T) (time.Time, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	return now.Add(-time.Hour), now.Add(time.Hour)
}

func TestExportCarriesIncomeRefundsAndPayoutsWithTheirSigns(t *testing.T) {
	f := newExportFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	fan := f.account(t, "fan-")
	from, to := f.window(t)

	sub := f.intent(t, fan.UserID, creator.UserID, 1200, false)
	f.income(t, creator.UserID, sub, 1200, false)
	f.refund(t, creator.UserID, sub, 1200)
	f.payout(t, creator.UserID, 300)

	exp, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "USD", From: from, To: to,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Three movements: the sale, its refund and the payout. The refund is present
	// because a statement that only counted sales would overstate income.
	if len(exp.Rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(exp.Rows), exp.Rows)
	}
	byKind := map[string][]ExportRow{}
	for _, row := range exp.Rows {
		byKind[row.Kind] = append(byKind[row.Kind], row)
	}

	sale := byKind[ledger.TxnTip]
	if len(sale) != 1 {
		t.Fatalf("sale rows = %d, want 1", len(sale))
	}
	if sale[0].GrossMinor != 1200 || sale[0].NetMinor != 960 || sale[0].Purpose != "tip" {
		t.Fatalf("sale row wrong: %+v", sale[0])
	}
	if sale[0].Reference != sub || sale[0].Currency != "USD" || sale[0].Simulated {
		t.Fatalf("sale row identity wrong: %+v", sale[0])
	}

	refund := byKind[ledger.TxnRefund]
	if len(refund) != 1 || refund[0].NetMinor != -960 || refund[0].GrossMinor != 0 {
		t.Fatalf("refund row wrong: %+v", refund)
	}

	payouts := byKind[ledger.TxnPayout]
	if len(payouts) != 1 || payouts[0].NetMinor != -300 || payouts[0].Purpose != "" {
		t.Fatalf("payout row wrong: %+v", payouts)
	}

	// Net is the creator's actual movement (960 - 960 - 300); gross is income only.
	if exp.NetMinor != -300 {
		t.Fatalf("net = %d, want -300", exp.NetMinor)
	}
	if exp.GrossMinor != 1200 {
		t.Fatalf("gross = %d, want 1200", exp.GrossMinor)
	}
	if exp.ExcludedSimulated != 0 {
		t.Fatalf("excluded simulated = %d, want 0", exp.ExcludedSimulated)
	}
}

func TestExportExcludesSimulatedMoneyAndStatesWhatItLeftOut(t *testing.T) {
	f := newExportFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	fan := f.account(t, "fan-")
	from, to := f.window(t)

	live := f.intent(t, fan.UserID, creator.UserID, 500, false)
	f.income(t, creator.UserID, live, 500, false)
	testIntent := f.intent(t, fan.UserID, creator.UserID, 500, true)
	f.income(t, creator.UserID, testIntent, 500, true)

	real, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "USD", From: from, To: to,
	})
	if err != nil {
		t.Fatalf("export real: %v", err)
	}
	if len(real.Rows) != 1 || real.NetMinor != 400 {
		t.Fatalf("real export leaked test money: %+v", real)
	}
	// The exclusion is reported rather than hidden.
	if real.ExcludedSimulated != 1 {
		t.Fatalf("excluded simulated = %d, want 1", real.ExcludedSimulated)
	}

	withTest, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "USD", From: from, To: to, IncludeSimulated: true,
	})
	if err != nil {
		t.Fatalf("export with test data: %v", err)
	}
	if len(withTest.Rows) != 2 || withTest.NetMinor != 800 {
		t.Fatalf("export including test money wrong: %+v", withTest)
	}
	var sawTest bool
	for _, row := range withTest.Rows {
		if row.Simulated {
			sawTest = true
		}
	}
	if !sawTest {
		t.Fatal("the test row is not marked simulated")
	}
}

func TestExportIsPerCurrencyAndRespectsThePeriod(t *testing.T) {
	f := newExportFixture(t)
	ctx := context.Background()
	creator := f.creator(t)
	fan := f.account(t, "fan-")
	from, to := f.window(t)

	live := f.intent(t, fan.UserID, creator.UserID, 500, false)
	f.income(t, creator.UserID, live, 500, false)

	// A different currency is a different statement: converting it would invent a
	// rate, so the export simply does not contain it.
	eur, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "EUR", From: from, To: to,
	})
	if err != nil {
		t.Fatalf("eur export: %v", err)
	}
	if len(eur.Rows) != 0 || eur.NetMinor != 0 {
		t.Fatalf("eur export carried USD rows: %+v", eur)
	}

	// A period that has not started yet is empty, not an error.
	future, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "USD",
		From: to.Add(24 * time.Hour), To: to.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("future export: %v", err)
	}
	if len(future.Rows) != 0 {
		t.Fatalf("future period carried %d rows", len(future.Rows))
	}
}

func TestExportRefusesRatherThanTruncatesAndValidatesTheRequest(t *testing.T) {
	f := newExportFixture(t, WithMaxExportRows(1))
	ctx := context.Background()
	creator := f.creator(t)
	fan := f.account(t, "fan-")
	from, to := f.window(t)

	for i := 0; i < 2; i++ {
		id := f.intent(t, fan.UserID, creator.UserID, 500, false)
		f.income(t, creator.UserID, id, 500, false)
	}

	if _, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "USD", From: from, To: to,
	}); !errors.Is(err, ErrExportTooLarge) {
		t.Fatalf("over-cap export: want ErrExportTooLarge, got %v", err)
	}

	if _, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "USD", From: to, To: from,
	}); !errors.Is(err, ErrInvalidExport) {
		t.Fatalf("reversed period: want ErrInvalidExport, got %v", err)
	}
	if _, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: creator.UserID, Currency: "EGP", From: from, To: to,
	}); !errors.Is(err, ErrInvalidExport) {
		t.Fatalf("unsupported currency: want ErrInvalidExport, got %v", err)
	}
	if _, err := f.svc.Export(ctx, ExportQuery{
		CreatorID: "", Currency: "USD", From: from, To: to,
	}); !errors.Is(err, ErrInvalidExport) {
		t.Fatalf("missing creator: want ErrInvalidExport, got %v", err)
	}
	long := ExportQuery{
		CreatorID: creator.UserID, Currency: "USD",
		From: from, To: from.AddDate(2, 0, 0),
	}
	if _, err := f.svc.Export(ctx, long); !errors.Is(err, ErrInvalidExport) {
		t.Fatalf("period over the cap: want ErrInvalidExport, got %v", err)
	}
}
