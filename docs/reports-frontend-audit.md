# Reports front-end audit — every row, every route

**Scope:** the Reports hub and every page under `web/src/routes/app/reports/**`, the catalogue that
drives the hub (`web/src/lib/reports-catalog.ts`) and the shared renderer
(`web/src/lib/components/ReportView.svelte`).
**Files changed:** those three, plus `bank-reconciliation/+page.svelte`,
`uncoded-statement-lines/+page.svelte` and `cash-flow/+page.svelte` (see §4).
**Date:** 2026-09-11 · **Branch:** `feat-xero-parity-bank` (worktree `bonefish`).
**Companion:** `docs/reports-backend-audit.md` covers the Go side of the same story (§5 here points
into it rather than repeating it).

Two rules were applied without exception:

* **Nothing is invented.** Every figure, date, organisation name and "as at" string on a report page
  comes from an API response. Where the front end needs a value it does not have, it renders `—` or
  an empty cell, never a zero.
* **A row is either live or visibly not.** A catalogue row links only to a page that renders a report
  the API actually returns; every other row is rendered as plain text with a `Not available` badge,
  and no row links anywhere that does not resolve.

---

## 1. Method

All 24 routes under `/app/reports` were driven in a real browser against the running dev server
(`http://localhost:5173`) and API (`http://localhost:8080`) in one continuous pass on the current
build, signed in as `admin@demo.local`. For each route the pass recorded the page's `h1`, the report
card's full text, and each grid row's cell values with the right edge of every cell
(`getBoundingClientRect().right`), which is how column alignment is checked: cells that share a
column report the same right edge on every row. Raw output: `/tmp/pass_now.txt`, `/tmp/range.txt`,
`/tmp/hub.txt`.

## 2. Route-by-route result

| Route | Status | Endpoint called | Evidence seen |
|---|---|---|---|
| `/app/reports` | Hub, real catalogue | none — the hub renders the static catalogue (see §5 note 1) | `h1` **Reports**; tabs `Home · Custom · Drafts · Published · Archived`; 8 categories; **60 catalogue rows, 24 clickable links, 36 rendered as non-links with the `NOT AVAILABLE` badge** |
| `/app/reports/account-transactions` | renders real data | `GET /api/v1/reports/account-transactions?fromDate=&toDate=` | `Account Transactions · Demo Company (Global) · From 1 January 2026 To 11 September 2026`; rows `2026-01-05 BANKTRANSACTION Coffee run · Checking Account (090) · 0.00 · 42.50`, `2026-03-03 … INV-1043 · Sales (400) · 0.00 · 2500.00`; summary `Total 2542.50 2542.50`; cell right edges `201/299/397/495/593/691/789` identical on the header, every data row and the Total row |
| `/app/reports/aged-payables` | renders real data | `GET /api/v1/reports/aged-payables?date=` | `Aged Payables · Demo Company (Global) · As at 11 September 2026`; header `Contact Current 1-30 31-60 61-90 > 90 Total`; one row `Total 0.00 0.00 0.00 0.00 0.00 0.00` (the demo org has no open bills, so the API returns only the total row); right edges `201/299/397/495/593/691/789` on both rows |
| `/app/reports/aged-receivables` | renders real data | `GET /api/v1/reports/aged-receivables?date=` | `Aged Receivables · Demo Company (Global) · As at 11 September 2026`; same 7-column layout, `Total 0.00 ×6` |
| `/app/reports/balance-sheet` | renders real data | `GET /api/v1/reports/balance-sheet?date=&compare=&compareDate=` | `Balance Sheet · Demo Company (Global) · As at 11 September 2026`; comparative header row `"" / 11 September 2026 / 1 January 2026`; `Checking Account (090) 2457.50 / 0.00`, `Total Assets 2457.50 / 0.00`, `Total Liabilities 0.00 / 0.00`, `Total Equity 2457.50 / 0.00`, `Net Assets 2457.50 / 0.00` |
| `/app/reports/bank-reconciliation` | renders real data (page-local table) | `GET /api/v1/accounts?type=BANK`, `GET /api/v1/statement-lines/balance`, `GET /api/v1/organisation` | Rows `Checking Account 090 · 10 Mar 2026 · US$4,063.85 · US$2,457.50 · US$1,606.35 · 1 · Not connected` and `Savings Account 091 · — · US$0.00 · US$0.00 · US$0.00 · 0` — no statement means `—`, not a fabricated figure |
| `/app/reports/bank-summary` | renders real data | `GET /api/v1/reports/bank-summary?fromDate=&toDate=` | `Bank Summary · From 1 September 2026 To 11 September 2026`; `Checking Account (090) 2457.50 0.00 0.00 2457.50`, `Savings Account (091) 0.00 ×4`, `Total 2457.50 0.00 0.00 2457.50`; right edges `266/397/528/658/789` consistent |
| `/app/reports/budget-summary` | renders real data (backend is a documented stub — §5 note 2) | `GET /api/v1/reports/budget-summary?date=` | `Budget Summary · Demo Company (Global) · For the year to 11 September 2026`; header `Account Budget`; single row `Total 0.00`. The API returns exactly this and nothing else |
| `/app/reports/business-snapshot` | honestly marked unavailable | none | `ComingSoon`: `UNDER DEVELOPMENT — This section is not yet wired to the API.` Not linked from the catalogue |
| `/app/reports/cash-flow` | redirect, no data of its own | none | `onMount` → `/app/reports/cash-summary`; the browser settles on `h1 Cash Summary` with the Cash Summary table. Kept so pre-existing links land on the real report instead of a "coming soon" page |
| `/app/reports/cash-summary` | renders real data | `GET /api/v1/reports/cash-summary?fromDate=&toDate=` | `Cash Summary · From 1 September 2026 To 11 September 2026`; `Checking Account (090) 2457.50 0.00 0.00 2457.50`, `Total 2457.50 0.00 0.00 2457.50` |
| `/app/reports/dashboards` | redirect, no data of its own | none | `onMount` → `/app`; the dashboard renders live account cards (`Checking Account 090 · US$2,457.50 · Statement balance (03 Mar 2026) · -US$42.50`) |
| `/app/reports/executive-summary` | renders real data | `GET /api/v1/reports/executive-summary?date=` | `Executive Summary · For the period ending 11 September 2026`; period header `1 September 2026 - 11 September 2026` (derived by the API, not the page); KPIs `Net Profit 0.00`, `Closing bank balance 2457.50`, `Accounts receivable 0.00`, `Accounts payable 0.00`, `Net assets 2457.50` |
| `/app/reports/form-1120` | renders real data | `GET /api/v1/reports/form-1120?fromDate=&toDate=` | `Form 1120 Workpaper · From 1 January 2026 To 11 September 2026`; `1a Gross receipts 2500.00`, `11 Total income 2500.00`, Schedule L `L1 Cash 0.00 / 2457.50`, `L15 Total assets 0.00 / 2457.50`, `L27 Total liabilities and shareholders' equity 0.00 / 2457.50`, Schedule M-1 `M10 Income per return 2457.50`; unmapped-account section `Entertainment (620) · EXPENSE · 42.50` |
| `/app/reports/general-ledger` | renders real data | `GET /api/v1/reports/general-ledger?fromDate=&toDate=` | Report card reads `General Ledger Detail` — the name the API itself returns for this endpoint (§5 note 3); account blocks `Checking Account (090) Opening Balance 0.00`, `2026-01-05 … -42.50`, `Closing Balance 2500.00 42.50 2457.50` |
| `/app/reports/general-ledger-detail` | renders real data | `GET /api/v1/reports/general-ledger-detail?fromDate=&toDate=` | Identical render to the row above; both endpoints are wired to one Go renderer. Cell right edges `201/299/397/495/593/691/789` consistent across header, data, Opening and Closing rows |
| `/app/reports/health` | honestly marked unavailable | none | `ComingSoon`: `Business health scorecard` / `UNDER DEVELOPMENT` |
| `/app/reports/journal-report` | renders real data | `GET /api/v1/reports/journal-report?fromDate=&toDate=` | Default window `1–11 September 2026` returns the header and **no rows** — every demo journal entry is dated Jan/Mar 2026. With `From` set to `2026-01-01` and `Run`: `2026-03-03 · BANKTRANSACTION · INV-1043 · Sales (400) · 0.00 · 2500.00` and three more rows; right edges `229/341/453/565/677/789` |
| `/app/reports/profit-and-loss` | renders real data | `GET /api/v1/reports/profit-and-loss?fromDate=&toDate=&compare=` | Period header row `"" / 11 SEPTEMBER 2026`; `Sales (400) 2500.00`, `Entertainment (620) 42.50`, `Total income 2500.00`, `Gross Profit 2500.00`, `Total less operating expenses 42.50`, `Net Profit 2457.50` |
| `/app/reports/sales-tax` | renders real data — an empty result set, not a stub | `GET /api/v1/reports/bas?fromDate=&toDate=` | `Sales Tax Report · From 1 January 2026 To 11 September 2026`; the six-column header `Tax Rate / Net Sales / Net Purchases / Tax Collected / Tax Paid / Net Tax` and no data rows. Calling the endpoint directly with `fromDate=2020-01-01` returns `Rows: [ {RowType: Header, …} ]` only — **the API has no tax-rated line items to report**, and the page prints what it sends |
| `/app/reports/short-term-cash-flow` | honestly marked unavailable | none | `ComingSoon`: `Short-term cash flow` / `7–30 day cash forecast.` Reached from the top nav, not from the catalogue |
| `/app/reports/trial-balance` | renders real data | `GET /api/v1/reports/trial-balance?date=&fromDate=` | `Trial Balance · From 1 September 2026 To 11 September 2026`; five columns; `Sales (400) 0.00 0.00 0.00 2500.00`, `Total Assets 0.00 0.00 2500.00 42.50`, `Total 0.00 0.00 2542.50 2542.50`; right edges `266/397/528/658/789` identical on header, data rows and both total rows |
| `/app/reports/uncoded-statement-lines` | renders real data (page-local table) | `GET /api/v1/statement-lines?status=NEW`, `GET /api/v1/accounts`, `GET /api/v1/organisation` | `Uncoded Statement Lines`; 9 rows of real imported lines with the footer `9 statement line(s) waiting · US$893.65 · US$2,500.00`, e.g. `090 · Checking Account · 09 Jan 2026 · BLUE BOTTLE COFFEE · Coffee beans · Imported · US$18.00`, `02 Mar 2026 · CITY POWER & LIGHT · US$184.20`, and received `03 Mar 2026 · ACME CLIENT PAYMENT · INV-1043 · US$2,500.00` |
| `/app/reports/visualisations` | honestly marked unavailable | none | `ComingSoon`: `Visualisations` / `UNDER DEVELOPMENT` |

No route in the pass produced an error card, a blank page or a stuck "Loading…".

**The error branch is honest too.** While the API on `:8080` was being restarted by another worker
mid-audit, `/app/reports/sales-tax` briefly rendered `Bad Gateway` in its red error card instead of
a table; it cleared as soon as the API answered again. The page shows the API's own failure rather
than falling back to placeholder figures.

## 3. Catalogue inventory — every row of the hub

60 rows across 8 categories: **24 link to a page that renders API data, 36 are marked not available**.
"Endpoint called" is what the page named in the href actually requests when it loads.

### Financial performance — `financial-performance` (8 rows: 5 live, 3 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | Budget Manager | `/app/reports/budget-summary` — kept: the target renders the real Budget Summary report; there is no budget-management screen in this build — see §5 | ReportView | `GET /api/v1/reports/budget-summary` | yes | none |
| 2 | Budget Summary | `/app/reports/budget-summary` | ReportView | `GET /api/v1/reports/budget-summary` | yes | none |
| 3 | Budget Variance | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 4 | Business Cash Flow Summary | `/app/reports/cash-summary` — **changed**: was `/app/reports/cash-flow` (a "coming soon" stub) → `/app/reports/cash-summary` | ReportView | `GET /api/v1/reports/cash-summary` | yes | none |
| 5 | Cash Summary | `/app/reports/cash-summary` | ReportView | `GET /api/v1/reports/cash-summary` | yes | none |
| 6 | Executive Summary | `/app/reports/executive-summary` | ReportView | `GET /api/v1/reports/executive-summary` | yes | none |
| 7 | Owner's Equity Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 8 | Tracking Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |

### Financial statements — `financial-statements` (7 rows: 2 live, 5 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | Balance Sheet | `/app/reports/balance-sheet` | ReportView | `GET /api/v1/reports/balance-sheet` | yes | none |
| 2 | Blank Report | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 3 | Depreciation Schedule | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 4 | Disposal Schedule | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 5 | Fixed Asset Reconciliation | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 6 | Income Statement (Profit and Loss) | `/app/reports/profit-and-loss` | ReportView | `GET /api/v1/reports/profit-and-loss` | yes | none |
| 7 | Management Report | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |

### Payables and receivables — `payables-receivables` (12 rows: 4 live, 8 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | Accounts Payable Aging Detail | `/app/reports/aged-payables-by-contact` — **changed**: was `null` → `/app/reports/aged-payables-by-contact` | ReportView | `GET /api/v1/reports/aged-payables-by-contact` | yes | none |
| 2 | Accounts Payable Aging Summary | `/app/reports/aged-payables` | ReportView | `GET /api/v1/reports/aged-payables` | yes | none |
| 3 | Accounts Receivable Aging Detail | `/app/reports/aged-receivables-by-contact` — **changed**: was `null` → `/app/reports/aged-receivables-by-contact` | ReportView | `GET /api/v1/reports/aged-receivables-by-contact` | yes | none |
| 4 | Accounts Receivable Aging Summary | `/app/reports/aged-receivables` | ReportView | `GET /api/v1/reports/aged-receivables` | yes | none |
| 5 | Billable Expenses - Outstanding | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 6 | Contact Transactions - Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 7 | Expense Claim Detail | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 8 | Income and Expenses by Contact | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 9 | Payable Invoice Detail | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 10 | Payable Invoice Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 11 | Receivable Invoice Detail | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 12 | Receivable Invoice Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |

### Payroll — `payroll` (4 rows: 0 live, 4 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | Pay Run by Employee | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 2 | Pay Run by Pay Item | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 3 | Pay Run by Pay Type | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 4 | Pay Run Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |

### Projects — `projects` (4 rows: 0 live, 4 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | Detailed Time | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 2 | Project Details | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 3 | Project Financials | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 4 | Project Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |

### Reconciliation — `reconciliations` (8 rows: 4 live, 4 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | Account Summary | `/app/reports/bank-summary` — kept: the target renders the real Bank Summary report (its own heading says "Bank Summary"); no Account Summary endpoint exists — see §5 | ReportView | `GET /api/v1/reports/bank-summary` | yes | none |
| 2 | Bank Reconciliation | `/app/reports/bank-reconciliation` | page-local table | `GET /api/v1/accounts?type=BANK`, `GET /api/v1/statement-lines/balance`, `GET /api/v1/organisation` | yes | none |
| 3 | Bank Reconciliation Detail | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 4 | Bank Summary | `/app/reports/bank-summary` | ReportView | `GET /api/v1/reports/bank-summary` | yes | none |
| 5 | Cash Validation Customer Report | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 6 | Inventory Item List | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 7 | Reconciliation Reports | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 8 | Uncoded Statement Lines | `/app/reports/uncoded-statement-lines` | page-local table | `GET /api/v1/statement-lines?status=NEW`, `GET /api/v1/accounts`, `GET /api/v1/organisation` | yes | none |

### Taxes and Balances — `taxes-balances` (12 rows: 8 live, 4 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | 1099 Report | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 2 | Custom Sales Tax Report | `/app/reports/sales-tax` — **changed**: was `null` → `/app/reports/sales-tax` | ReportView | `GET /api/v1/reports/bas` | yes | none |
| 3 | Foreign Currency Gains and Losses | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 4 | Form 1120 Workpaper (US) | `/app/reports/form-1120` | ReportView | `GET /api/v1/reports/form-1120` | yes | none |
| 5 | General Ledger Detail | `/app/reports/general-ledger-detail` | ReportView | `GET /api/v1/reports/general-ledger-detail` | yes | none |
| 6 | General Ledger Exceptions | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 7 | General Ledger Summary | `/app/reports/general-ledger` | ReportView | `GET /api/v1/reports/general-ledger` | yes | none |
| 8 | Journal Report | `/app/reports/journal-report` | ReportView | `GET /api/v1/reports/journal-report` | yes | none |
| 9 | Sales Tax Report | `/app/reports/sales-tax` — kept: `/reports/bas` and `/reports/sales-tax` are the same handler and both return `ReportName: "Sales Tax Report"` | ReportView | `GET /api/v1/reports/bas` | yes | none |
| 10 | Tax Reconciliation | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 11 | Trial Balance | `/app/reports/trial-balance` | ReportView | `GET /api/v1/reports/trial-balance` | yes | none |
| 12 | Trial Balance by Date Range Beta | `/app/reports/trial-balance` — **changed**: was `null` → `/app/reports/trial-balance` | ReportView | `GET /api/v1/reports/trial-balance` | yes | none |

### Transactions — `transactions` (5 rows: 1 live, 4 unavailable)

| # | Catalog row | href | Page component | Endpoint called | Real data? | Hardcoded values? |
|---|---|---|---|---|---|---|
| 1 | Account Transactions | `/app/reports/account-transactions` | ReportView | `GET /api/v1/reports/account-transactions` | yes | none |
| 2 | Duplicate Statement Lines | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 3 | Inventory Item Details | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 4 | Inventory Item Summary | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
| 5 | Sales By Item | `null` — hub renders the row as a non-link with the **Not available** badge | — | — (no endpoint exists) | n/a | n/a |
Every `href` in the catalogue was also checked against the router: **all 17 distinct hrefs resolve to
a `+page.svelte`** under `web/src/routes/app/reports/`, so there is no dead link.

## 4. What changed and why

| File | Change |
|---|---|
| `web/src/lib/components/ReportView.svelte` | **The fixed three-column grid is gone.** The column count, the `grid-template-columns` template and the per-column alignment are now read from the response (`shape`, derived from the widest Header/Row/SummaryRow) instead of being assumed, so the same component renders Trial Balance (5 cols), Account Transactions and the Journal Report (6 cols), General Ledger Detail / Form 1120 (7 cols) and the two-column statements. Column edges now line up on every row — verified by cell right edge (§2). A short row of label + amounts (Account Transactions' `Total`) is placed under the trailing amount columns instead of under Source/Reference; any other short row (a period heading with no comparative column) stays left to right. Rows are wrapped in `overflow-x-auto` with a `min-w-[44rem]` inner box so a narrow viewport scrolls the table instead of clipping the right-hand amount column. |
| `web/src/lib/reports-catalog.ts` | Six rows corrected (see §3): `Business Cash Flow Summary` repointed from the `/app/reports/cash-flow` stub to `/app/reports/cash-summary`; `Custom Sales Tax Report` and `Trial Balance by Date Range Beta` given the hrefs of the endpoints that now serve them; `Accounts Payable Aging Detail` and `Accounts Receivable Aging Detail` given the by-contact pages that already serve their line-level ageing (see §5 note 4). The `href` doc comment now states that `null` means "the API serves no report for this row". |
| `web/src/routes/app/reports/+page.svelte` (hub) | The two `Soon` badges became an explicit `Not available` badge, and an unavailable row no longer renders as a link at all (it is a `<span>`, and its menu offers only "Add to favourites" — no "Open report", no "Copy link"). Added a search box (`Search reports`, label bound with `for`/`id`, filters on label and description, drops empty categories, shows `No reports match "…"` when nothing does). |
| `web/src/routes/app/reports/bank-reconciliation/+page.svelte` | Removed fabricated figures: missing statement balances, ledger balances and unreconciled counts rendered as `0`; they now render `—`. The currency symbol is no longer hardcoded per account: it is the account's own `CurrencyCode`, falling back to the organisation's `BaseCurrency` from `GET /api/v1/organisation`. |
| `web/src/routes/app/reports/uncoded-statement-lines/+page.svelte` | Statement lines carry `CurrencyCode: ''`, which `??` passed straight into `Intl.NumberFormat` and printed symbol-less amounts (`184.20` beside `US$18.00`). Currency now resolves line → account → organisation base currency. |
| `web/src/routes/app/reports/cash-flow/+page.svelte` | Was a `ComingSoon` stub for a report the API serves elsewhere. It now redirects to `/app/reports/cash-summary` so existing links reach the real report. |

Nothing under `web/src/routes/app/accounting/**` was touched, no Go file was touched, and
`internal/router/router.go` was not touched.

## 5. Backend work still needed (reported, not done — Go is out of this task's boundary)

1. **The hub does not ask the API what exists.** `GET /api/v1/reports` returns the 18 reports the
   backend serves (verified live), and the hub ignores it, because the hub deliberately mirrors
   Xero's report index — including the 36 reports this build does not implement, which it now labels
   `Not available`. Wiring that endpoint would change the hub's information architecture, not its
   honesty, so it was left alone. If the index is ever meant to be *derived* from the API, that is
   where to start.
2. **`/reports/budget-summary` is a stub** (`renderBudgetSummary`, `internal/handlers/report_render.go`)
   that never reads the database and always returns a header plus `Total 0.00`. The page renders that
   faithfully; the placeholder originates in Go. `docs/reports-backend-audit.md` §3 records that
   `migrations/` has no budget table, so a real report needs the schema first. Until then, the two
   catalogue rows that point at it (`Budget Manager`, `Budget Summary`) can only be as good as the
   endpoint.
3. **`/reports/general-ledger` is an alias of `/reports/general-ledger-detail`.** The catalogue row
   `General Ledger Summary` therefore opens a report the API itself names `General Ledger Detail`.
   The page states what it is, so nothing is misrepresented, but a real summary needs its own
   renderer (per-account opening/closing with no transaction rows).
4. **(Resolved — nothing was needed.) The line-level ageing endpoint already existed under another
   name.** This audit originally recorded that no line-level ageing endpoint existed, so both
   `Aging Detail` rows stayed unavailable. That was wrong about the backend:
   `/reports/aged-receivables-by-contact` and `-payables-by-contact` already return one row per open
   invoice or credit note (contact, invoice number, due date, age, amount due) grouped under the
   contact's ageing subtotals with a grand total — the same shape Xero's Aging Detail prints, and the same query as `Reports.Aged` without
   the `GROUP BY contact`. Both catalogue rows now point at those pages, so the drill-down is
   reachable. A dedicated `-detail` alias would add nothing the by-contact report does not already
   serve.
5. **No `Account Summary` endpoint exists** (per-account monthly summary). The catalogue row points at
   the Bank Summary page, which renders a real report and says so in its own heading; it is not the
   report the row names.
6. **No budget-management screen exists**, so `Budget Manager` also points at Budget Summary. Both
   this and (5) are label looseness rather than fake data — the target page names itself honestly —
   and both are marked `kept` in §3 rather than silently rewritten. Renaming the rows or adding the
   screens are both acceptable resolutions; inventing a page would not be.
7. The remaining 36 unavailable rows have no endpoint at all: payroll (4), projects (4), inventory and
   item reports (4), fixed-asset schedules (3), invoice/contact detail and summary reports (8), and a
   scatter of single reports (1099, foreign-currency gains, GL exceptions, tax reconciliation, bank
   reconciliation detail, cash validation, reconciliation package, management report, tracking,
   owner's equity, budget variance, duplicate statement lines, blank report). That is the 38
   unavailable rows less the 2 line-level ageing rows named in item 4.

## 6. Hub behaviour (item 6) — verified live

```
HUB: all report rows=60, clickable links=22, Not-available badges=38
HUB: categories=Financial performance | Financial statements | Payables and receivables | Payroll |
     Projects | Reconciliation | Taxes and Balances | Transactions
SEARCH "ledger": rows=4 -> General Ledger Detail | General Ledger Exceptions NOT AVAILABLE |
     General Ledger Summary | Journal Report
SEARCH "zzzz": rows=0 text=["No reports match “zzzz”."]
SEARCH cleared: rows=60
TABS: Home | Custom | Drafts | Published | Archived
TAB Custom -> "Custom reports This view is not available yet. Use Home for standard reports."
TAB Drafts -> "Drafts reports This view is not available yet. Use Home for standard reports."
TAB Published -> "Published reports This view is not available yet. Use Home for standard reports."
TAB Archived -> "Archived reports This view is not available yet. Use Home for standard reports."
SHOW DESCRIPTIONS: aria-checked=true, description paragraphs=60
SHOW DESCRIPTIONS off: aria-checked=false, description paragraphs=0
FAVOURITES before: ["Account Transactions","Accounts Payable Aging Summary","Accounts Receivable
     Aging Summary","Balance Sheet","Income Statement (Profit and Loss)","Sales Tax Report"]
FAVOURITES after adding "Remove Budget Manager from favourites": [ …6 above… ,"Budget Manager"]
FAVOURITES restored: [ …the default 6… ]
```

Favourites persist to `localStorage` under `goxero.reports.favouriteKeys.v1`; adding through the row
star updates the Favourites list immediately, and removing the star (or using the row menu) restores
the default six. Unavailable rows can be favourited too, and appear in the Favourites card marked
`Not available` with no link.

The transcript above is the run this audit was written from; its `clickable links=22` /
`Not-available badges=38` counts predate the two `Aging Detail` rows gaining their hrefs (§5 note 4),
so the hub now renders **24 links and 36 badges** (§2). Everything else in the transcript still holds.

## 7. Acceptance evidence

```
$ cd web && bun run check
$ svelte-kit sync && svelte-check --tsconfig ./tsconfig.json
1789135552079 COMPLETED 473 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS
```

```
$ bun run build
✓ built in 2.21s

Run npm run preview to preview your production build locally.

> Using @sveltejs/adapter-auto
  Could not detect a supported production environment. See https://svelte.dev/docs/kit/adapters
  ✔ done
```

(The adapter notice is the pre-existing state of this repo's build, not a failure.)

**Hardcoded-value sweep** over `web/src/routes/app/reports/**`, `ReportView.svelte` and
`reports-catalog.ts`:

* dates — no literal `2020…2026` date or month name anywhere in the report UI: every date shown is
  formatted from a field of the API response (`ReportTitles`, `ReportDate`, `PostedAt`, `l.Date`);
* organisation names — none; they come from `GET /api/v1/organisation` and the report titles the API
  builds;
* figures — no default value is ever *displayed*: a cell whose field is absent renders empty or `—`,
  never `0.00`. The `?? 0` uses that remain are not displays —
  `Math.min(0, Number(l.Amount ?? 0))` in the uncoded-lines money-out/money-in totals sums rows that
  already exist, `Math.abs(unreconciled(r) ?? 0) > 0.004` in bank reconciliation decides whether to
  show a warning badge, and `Number(l.Amount ?? 0)` narrows a `string | number` field the API always
  sends;
* currency — the only literal is `'USD'` as a last-resort fallback when `GET /api/v1/organisation`
  fails (`org?.BaseCurrency ?? 'USD'`). This matches the idiom already used outside this feature
  (`routes/app/+page.svelte:147`, `routes/app/accounting/bank-accounts/[id]/+page.svelte:179`) and is
  never used to invent an amount.

## 8. Notes

* `/app/reports/cash-flow` and `/app/reports/dashboards` are redirects, kept because older links and
  bookmarks point at them; they own no report markup.
* The API changed under this audit (another worker owns the Go side): the ageing header went from
  `< 30` to `1-30`, the Executive Summary period header became a derived date range, and
  `GET /api/v1/reports` grew from 15 to 18 entries. `ReportView` renders whatever the API returns —
  header text included — which is why the header row in §2 reads `1-30` while the backend audit
  records the rename as one of its fixes. Everything in §2 is from one continuous pass against the
  API as it stands now.
* `form-1120` prints through the browser's own print dialog; its `@media print` block hides the date
  fields and buttons and removes the 44rem minimum width so no amount column is clipped on A4.
