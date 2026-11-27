# Proposed creator financial screens — review draft

**Status: proposed, awaiting owner approval. No Android code exists for these screens.**
Built on the approved direction (review-v3) with its own `assets/app.css`; the 18 existing
screens, boards and renders are untouched. Nothing here is rendered by CI yet, and nothing
was built locally.

Files (all in this folder):

| File | What it shows |
| --- | --- |
| `19-studio-live.html` | The existing Studio screen with its earnings card driven by the live API |
| `20-payout-request.html` | Payout request sheet: available balance, amount, presets, method, note, validation, honesty note |
| `21-payout-history.html` | Creator payout history: reserved/approved, rejected with reason, cancelled |
| `22-earnings-statement.html` | Creator earnings statement: period, currency, ledger movements, totals, CSV export |
| `23-financial-states.html` | Five non-happy states: requested, insufficient, conflict, empty statement, export too large |

Open them in a browser from the repository; they link to `../assets/app.css` and
`../assets/img/*` exactly like the existing screens.

## 1. The canonical dataset used in the mockups

The numbers are the ones `backend/cmd/e2eseed` now produces and asserts (D-079), so a
reviewer can compare a screen with the seeded database field by field:

| Value | Minor units | Displayed |
| --- | --- | --- |
| Live earnings before the payout | 4000 | $40.00 |
| Approved (reserved) payout | 1000 | $10.00 |
| Available balance | 3000 | $30.00 |
| Simulated earnings, excluded | 720 | $7.20 |
| Minimum request | 1000 | $10.00 |
| Gross on the live tip | 5000 | $50.00 |
| Statement rows (live) | 2 | tip + payout |

The platform share is the difference between gross and net (20% in the deployment default);
the UI shows both and never computes one from the other in the client.

## 2. What changed in Studio, and why (approval point)

The layout, photography, type, spacing, cards and tab bar are unchanged. Three deltas:

1. **Earnings card** now reads `GET /v1/creator/payouts`:
   `balance.available_minor` as the figure, `balance.currency`, a line for excluded test
   money from `net_simulated_minor`, the "No rail" flag replacing "Payouts off", and two
   actions: **Statement** and **Request payout**.
   Removed because nothing can back them: "+ $3,240.00 this month", "MRR $115,048",
   "37 unlocks · 7d" and the 7-day chart (no endpoint returns a daily or monthly series).
2. **Chart card** is replaced by a one-line "Statement · this year" entry that opens the
   real statement. Restoring the chart needs a new analytics endpoint; it is not faked here.
3. **Audience cards** show "—" with the same layout. Member, tier-share and unlock counts
   have no endpoint today; v3's own rule for tier counts was "show — with not tracked yet
   rather than inventing". These are non-financial and outside this phase's scope, but they
   are the same class of fabricated figure, so they are marked rather than kept.

## 3. Screen → API map

### 19 Studio earnings card

| Element | Source | Rule |
| --- | --- | --- |
| Available figure | `GET /v1/creator/payouts` → `balance.available_minor` | Live money only; the server excludes simulated rows (D-070) |
| Currency label | `balance.currency` | One of USD/EUR/GBP |
| "Excludes $7.20 test money" | `GET /v1/creator/earnings` → `net_simulated_minor` | Shown so excluded money is reported, not hidden |
| "No rail" flag | No payout rail exists (D-061/D-065) | Approved means reserved in the ledger; never "paid" |
| Statement action | `GET /v1/creator/earnings/export` | Opens screen 22 |
| Request payout action | `POST /v1/creator/payouts` | Opens screen 20 |
| "2 movements · gross/net" | `GET /v1/creator/earnings/export` headers/rows | Same figures as the CSV it downloads |

### 20 Payout request

| Element | Source | Rule / error |
| --- | --- | --- |
| Available, minimum | `balance.available_minor`, `balance.minimum_minor` | Minimum is 1000 minor units |
| Amount input | `POST /v1/creator/payouts` → `amount_minor` | 1000 … 10,000,000 minor units, integer |
| Presets (Min/Half/All) | client convenience | Values still validated by the server |
| Method radios | `method` | Closed set: `bank`, `wallet`, `manual` (label only; no destination is stored) |
| Note | `note` | ≤ 500 characters, trimmed, optional |
| Submit | `POST /v1/creator/payouts` | 201 `requested`; 400 `invalid_payout`; 403 `forbidden` (no creator profile); 409 `insufficient_balance`; 409 `payout_conflict` (one open request per currency) |
| Honesty inset | Backend has no rail | Approval reserves; no automatic transfer; "paid" is unreachable |
| Cancel (states screen) | `POST /v1/creator/payouts/{id}/cancel` | Owner only; only while `requested`; 409 otherwise |

### 21 Payout history

| Element | Source | Rule |
| --- | --- | --- |
| Summary | `GET /v1/creator/payouts` | `available_minor`, `pending_minor`, `minimum_minor` |
| Row list | `payouts[]` | Fields: `amount_minor`, `currency`, `method`, `status`, `note`, `decision_note`, `decided_by`, `decided_at`, `created_at` |
| Status chips | `status` | `requested`, `approved`, `rejected`, `cancelled` only. `paid`/`failed` exist in the schema but no endpoint sets them, so the UI has no such chip |
| Reason on a refusal | `decision_note` | Server refuses a decision without ≥ 3 characters |
| Event trail | `GET /v1/creator/payouts/{id}` → `events[]` | `action` request/approve/reject/cancel with from→to status |
| Cancel action | `POST …/{id}/cancel` | Only on `requested` |
| "Reserved" wording | Ledger posting `payout:<id>` | Debits the creator payable, credits `platform:payout_clearing` |

### 22 Creator statement

| Element | Source | Rule |
| --- | --- | --- |
| Period presets and From/To | `from`, `to` (YYYY-MM-DD) | Inclusive UTC days; max 400 days |
| Currency | `currency` | USD/EUR/GBP; one currency per statement, never converted |
| Totals: gross / net / entries | CSV rows + `X-Export-Gross-Minor`, `X-Export-Net-Minor`, `X-Export-Rows` | Net includes refunds and payouts as negative lines |
| "1 test row excluded" | `X-Export-Excluded-Simulated` | Default `include_simulated=false` |
| Movement rows | CSV columns `occurred_at, kind, purpose, reference, currency, gross_minor, net_minor, data_class` | Gross only where an intent backs the row |
| Download CSV | `GET /v1/creator/earnings/export` | `text/csv` attachment, no-store, 10 requests/hour |
| "Include test rows" | `include_simulated=true` | The same request with test money included |
| Minor units note | CSV format | Integer minor units and RFC 3339 timestamps in the file |
| No PDF / email | Not implemented | Stated on screen; there is no receipt endpoint |

### 23 States

| State | Trigger | Copy source |
| --- | --- | --- |
| Requested | `POST` 201, `status=requested` | Payout object |
| Not enough | `409 insufficient_balance` | `available_minor` vs requested amount |
| Already open | `409 payout_conflict` | One undecided request per currency (D-079) |
| Rejected | `decision_note` on a `rejected` row | Reason is mandatory server-side; shown in `21-payout-history.html` |
| Empty statement | 200 with zero rows | Period has no movements |
| Too many rows | `400 export_too_large` | 5,000-row cap; refused, not truncated |

## 4. Deliberately not shown

- **Paid / in transit / failed** states: no endpoint sets them and no rail exists.
- **Real transfer instructions, IBAN, wallet addresses, KYC**: the backend stores no
  destination, and a retention/tokenisation story is a prerequisite (see 0014 migration).
- **MRR, growth, 7-day series, commission percentage in the UI**: no endpoints.
- **PDF statement, email receipt**: not built.
- **Finance/admin approval queue**: excluded from this phase by instruction.

## 5. Notes for the reviewer

- A single inline `--onfill` token is set on the new screens only (the v3 light theme never
  defined it, while dark mode does), so filled buttons have readable label text. Existing
  screens are not modified.
- These five drafts were rasterised locally with headless Chromium (one browser process at
  a time, no build step) into `proposed/png/` so the review can happen without a browser.
  All five measure 412 x 915 with no overflowing element.
- A loading state is not drawn as its own screen: the Studio earnings card keeps its height
  with the existing `.skeleton` while `GET /v1/creator/payouts` is in flight.
- Promoting these into the rendered inventory later means moving them to `screens/` as
  `19…23` and running `design-render-v3.yml`. That is a GitHub-hosted render; nothing is
  built locally and no Android build is involved.

## 6. Approval checklist

1. Studio deltas (card fields, chart replaced by a statement entry, audience figures marked).
2. Payout request sheet: amount/preset/validation/method-as-label/honesty inset.
3. Payout history: reserved/rejected/cancelled wording and event trail.
4. Statement: period fields, currency, ledger columns, CSV-only export and limits.
5. States copy and error codes.
6. Where the two entry points live (Studio card → Request payout / Statement, Statement →
   history) — or a different route through the app.
