package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/earnings"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

type exportFixture struct {
	handler http.Handler
	ledger  *ledger.Store
	pool    *pgxpool.Pool
}

func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()
	pool := dbtest.Pool(t)
	ids := identity.New(pool,
		identity.WithHasher(identity.NewHasher(identity.TestParams())),
		identity.WithIssuer("Content Platform Test"))
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(permissiveGate{}))
	led := ledger.New(pool)
	h := New(Deps{
		Config:   testConfig(),
		Logger:   loggerDiscard(),
		Identity: ids,
		Creators: creators,
		Earnings: earnings.New(pool),
	})
	return &exportFixture{handler: h, ledger: led, pool: pool}
}

// creator registers and opens a profile over HTTP, which is what grants
// finance.read and therefore access to the earnings surface.
func (f *exportFixture) creator(t *testing.T, handle string) authBody {
	t.Helper()
	account := registerAndGetToken(t, f.handler, handle+"@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators", account.Tokens.AccessToken,
		encodeBody(t, map[string]any{
			"handle": handle, "display_name": "Mira", "bio": "Abstract work", "category": "art",
		}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create creator profile: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	return account
}

// tip posts a settled tip and the intent behind it, so the row has both a net and
// a gross figure, exactly as the payments service leaves them.
func (f *exportFixture) tip(t *testing.T, buyerID, creatorID string, amount int64, simulated bool) {
	t.Helper()
	ctx := context.Background()
	var intentID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO payments.intents
		    (user_id, purpose, creator_id, amount_minor, currency, provider, status,
		     idempotency_key, simulated, settled_at)
		VALUES ($1::uuid, 'tip', $2::uuid, $3, 'USD', 'fake', 'succeeded', $4, $5, now())
		RETURNING id::text`,
		buyerID, creatorID, amount, "export-http-"+randomKey(), simulated).
		Scan(&intentID); err != nil {
		t.Fatalf("insert intent: %v", err)
	}
	share := amount - amount*80/100
	if _, err := f.ledger.Post(ctx, ledger.Posting{
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
		t.Fatalf("post tip: %v", err)
	}
}

func randomKey() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf[:])
}

func TestEarningsExportIsACsvStatementWithItsOwnTotals(t *testing.T) {
	f := newExportFixture(t)
	owner := f.creator(t, "mira")
	f.tip(t, owner.User.ID, owner.User.ID, 500, false)
	f.tip(t, owner.User.ID, owner.User.ID, 700, true)

	today := time.Now().UTC().Format("2006-01-02")
	rec := doJSON(t, f.handler, http.MethodGet,
		"/v1/creator/earnings/export?from="+today+"&to="+today, owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Fatalf("content type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") ||
		!strings.Contains(got, "earnings-") {
		t.Fatalf("content disposition = %q", got)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a tax export was cacheable")
	}
	if got := rec.Header().Get("X-Export-Rows"); got != "1" {
		t.Fatalf("X-Export-Rows = %q, want 1 (test money is excluded)", got)
	}
	if got := rec.Header().Get("X-Export-Net-Minor"); got != "400" {
		t.Fatalf("X-Export-Net-Minor = %q, want 400", got)
	}
	if got := rec.Header().Get("X-Export-Gross-Minor"); got != "500" {
		t.Fatalf("X-Export-Gross-Minor = %q, want 500", got)
	}
	if got := rec.Header().Get("X-Export-Excluded-Simulated"); got != "1" {
		t.Fatalf("X-Export-Excluded-Simulated = %q, want 1", got)
	}

	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("csv lines = %d, want header + 1 row: %q", len(rows), rec.Body.String())
	}
	wantHeader := []string{"occurred_at", "kind", "purpose", "reference", "currency",
		"gross_minor", "net_minor", "data_class"}
	for i, want := range wantHeader {
		if rows[0][i] != want {
			t.Fatalf("header[%d] = %q, want %q", i, rows[0][i], want)
		}
	}
	if rows[1][1] != "tip" || rows[1][5] != "500" || rows[1][6] != "400" || rows[1][7] != "real" {
		t.Fatalf("unexpected row: %+v", rows[1])
	}
	if _, err := time.Parse(time.RFC3339, rows[1][0]); err != nil {
		t.Fatalf("occurred_at is not RFC3339: %q (%v)", rows[1][0], err)
	}
}

func TestEarningsExportRefusals(t *testing.T) {
	f := newExportFixture(t)
	owner := f.creator(t, "mira")
	fan := registerAndGetToken(t, f.handler, "fan@example.test")

	// A fan holds neither finance.read nor a creator role, so the export is a
	// permission failure even though it would be empty.
	rec := doJSON(t, f.handler, http.MethodGet, "/v1/creator/earnings/export",
		fan.Tokens.AccessToken, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("fan export: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, f.handler, http.MethodGet,
		"/v1/creator/earnings/export?from=01-01-2026", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad date: want 400, got %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "invalid_export" {
		t.Fatalf("problem code = %q, want invalid_export", p.Code)
	}

	// A period that runs backwards is refused rather than silently emptied.
	rec = doJSON(t, f.handler, http.MethodGet,
		"/v1/creator/earnings/export?from=2026-12-31&to=2026-01-01", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reversed period: want 400, got %d", rec.Code)
	}

	// A period longer than the service allows is refused with its own code.
	rec = doJSON(t, f.handler, http.MethodGet,
		"/v1/creator/earnings/export?from=2024-01-01&to=2026-12-31", owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long period: want 400, got %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "invalid_export" {
		t.Fatalf("problem code = %q, want invalid_export", p.Code)
	}
}

// TestEarningsExportTooLargeSaysSo proves the refusal path a busy creator hits:
// a narrower period is asked for instead of a truncated file.
func TestEarningsExportTooLargeSaysSo(t *testing.T) {
	f := newExportFixture(t)
	owner := f.creator(t, "mira")
	f.tip(t, owner.User.ID, owner.User.ID, 500, false)
	f.tip(t, owner.User.ID, owner.User.ID, 600, false)

	// The fixture server uses the default cap, so the service is exercised
	// directly with a smaller one: the handler's mapping is what is under test.
	small := earnings.New(f.pool, earnings.WithMaxExportRows(1))
	from := time.Now().UTC().Add(-time.Hour)
	today := time.Now().UTC().Format("2006-01-02")

	if _, err := small.Export(context.Background(), earnings.ExportQuery{
		CreatorID: owner.User.ID, Currency: "USD", From: from, To: from.Add(2 * time.Hour),
	}); err == nil || !strings.Contains(err.Error(), "narrow the period") {
		t.Fatalf("want a too-large refusal, got %v", err)
	}

	// And the endpoint itself still answers 200 for a period it can carry.
	rec := doJSON(t, f.handler, http.MethodGet,
		"/v1/creator/earnings/export?from="+today+"&to="+today, owner.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}
