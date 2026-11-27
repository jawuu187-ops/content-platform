# Proposed creator financial screens — review draft r2

**Status: adjusted after owner review; awaiting approval. No Android code exists for these screens.**
Direction review-v3 only: its stylesheet, imagery, type and spacing are reused; the 18 original
screens, boards and renders are untouched.

| File | What it shows |
| --- | --- |
| `19-studio-live.html` | Studio, unchanged layout, earnings card wired to the live API |
| `20-payout-request.html` | Payout request sheet with no operational method and the reserve warning before the action |
| `21-payout-history.html` | Requested / approved-as-reserved / rejected / cancelled, with reasons and event trail |
| `22-earnings-statement.html` | Creator statement: period, currency, ledger rows, gross vs net, CSV export |
| `23-financial-states.html` | Five states verified against real responses: requested, insufficient, conflict, empty, too-large |

## 1. Verification method

Every figure and message below was captured from a running local API (`APP_ENV=local`,
fake payment provider) seeded by `cmd/e2eseed`, on a disposable database, then cross-checked
against the CSV body and response headers. The commands and payloads are quoted verbatim.

Captured canonical state (creator `Mira Vale`, USD):

| Source | Value |
| --- | --- |
| `GET /v1/creator/payouts` → `balance` | `available_minor: 3000`, `pending_minor: 0`, `minimum_minor: 1000`, `currency: USD` |
| Same → `payouts[]` | approved 1000 (method label `bank`), rejected 1500 (`manual`), cancelled 2000 (`manual`) |
| `GET /v1/creator/earnings` | live net `3000`, test net `720`, live count 1, test count 1, bucket tip gross `5000` |
| `GET /v1/creator/earnings/export` headers | `X-Export-Rows: 2`, `X-Export-Gross-Minor: 5000`, `X-Export-Net-Minor: 3000`, `X-Export-Excluded-Simulated: 1`, `X-Export-Currency: USD` |
| CSV body | `tip,…,5000,4000,real` and `payout,…,0,-1000,real` |
| `include_simulated=true` | rows 3, gross 5900, net 3720, excluded 0, one `test` row |
| `currency=EUR` | rows 0, gross 0, net 0 — no conversion |

## 2. Studio (19): what is live, and the one gap

The layout, cards, photography, typography and tab bar are unchanged.

**Live, endpoint-backed:**

| Element | Field |
| --- | --- |
| Available balance | `balance.available_minor` (3000 → $30.00) |
| Currency | `balance.currency` |
| Excluded test money | `net_simulated_minor` (720 → $7.20) |
| No-rail flag | no payout rail exists; approval reserves only |
| Statement entry | export headers: 2 movements, gross $50.00, net $30.00 |

Removed because no endpoint returns them: the 7-day chart, "+$3,240 this month", "MRR
$115,048", "37 unlocks · 7d". They were invented in v3 and cannot be shown as real.

**Audience statistics — exact gap, reported rather than invented:**

- Data exists in the database: `creator.subscriptions` (status, tier), `creator.entitlements`
  (scope/source/expiry), `creator.post_unlocks` (granted_at), `creator.posts`, `creator.tiers`.
  For the seeded account the real values are 1 active subscriber-scope entitlement (source
  `admin`), 0 active subscriptions, 0 unlocks in the last 7 days.
- **No endpoint exposes any of them.** There is no `analytics` service (the package directory
  is empty) and no creator stats route; `GET /v1/creators/{handle}` and `/tiers` return no
  counts.
- Therefore the mockup shows the three tiles with no figure and the label only. Nothing is
  fabricated. To make the tiles live, one small read-only endpoint is needed — proposed, not
  implemented:

```sql
-- GET /v1/creator/stats?currency=USD  (creator only; payouts.request or finance.read)
SELECT
  (SELECT count(*) FROM creator.entitlements e
    WHERE e.creator_id = $1 AND e.scope IN ('subscriber','all')
      AND e.revoked_at IS NULL AND (e.expires_at IS NULL OR e.expires_at > now())) AS members,
  (SELECT count(*) FROM creator.subscriptions s
    WHERE s.creator_id = $1 AND s.status = 'active'
      AND s.current_period_end > now()) AS paid_members,
  (SELECT count(*) FROM creator.post_unlocks u JOIN creator.posts p ON p.id = u.post_id
    WHERE p.creator_id = $1 AND u.granted_at > now() - interval '7 days') AS unlocks_7d,
  (SELECT t.id FROM creator.tiers t
    WHERE t.creator_id = $1 AND t.is_active ORDER BY t.sort_order LIMIT 1) AS top_tier;
```

"On Studio" would be `paid_members on the top tier / paid_members`; with the seeded account
it is 0 of 0 (no paid members), which is why no number is drawn. **Approval request:** allow
this endpoint with the Android work, or leave the tiles label-only.

## 3. Payout request (20)

- No bank, wallet or manual option is offered as if it worked. The method row is
  **informational**: "No bank, wallet or account is connected… the API defaults it to
  `manual`". The server accepts a label (`manual`, `bank`, `wallet`) and stores no
  destination; omitting it yields `manual` (verified in the 201 body).
- The reserve warning sits **immediately above the CTA**, not after it:
  "Approval reserves $20.00 in your ledger. It does not transfer money." followed by the
  no-rail explanation.
- Bounds and validation are the server's: 1000 … 10,000,000 minor units (verified 400
  `invalid_payout` for 100), one undecided request per currency (verified 409
  `payout_conflict`), available balance (verified 409 `insufficient_balance`), note ≤ 500
  (server trims and limits), currency USD/EUR/GBP.
- The `method` on the seeded approved row is displayed in history as "method label: bank · no
  destination stored", never as a connected rail.

## 4. Payout history (21)

Verified list payload states: `approved` (reserved, decision note), `rejected` (decision note
required, ≥ 3 characters), `cancelled` (nothing reserved). Detail events are exactly two per
row: `request` then `approve`/`reject`/`cancel`, with `from_status`/`to_status`.

- No paid or transferred state is shown; no endpoint sets `paid`/`failed`.
- "Reserved" wording matches the ledger: `payout:<id>` debits the creator payable and credits
  `platform:payout_clearing`.
- `decision_note` is displayed as finance wrote it. The approval-note text in the mockup is
  illustrative (notes are free text); amounts, statuses, method labels and event counts come
  from the captured responses.

## 5. Creator statement (22) — totals checked against the CSV

| Element | Value (from headers/body) |
| --- | --- |
| Net movement | `X-Export-Net-Minor` 3000 → $30.00 |
| Gross | `X-Export-Gross-Minor` 5000 → $50.00 |
| Entries | `X-Export-Rows` 2 |
| Test rows excluded | `X-Export-Excluded-Simulated` 1 |
| Tip row | gross 5000 → $50.00, net 4000 → +$40.00, `real` |
| Payout row | gross 0 → "—", net −1000 → −$10.00, `real` |
| By kind | tips +$40.00, payouts −$10.00 |

Gross is what the fan was charged; net is the creator's movement after the platform share and
includes refunds and payouts as negative lines. Periods are inclusive UTC days; over 400 days
is 400 `invalid_export` ("the period may span at most 400 days", verified); over 5,000 rows is
400 `export_too_large` ("this period holds too many movements for one export…", verified).
Currency is one per file and never converted (EUR export: 0 rows). CSV only — no PDF, no
email receipt, no pretend download.

## 6. States (23) — verified responses

| Card | Status | Verified body |
| --- | --- | --- |
| Requested | 201 | `payout.status = requested`; `method` defaults to `manual` when omitted |
| Insufficient | 409 `insufficient_balance` | "asked for 9000, 3000 is available" |
| Open request | 409 `payout_conflict` | "a payout request in USD is already open" |
| Empty period | 200 | `X-Export-Rows: 0`, `X-Export-Net-Minor: 0` |
| Too many rows | 400 `export_too_large` | "this period holds too many movements for one export; request a narrower period" |

The below-minimum refusal (400 `invalid_payout`, "the smallest payout is 1000 minor units") is
verified but not drawn as a card: the request sheet already states the minimum before entry.

## 7. Navigation and Android back behaviour

| From | To | Back |
| --- | --- | --- |
| Studio earnings card | Request payout (sheet) | Closes the sheet; Studio unchanged |
| Studio earnings card | Statement | Returns to Studio |
| Statement (header action) | Payout history | Returns to Statement |
| Payout history | Request detail | Returns to Payout history |
| Request sheet after 201 | Studio card shows the new pending amount | — |

No financial screen becomes a fifth tab; they are pushed screens and one sheet, so Android back
pops in the order above and never leaves the Studio tab.

## 8. Deliberately absent

Paid/failed/transferred states; connected destinations (bank, wallet, IBAN, KYC); daily or
monthly charts; MRR; commission percentage in the UI; PDF or email statements; finance queue;
audience numbers until an endpoint exists.

## 9. Approval checklist

1. Studio: live card, statement entry, audience tiles label-only with the endpoint gap above.
2. Request sheet: informational method, reserve warning placement and copy.
3. History: reserved/rejected/cancelled wording, note and two-event trail.
4. Statement: gross/net split, period and currency controls, CSV-only export and limits.
5. States: the five cards and their exact codes.
6. Navigation and back behaviour.
7. Whether to add `GET /v1/creator/stats` with the implementation so the audience tiles can
   show real counts (recommended), or to ship them label-only.
