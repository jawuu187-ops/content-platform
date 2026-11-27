package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/ledger"
	"github.com/jawuu187-ops/content-platform/backend/internal/payouts"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

type payoutFixture struct {
	handler http.Handler
	ids     *identity.Service
	ledger  *ledger.Store
	pool    *pgxpool.Pool
}

func newPayoutFixture(t *testing.T) *payoutFixture {
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
		Payouts:  payouts.New(pool, led),
	})
	return &payoutFixture{handler: h, ids: ids, ledger: led, pool: pool}
}

// creatorWithMoney registers an account, opens a creator profile over HTTP (which
// is what grants the creator role) and credits the payable account as a
// settlement would.
func (f *payoutFixture) creatorWithMoney(t *testing.T, handle string, amountMinor int64) (authBody, string) {
	t.Helper()
	account := registerAndGetToken(t, f.handler, handle+"@example.test")
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators", account.Tokens.AccessToken,
		encodeBody(t, map[string]any{
			"handle": handle, "display_name": "Mira", "bio": "Abstract work", "category": "art",
		}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create creator profile: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if amountMinor > 0 {
		if _, err := f.ledger.Post(context.Background(), ledger.Posting{
			Kind:      ledger.TxnSubscription,
			Reference: "http-credit-" + account.User.ID,
			Currency:  "USD",
			Entries: []ledger.Entry{
				{AccountCode: ledger.AccountProviderClearing, AmountMinor: amountMinor},
				{AccountCode: "creator:" + account.User.ID + ":payable", AmountMinor: -amountMinor},
			},
		}); err != nil {
			t.Fatalf("credit the creator: %v", err)
		}
	}
	return account, account.User.ID
}

func (f *payoutFixture) finance(t *testing.T, email string) authBody {
	t.Helper()
	account := registerAndGetToken(t, f.handler, email)
	// The role-grant route needs an existing admin; the grant itself is proven in
	// package identity, so the fixture uses the service directly.
	if _, err := f.ids.GrantRole(context.Background(),
		identity.Principal{UserID: "fixture-admin", Roles: []string{identity.RoleAdmin}},
		account.User.ID, identity.RoleFinance, ""); err != nil {
		t.Fatalf("grant finance role: %v", err)
	}
	// The token carries roles resolved per request, so the same token now holds
	// payouts.approve.
	return account
}

func TestPayoutRequestApproveFlowOverHTTP(t *testing.T) {
	f := newPayoutFixture(t)
	creatorAccount, _ := f.creatorWithMoney(t, "mira", 2500)
	finance := f.finance(t, "finance@example.test")

	rec := doJSON(t, f.handler, http.MethodGet, "/v1/creator/payouts", creatorAccount.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list payouts: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var list struct {
		Payouts []payouts.Request `json:"payouts"`
		Balance payouts.Balance   `json:"balance"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Balance.AvailableMinor != 2500 || list.Balance.MinimumMinor != payouts.MinAmountMinor {
		t.Fatalf("unexpected balance: %+v", list.Balance)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a balance was cacheable")
	}

	rec = doJSON(t, f.handler, http.MethodPost, "/v1/creator/payouts", creatorAccount.Tokens.AccessToken,
		encodeBody(t, map[string]any{"amount_minor": 2000, "currency": "USD", "note": "august"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("request payout: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created struct {
		Payout payouts.Request `json:"payout"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode payout: %v", err)
	}
	if created.Payout.Status != payouts.StatusRequested {
		t.Fatalf("status = %q, want requested", created.Payout.Status)
	}

	// A creator cannot read the finance queue even though they can request.
	rec = doJSON(t, f.handler, http.MethodGet, "/v1/admin/payouts", creatorAccount.Tokens.AccessToken, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("creator reading the queue: want 403, got %d", rec.Code)
	}

	rec = doJSON(t, f.handler, http.MethodGet, "/v1/admin/payouts", finance.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("finance queue: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, f.handler, http.MethodPost,
		"/v1/admin/payouts/"+created.Payout.ID+"/approve", finance.Tokens.AccessToken,
		encodeBody(t, map[string]any{"note": "verified"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var approved struct {
		Payout payouts.Request `json:"payout"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &approved); err != nil {
		t.Fatalf("decode approval: %v", err)
	}
	if approved.Payout.Status != payouts.StatusApproved || approved.Payout.DecidedBy != finance.User.ID {
		t.Fatalf("unexpected approval: %+v", approved.Payout)
	}

	// Approval reserved the money in the ledger, so the balance drops.
	rec = doJSON(t, f.handler, http.MethodGet, "/v1/creator/payouts", creatorAccount.Tokens.AccessToken, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list after approval: %v", err)
	}
	if list.Balance.AvailableMinor != 500 {
		t.Fatalf("available after approval = %d, want 500", list.Balance.AvailableMinor)
	}

	// A second approval of the same request is a conflict, not a 500.
	rec = doJSON(t, f.handler, http.MethodPost,
		"/v1/admin/payouts/"+created.Payout.ID+"/approve", finance.Tokens.AccessToken, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second approval: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if p := decodeProblem(t, rec); p.Code != "payout_conflict" {
		t.Fatalf("problem code = %q, want payout_conflict", p.Code)
	}
}

func TestPayoutRefusalsOverHTTP(t *testing.T) {
	f := newPayoutFixture(t)
	creatorAccount, _ := f.creatorWithMoney(t, "mira", 2500)
	finance := f.finance(t, "finance@example.test")
	fan := registerAndGetToken(t, f.handler, "fan@example.test")

	// More than the live balance is refused with its own code, so a client can
	// tell "you asked for too much" from "your input was malformed".
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creator/payouts", creatorAccount.Tokens.AccessToken,
		encodeBody(t, map[string]any{"amount_minor": 9000, "currency": "USD"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("over-balance: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if p := decodeProblem(t, rec); p.Code != "insufficient_balance" {
		t.Fatalf("problem code = %q, want insufficient_balance", p.Code)
	}

	// A fan has no creator profile and therefore no payable balance to spend.
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/creator/payouts", fan.Tokens.AccessToken,
		encodeBody(t, map[string]any{"amount_minor": 2000, "currency": "USD"}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("fan request: want 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Unknown fields are refused rather than ignored: no client gets to name a
	// creator or a status in a payout request.
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/creator/payouts", creatorAccount.Tokens.AccessToken,
		`{"amount_minor":2000,"currency":"USD","creator_id":"someone-else"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}

	// A refusal from finance needs a reason; an empty note is a 400.
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/creator/payouts", creatorAccount.Tokens.AccessToken,
		encodeBody(t, map[string]any{"amount_minor": 2000, "currency": "USD"}))
	var created struct {
		Payout payouts.Request `json:"payout"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode payout: %v", err)
	}
	rec = doJSON(t, f.handler, http.MethodPost, "/v1/admin/payouts/"+created.Payout.ID+"/reject",
		finance.Tokens.AccessToken, encodeBody(t, map[string]any{"note": ""}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reject without a reason: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Approving a request that does not exist is a 404 with the shared shape.
	rec = doJSON(t, f.handler, http.MethodPost,
		"/v1/admin/payouts/00000000-0000-4000-8000-000000000000/approve", finance.Tokens.AccessToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("approve unknown: want 404, got %d (%s)", rec.Code, rec.Body.String())
	}
}
