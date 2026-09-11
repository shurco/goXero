# /app/accounting/bank-accounts — Xero Manage Bank Accounts parity

Page: `web/src/routes/app/accounting/bank-accounts/+page.svelte` (the only file changed).
Reference: `https://go.xero.com/app/!!6Sp3/manage-bank-accounts`, measured live in an Orca
browser tab at viewport 882px (Xero's content column 842 wide, x=20).

## 1. The defect — `accountId` is not a parameter the API reads

`BankTransactionHandler.List` (`internal/handlers/bank.go:30`) reads `bankAccountId`.
The page was sending `accountId`, so the filter was ignored and every card computed its
figures from the organisation's whole transaction set.

```
GET /api/v1/bank-transactions?accountId=<a>&pageSize=500      # both accounts
  account 34f257ca… (090): count: 49  byAccount: {34f257ca…: 48, a284bfed…: 1}
  account a284bfed… (091): count: 49  byAccount: {34f257ca…: 48, a284bfed…: 1}

GET /api/v1/bank-transactions?bankAccountId=<a>&pageSize=500  # filters correctly
  account 34f257ca… (090): count: 48  byAccount: {34f257ca…: 48}
  account a284bfed… (091): count:  1  byAccount: {a284bfed…: 1}
```

Before the fix both cards rendered "Up to date / (US$1,473.79) / -US$1,473.79".
After: `orca network --page <tab>` over a fresh load shows **0** `/api/v1/bank-transactions`
requests; the page now calls only `/api/v1/statement-lines/balance?bankAccountId=…` and
`/api/v1/statement-lines?bankAccountId=…&unreconciled=false&pageSize=500` (the latter only
to draw the chart).

## 2. Figures now come from `statementApi.balance(id)`

| | 090 Business Bank Account | 091 Business Savings Account |
|---|---|---|
| `balance` endpoint | `LedgerBalance: "7430.22"`, `StatementBalance: "12392.72"`, `UnreconciledCount: 27`, `ReconciledCount: 74`, `LastStatementEnd: 2026-09-10T00:00:00Z` | `LedgerBalance: "-250"`, `StatementBalance: "1250"`, `UnreconciledCount: 2`, `ReconciledCount: 1`, `LastStatementEnd: 2026-03-09T00:00:00Z` |
| card, read from the live DOM | `Business Bank Account` / `090-8007-006543` / `Reconcile 27 items` / `US$7,430.22` / `Statement balance(10 Sept) = US$12,392.72` | `Business Savings Account` / `121314-121314` / `Reconcile 2 items` / `-US$250.00` / `Statement balance(9 Mar) = US$1,250.00` |

Account 090 agrees with Xero's own card ("Balance in Xero 7,430.22", "Reconcile 27 items").
The two cards are no longer identical.

The empty state is decided from the same response — `ReconciledCount + UnreconciledCount === 0`
— not from a transaction fetch, per the brief. `UnreconciledCount === 0` draws no button at all.

## 3. Geometry, Xero measured vs goXero measured

Both columns are `getBoundingClientRect()` + `getComputedStyle()` read from the live DOM.

| Element | Xero | goXero |
|---|---|---|
| page background | `rgb(242, 243, 244)` | surface wrapper `0,56 882x796` `rgb(242, 243, 244)` |
| `h1` | `16,130 133x24` 17px/700 `rgb(0,10,30)` lh 24px `padding-right 12px` | `20,98 135x24` 17px/700 `rgb(0,10,30)` lh 24px `padding-right 12px` |
| heading block | 60px tall, children centred (h1 +18, buttons +14) | `20,80 842x60`, h1 +18, buttons +14 |
| "Manage bank rules" | `527,126 144x32` white, `1px solid rgb(166,169,176)`, r3, `5px 12px`, 13px/700, `rgb(0,120,200)` | `519,94 149x32` white, `1px solid rgb(166,169,176)`, r3, `5px 12px`, 13px/700, `rgb(0,120,200)` |
| "Add bank account" | `683,126 139x32` `rgb(0,120,200)`, r3, `5px 12px`, 13px/700, white | `680,94 142x32` `rgb(0,120,200)`, r3, `5px 12px`, 13px/700, white |
| icon toggle | `834,126 32x32`, 12px right of the button | `834,94 32x32`, 12px right of the button |
| "2 accounts · N items" line | absent | removed (`document.querySelectorAll('p')` matching /account/ → `[]`) |
| card gap / first card | 24px; cards start 20px below the heading block | 24px; `20,160` |
| card | `20,192 842x323` white, r3, `box-shadow rgba(0,10,30,.2) 0 0 0 1px`, no border | `20,160 842x323` white, r3, same shadow, no border |
| card header | 90px, `padding 16px 20px`; overflow btn `802,217 40x40` (25 from top, 20 from right) | 90px, `padding 16px 20px`; overflow btn `802,185 40x40` (25 from top, 20 from right) |
| account name | `40,208 762x28` 17px/700 `rgb(0,120,200)` lh 28px | `40,176 762x28` 17px/700 `rgb(0,120,200)` lh 28px |
| header rule | full-width section `div.mf-bank-widget-panel--section` with `border-top: 1px solid rgb(204,206,210)` at card y=90 | `div.border-t.border-[#ccced2]` `20,250 842x233`, `border-top: 1px solid rgb(204,206,210)`, card y=90 |
| account number | `40,244 16` tall, 13px/700 `rgba(0,10,30,.75)`, 8px below the name | `40,212 16` tall, 13px/700 `rgba(0,10,30,.75)`, 8px below the name |
| "Reconcile N items" | `40,303 143x32` `rgb(0,120,200)`, r3, `5px 12px`, 13px/700, white, mb 12px | `40,270 145x32` `rgb(0,120,200)`, r3, `5px 12px`, 13px/700, white, mb 12px |
| table | `802` wide, `border-collapse`, rows 24px | `802` wide, `border-collapse`, rows 24px |
| left cell | `615x24`, `padding 0 25px 8px 0`, left, 13px/400, value `rgb(50,70,90)` | `615x24`, `padding 0 25px 8px 0`, left, 13px/400, `rgb(50,70,90)` |
| right cell | `187x24`, `padding 0 0 8px`, right, 13px/400, value `rgb(50,70,90)`; prints `7,430.22` / `13,985.32` (base currency, no prefix) | `187x24`, `padding 0 0 8px`, right, 13px/400, `rgb(50,70,90)`; prints `7,430.22` / `12,392.72` |
| statement label | `Statement balance (11 Sept)` — one space before the bracket | `Statement balance (10 Sept)` |
| month abbreviation | "16 Aug", "30 Aug", "6 **Sept**" | "1 Jul", "25 Jul", "18 Aug", "11 **Sept**" |
| chart | plot `802x70` at y 415, axis strip `802x30` at y 485 holding an `802x20` svg, no top border | plot svg `802x70` at `40,383`, axis strip `802x30` at y 453 holding an `802x20` svg at `40,458`, no top border |
| axis label | 12px, `rgb(64,71,86)` | 12px, `rgb(64,71,86)` |
| empty-state heading | `40,650` 15px/700 `rgb(0,10,30)`, lh 24px, left-aligned at the body's 20px inset | same class string measured at `40,616` 15px/700 `rgb(0,10,30)`, lh 24px, 20px inset |
| empty-state button | `40,687 32` tall, `rgb(0,120,200)`, r3, `5px 12px`, 13px/700, white, 12-13px below the heading | `40,652 32` tall, `rgb(0,120,200)`, r3, `5px 12px`, 13px/700, white, 12px below the heading |

Rows that deliberately do not match, and why:

- **`document.body` background.** Xero's body is `rgb(242,243,244)`; goXero's body carries the
  app shell's `bg-ink-50` (`rgb(247,248,250)`) for every page. Restyling the body would change
  every screen, so the page paints the Xero surface on its own wrapper, bled to the viewport
  (`0,56 882x796` at `rgb(242,243,244)`). The visible page surface matches; `document.body` does not.
- **Button widths.** 144/139 vs 149/142 etc. — goXero renders Inter, Xero Helvetica Neue. Height,
  padding, radius, colours, font size/weight and the 12px gaps are identical; the text is a few
  px wider. Font choice is the app's, not this page's.
- **Marker spacing.** Xero plots a rolling ~30-day daily series (31 markers); goXero plots every
  statement line (101 for account 090), so its markers are ~8px apart against Xero's ~25px. The
  rule is one marker per plotted point in both; the series the point comes from differs.
- **Axis tick paths.** Xero's axis svg carries `path.mf-bank-widget-BalanceGraph--domain`
  elements that compute to `stroke: none`. Their count is not stable: 4 at first paint, then
  +1 per second as the widget re-renders (measured 4, 5, 10, 12, 13, 23, 33, 35 on the same
  card across reloads). goXero draws one domain path plus Xero's four `<g class="tick">`
  groups, each a zero-length `<line>` mark and a label — the same visible result, without the
  leak.
- **Currency formatting.** A balance the organisation holds in its **base** currency is printed
  as a bare number (`7,430.22`), matching Xero. An account held in any other currency keeps the
  app's `formatCurrency` prefix. This is a page-local `formatBalance`; the shared
  `formatCurrency` is untouched, so every other screen is unchanged. The *values* are the
  endpoint's, unmodified.
- **Empty card height** 200 vs 199 — one pixel in the "No transactions imported" body; the 90px
  header and the y=90 rule are identical.

## 4. Checks

```
$ cd web && bun run check
1789139424446 START "…/web"
1789139424446 COMPLETED 473 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS

$ cd web && bun run build
✓ built in 2.04s
```

## 5. No frames

```
$ grep -rn "<iframe" web/src internal cmd
(no matches, exit 1)
```
Live DOM on the page: `{"iframes": 0, "frames": 0}`.

## 6. Not done

- `BankBalanceChart.svelte` was left untouched; the chart is now drawn inline in the page with
  the exported `$lib/bank-balance-chart` helpers. That component is used by no other screen, so
  it is now dead code — left in place rather than deleted, since the brief scoped my changes to
  this page.
- The empty card could not be observed with goXero's demo data: both demo bank accounts have
  statement lines (091 has 3). Its geometry above was measured by putting the page's own
  empty-state class string into a real card body at the real container width. The markup it was
  measured from is the markup the page renders.
- Xero's per-account menu measured live is: Find · Account Transactions · Bank Statements ·
  New ▸ (Spend Money, Receive Money, Transfer Money) · Reconcile · Reconcile Account · Bank Rules ·
  Reconciliation Report · Import a Statement · Bank Feeds · View Status Updates · Edit account
  details. goXero keeps its own five destinations (Account transactions, Manage bank feeds,
  Import bank statement, Edit account details, Bank summary report) — no new routes were invented.
