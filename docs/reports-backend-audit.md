# Report backend audit — Xero parity

**Scope:** every report endpoint registered in `internal/router/router.go`'s `/api/v1/reports/*`
block (plus its `/api.xro/2.0/Reports/Form1120` sibling), the repository aggregates behind them,
and the renderers that shape the canonical Xero `Reports` envelope.
**Files owned and changed:** `internal/handlers/report.go`, `internal/handlers/report_render.go`,
`internal/repository/report.go`, `internal/handlers/reports_parity_integration_test.go` (new).
**Date:** 2026-09-11 · **Branch:** `feat-xero-parity-bank` (worktree `bonefish`).

Two conventions run through the whole audit:

* **An as-at report** (`date`) bounds its aggregates with `<= date`; a **period report**
  (`fromDate`/`toDate`) needs *both* bounds, because a dropped lower bound returns lifetime totals
  and a dropped upper bound returns the future. Every aggregate was checked against that rule; the
  only one that escaped it is defect 3 below.
* **`renderXeroReport` and `models.Report` are unchanged.** No wire shape moved. Reports that were
  already correct were left alone, including their quirks.

---

## 1. Endpoint table

| Endpoint | Closest Xero report | Data source (repository method + SQL) | Defects found | Fix applied | Residual gaps |
|---|---|---|---|---|---|
| `GET /reports` | `GET /Reports` (index) | `ReportHandler.ReportsList` — static catalogue, no SQL | Catalogue omitted three registered reports (`aged-*-by-contact`, `form-1120`) | Added them (`internal/handlers/report.go`) | Not the Xero envelope: it is `{"Reports":[…]}` without `Id`/`Status`/`ProviderName`, and carries `Path` (an SPA convenience) rather than Xero's `ReportID` list. Renaming fields would break the frontend, so the shape stays; documented here. |
| `GET /reports/invoice-summary` | *none* — not a Xero report | `InvoiceHandler.Summary` → `Invoices.Summary`, `aggregate` over `invoices` by status | **Out of my ownership** (`internal/handlers/invoice.go`, `internal/repository/invoice.go`) — but it is registered under `/reports/*` and does not return the Xero envelope and is not date-bounded at all | None (not my boundary) | Reported only. A client iterating `/reports/*` as Xero reports will mis-parse this one. It belongs under a non-report path or behind a `?status=` summary parameter. |
| `GET /reports/trial-balance` | Trial Balance | `Reports.TrialBalance` — one scan over `accounts LEFT JOIN (periodLines @ $3)` with `journal_date >= $2 AND <= $3` for the period columns and `>= $4 (FY start) AND <= $3` for YTD | None found. Debit == Credit in both windows; all 19 account types land in a bucket | None. Added regression coverage (period debits == credits, YTD debits == credits, window narrows the figures) | Period columns are net movement per account (`net_amount > 0 / < 0`), not gross debit/credit turnover — same as Xero's TB. Accounts with no movement in either window are hidden from the render, which matches Xero. |
| `GET /reports/profit-and-loss` | Profit and Loss | `Reports.ProfitAndLoss` — one scan over `accounts LEFT JOIN (periodLines @ max(to))`, per-column `CASE` for `[$2,$3]` (current) and `[$4,$5]` (comparative) | **Defect 1** (comparative totals aliased); doc comment advertised `periods`/`timeframe`/`paymentsOnly` that are not implemented | `zeroPtr()` per accumulator; doc comment corrected to list only what is implemented | `?periods`/`?timeframe` multi-period columns and `?paymentsOnly` are not implemented (Xero has them). Accounts with zero movement render as `0.00` rows rather than being hidden. |
| `GET /reports/profit-loss` | Profit and Loss (alias) | Same handler/source | None | None | Alias; same gaps as above. |
| `GET /reports/balance-sheet` | Balance Sheet | `Reports.BalanceSheet` — `accounts LEFT JOIN (periodLines @ max(asOf, cmp))`, `journal_date <= $2` / `<= $3` | **Defect 2** (comparative totals aliased — every comparative total printed the same number and doubled it) | `zeroPtr()` per accumulator | Comparative defaults to the financial-year start, not "same date last year" (Xero offers both); `?compareDate` overrides. Accounts with a zero balance are still listed. |
| `GET /reports/aged-receivables` | Aged Receivables | `Reports.Aged` — `invoices JOIN contacts`, `status='AUTHORISED' AND amount_due > 0 AND date <= $3`, `GROUP BY` contact | **Defect 4** (buckets read the nullable `due_date`, so an invoice with no due date sat in `Total` and in no bucket); **Defect 6** (column header `"< 30"` mislabelled the `1..30` bucket) | Buckets read `COALESCE(i.due_date, i.date)`; header renamed `1-30` | Credit notes / overpayments are not netted off the ageing (Xero includes them); `amount_due > 0` excludes credits by construction. Ageing is from `due_date`, falling back to the invoice date, not from a payment term recalculation. |
| `GET /reports/aged-payables` | Aged Payables | Same, `type='ACCPAY'` | Same as above | Same as above | Same as above. |
| `GET /reports/aged-receivables-by-contact` | Aged Receivables by Contact | Same, `type='ACCREC'` | Route was wired to the *same* handler as `/aged-receivables` and ignored `contactID`, so it was not a by-contact report at all | `Aged` takes a `contactID *uuid.UUID` (`AND ($4::uuid IS NULL OR i.contact_id = $4)`); handler reads `?contactID` (and `?ContactID`) via `firstNonEmpty` | Xero's By-Contact report is one section per contact with a per-contact subtotal; ours returns the filtered rows in the flat aged layout. Same credit-note/overpayment caveat. |
| `GET /reports/aged-payables-by-contact` | Aged Payables by Contact | Same, `type='ACCPAY'` | Same as above | Same as above | Same as above. |
| `GET /reports/bank-summary` | Bank Summary | `Reports.BankSummary` — `accounts LEFT JOIN (periodLines @ $3)`, `type = 'BANK'`; opening `journal_date < $2`, received/spent `BETWEEN $2 AND $3`, closing `<= $3` | None found. Opening + Received − Spent == Closing on every row; two-sided bounds | None. Regression coverage added | Only `type = 'BANK'` accounts are included (no CURRLIAB bank-linked accounts), and no per-account `Cash`/`Cheque` split. |
| `GET /reports/cash-summary` | Cash Summary | Same `Reports.BankSummary` call, re-labelled by `renderCashSummary` | None found (same aggregation as Bank Summary) | None | Identical numbers to Bank Summary, labelled differently. Xero's Cash Summary groups bank accounts differently and adds a comparative period column; ours does not. |
| `GET /reports/executive-summary` | Executive Summary | `Reports.ExecutiveSummary` — composes P&L + Balance Sheet + Bank Summary, plus two invoice aggregates | **Defect 3** (AR/AP KPIs aggregated *every* authorised invoice the org ever raised — no date bound, so the KPI disagreed with the aged report it drills into); **Defect 5** (column header hardcoded to `"This month"`) | Both AR and AP queries now carry `amount_due > 0 AND date <= $3::date` (as-at); header derived from the requested window via `periodColumnLabel` | `?fromDate` is now honoured for the flow KPIs (default: the month ending on `date`). Balance KPIs (AR, AP, net assets, closing bank) are as at `date` — a mixed-semantics report, which is what Xero's Executive Summary is. |
| `GET /reports/budget-summary` | Budget Summary | None — **stub** | No budget data exists anywhere in the schema | Kept as a documented stub (see §3) | Whole report: no budget table in `migrations/`. |
| `GET /reports/bas` | BAS / Sales Tax | `Reports.SalesTaxByRate` — `invoices JOIN invoice_line_items`, `status IN ('AUTHORISED','PAID') AND i.date BETWEEN $2 AND $3`, `GROUP BY tax_type` | Missing the total row Xero prints under the per-rate lines | Added a derived `Total` summary row (summed from the rate rows, nothing hardcoded) | Reads invoice line items directly instead of the GL (deliberate: it needs the ACCREC/ACCPAY split, which `gl_journal_lines` does not carry) — so an adjustment journal is invisible to it. Credit notes are not included. `ReportID` is `BASReport` and the title reads "Sales Tax Report" for both this route and its alias. |
| `GET /reports/sales-tax` | Sales Tax Report (alias) | Same handler | Same as above | Same as above | Same as above. |
| `GET /reports/journal-report` | Journal Report | `Reports.JournalFeed` → `journalFeed` — `gl_journals JOIN gl_journal_lines JOIN accounts`, `journal_date BETWEEN $2 AND $3` | None found (two-sided) | None. Regression coverage added (no dated row may fall outside the window) | One row per GL *line*, not one section per journal (Xero's Journal Report is per journal). No pagination/`offset`; a wide window returns everything. |
| `GET /reports/account-transactions` | Account Transactions | `Reports.AccountTransactions` → `journalFeed` with `?accountID` (repeatable or comma-separated) | None found (two-sided; `?accountID` honoured) | None. Regression coverage added | Accounts with no movement produce no rows (Xero lists the account header with zero movement). No opening/closing balance columns. |
| `GET /reports/general-ledger-detail` | General Ledger Detail | `Reports.GeneralLedgerDetail` — per-account `accounts LEFT JOIN (periodLines @ $3)` for debit/credit/closing, then `journalFeed` for the lines; opening = closing − period movement | None found. Opening + Debit − Credit == Closing on every account; two-sided | None. Regression coverage added | Empty accounts are skipped by the renderer; the running balance is per account in `a.code` order. |
| `GET /reports/general-ledger` | General Ledger Detail (alias) | Same handler | None | None | Alias; same gaps. |
| `GET /reports/form-1120` | *none* — Xero does not produce a 1120 (see `docs/xero-parity-1120-and-bank.md`) | `ReportRepository.Form1120` / `Form1120IncomeStatementType` in `internal/repository/form1120.go` | **Not audited — owned by a concurrent session** (`internal/repository/form1120.go`, `internal/handlers/report_form1120.go`, its integration test). Reading it: distinct accumulators (no aliasing), no hardcoded figures found | None (out of my boundary) | Whatever that session reports. Note it shares `pnlAccountTypes` with this package — do not change that variable's membership without checking the 1120 mapping. |
| `GET /api.xro/2.0/Reports/Form1120` | Same workpaper on Xero's path shape | Same handler | None | None | Alias. |

---

## 2. Defects fixed, with evidence

### Defect 1 — Profit & Loss comparative totals all aliased one variable

`internal/repository/report.go`, `ProfitAndLoss`:

```go
zero := decimal.Zero
pnl.ComparativeIncome, pnl.ComparativeGross = &zero, &zero
pnl.ComparativeNet, pnl.ComparativeCostSales, pnl.ComparativeExpenses = &zero, &zero, &zero
```

All five accumulators were pointers to **one** variable, so every line added itself to a shared
running sum and each headline row printed the others' arithmetic. The per-row comparative cells
stayed correct, which is exactly why it went unnoticed.

Observed before the fix, `?fromDate=2026-01-01&toDate=2026-03-31&compare=true` on a fixture whose
prior-year window held 200 income and 40 expenses:

| Row | Comparative shown | Correct |
|---|---|---|
| Total income | 0.00 | 200.00 |
| Gross Profit | 0.00 | 200.00 |
| Total less operating expenses | 0.00 | 40.00 |
| Net Profit | 0.00 | 160.00 |

Fixed with `zeroPtr()` — one `decimal.Decimal` per accumulator. Now: 200 / 200 / 40 / 160.

### Defect 2 — Balance Sheet comparative totals all aliased one variable

Same shape, four accumulators. Worse, the last statement re-added one of them to itself:

```go
bs.ComparativeTotalEquity = bs.ComparativeTotalEquity.Add(*bs.ComparativeRetainedEarnings)
```

Observed before the fix with `?date=<d>&compare=true`: `Total Assets`, `Total Liabilities`,
`Retained Earnings` and `Total Equity` all printed **1400.00**, and `Net Assets` printed **0.00**,
where the true comparative column was 350 / 40 / 310 / 310 / 310. (350 + 40 − (−310) = 700 landed in
the shared variable, then the equity line doubled it to 1400.)

Now every comparative total equals the sum of the comparative cells printed above it, and the
identity assets == liabilities + equity holds **in both columns**.

### Defect 3 — Executive Summary AR/AP were not date-bounded

`ExecutiveSummary` aggregated `invoices` with only `organisation_id`, `type` and `status` — no date
predicate at all, unlike every aged report. The KPI therefore reported lifetime receivables:

```
/reports/executive-summary?date=2026-03-31  → Accounts receivable 314.00
/reports/aged-receivables?date=2026-03-31   → Total              215.00   (Δ 99.00)
```

The 99.00 was an invoice dated 2026-04-15 — after the as-at date, and correctly absent from the aged
report. Fixed by adding `amount_due > 0 AND date <= $3::date` to both the AR and AP queries, so the
KPI is now the same row set (and the same number) as the aged report it drills into.

### Defect 4 — Ageing buckets dropped invoices with no due date

`invoices.due_date` is nullable, and the bucket `CASE` expressions read `$3::date - i.due_date`
while `Total` summed `i.amount_due` unconditionally. An authorised invoice with no due date was
counted in the total and in no bucket:

```
Contact row:  Current 30.00 | 1-30 20.00 | 31-60 10.00 | 61-90 0.00 | > 90 100.00 | Total 215.00
                                              buckets sum to 160.00, Total says 215.00 (Δ 55.00)
```

Fixed by bucketing on `COALESCE(i.due_date, i.date)` — age from the invoice date when no due date is
set, which is what a reader expects and what makes `Total == Σ buckets` a check the report can pass.
The fallback can never itself be NULL: a row only enters the report when `date <= asOf` is true.

### Defect 5 — Rounding used banker's rounding instead of Xero's

`money()` called `StringFixedBank(2)` (round-half-to-**even**): 2.345 rendered as `2.34`. Xero's
reports round half **away from zero** (`2.35`). Changed to `StringFixed(2)`. This is the class of
one-cent disagreement that makes a printed total differ from the sum of the rows above it.

### Defect 6 — Ageing column header mislabelled the first bucket

The header read `"< 30"` for the bucket the SQL defines as `BETWEEN 1 AND 30`, which reads as
"under 30 days outstanding" and is wrong for every row in it. Renamed to `1-30`; `Current` keeps its
Xero meaning ("not yet due").

### Smaller faithfulness fixes

* `renderExecutiveSummary`'s column header was the literal `"This month"` regardless of the window
  requested. It is now derived by `periodColumnLabel(from, to)` ("March 2026", or a date range for a
  longer window), and `?fromDate` is honoured.
* `/reports/bas` had no total row; added one derived from the rate rows.
* The `-by-contact` aged routes ignored `?contactID`; they now filter (`?contactID` and `?ContactID`).
* `ReportsList` omitted three registered reports; added.
* The `ReportHandler` doc comment advertised `?periods`, `?timeframe`, `?standardLayout` and
  `?paymentsOnly`, none of which are implemented. Replaced with an explicit "not yet implemented"
  list, so the comment no longer promises behaviour the code does not have.

---

## 3. `BudgetSummary`: deliberately still a stub

`grep` over `migrations/` finds **no budget table** — no `budgets`, `budget_lines` or equivalent, and
no budget column anywhere in the schema. There is nothing from the database to aggregate, so the
endpoint stays a documented stub that returns an empty Xero report (which is what the real API
returns for an organisation with no budget set). Deriving "budget" from actuals would silently
misreport plan against actual, which is worse than an empty report. The rationale is recorded in the
handler comment at `internal/handlers/report.go`.

---

## 4. Out-of-boundary files (reported, not edited)

* `internal/handlers/invoice.go` + `internal/repository/invoice.go` — `/reports/invoice-summary`
  (see the table). Not a Xero report, not enveloped, no date bound.
* `internal/repository/bank_statement.go`, `internal/handlers/bank_statement*.go`, `bank_*.go` —
  owned by concurrent sessions and edited while this audit ran. **No report endpoint is implemented
  in those files**, so nothing report-related was left unfixed there.
* `internal/repository/form1120.go`, `internal/handlers/report_form1120.go` — the `/reports/form-1120`
  workpaper, owned by a concurrent session. Not modified. It is the only caller of
  `pnlAccountTypes` outside this package; that variable's membership is shared, so changing it here
  would affect the 1120 mapping.
* `internal/router/router.go` — read only. No route was added, removed or renamed.
* `internal/models/report.go` — read only. The envelope and `Report`/`ReportRow`/`ReportCell` shapes
  are untouched; `renderXeroReport` is unchanged.

---

## 5. Regression tests

New file: `internal/handlers/reports_parity_integration_test.go` (extends
`reports_values_integration_test.go`, same `handlers_test` package). Fixtures are posted through the
public API (manual journals + invoices), so the reports aggregate real double-entry journals, and the
ledger is spread across three periods — prior year (comparative column), Q1 2026 (reported), Q2 2026
(must be excluded).

| Test | Proves |
|---|---|
| `TestHTTP_Reports_ComparativeColumnsAreCorrect` | Comparative P&L and Balance Sheet totals satisfy their arithmetic and are not aliased; every comparative total equals the sum of the comparative cells above it |
| `TestHTTP_Reports_ExecutiveSummaryMatchesAgedTotals` | Executive Summary AR/AP equal the aged report totals, exclude an invoice dated after the as-at date, and move when the as-at date moves |
| `TestHTTP_Reports_AgedBucketsPartitionTheTotal` | Every contact row and the grand total satisfy `Total == Σ buckets`, including an invoice with no due date; `?contactID` narrows the by-contact routes |
| `TestHTTP_Reports_DateWindowsNarrowTheNumbers` | Two disjoint windows return different sums; an account with activity only outside the window reports zero inside it; journal/account-transaction/GL rows all fall inside the requested window |
| `TestHTTP_Reports_AccountingIdentities` | TB debits == credits (period and YTD); P&L net profit == income − expenses (± comparative); BS assets == liabilities + equity and net assets == assets − liabilities; aged total == Σ buckets; bank summary opening + received − spent == closing; BAS total == Σ rate rows |
| `TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope` | All 21 report routes answer 200 in the canonical envelope (`Id`, `Status: OK`, `ProviderName: goxero`, RFC3339 `DateTimeUTC`, exactly one `Reports[]` entry with `ReportID`/`ReportName`/`ReportType`, known `RowType`s) |
| `TestHTTP_Reports_IndexIsACatalogue` | `/reports` remains a well-formed index |

Each of defects 1–4 was re-introduced by hand to confirm the new tests fail on them, then re-fixed:

| Reverted defect | Failure the test reported |
|---|---|
| 1 — P&L comparatives aliased | `2025 income` / `2025 gross profit` / `2025 net profit` wrong; "comparative income and net profit must not be the same variable" |
| 2 — Balance Sheet comparatives aliased | `comparative total assets == sum of asset rows: got 960.00, want 240.00` (every comparative total the same number) |
| 3 — Executive Summary unbounded | `executive summary AR == aged receivables total: got 254.00, want 155.00` |
| 4 — ageing buckets drop NULL due dates | `contact row …: total == sum of buckets: got 155.00, want 100.00`; "the invoice without a due date was left out of every bucket" |

All four tests were red before the fixes and green after.

---

## 6. Acceptance output

```
$ go build ./... && go vet ./...
(exit 0 — no output)
```

```
$ go test ./internal/...
ok  	github.com/shurco/goxero/internal/bankcoding	0.300s
ok  	github.com/shurco/goxero/internal/bankfeed	0.574s
ok  	github.com/shurco/goxero/internal/bankrules	0.764s
ok  	github.com/shurco/goxero/internal/bankstatement	0.369s
ok  	github.com/shurco/goxero/internal/config	0.917s
ok  	github.com/shurco/goxero/internal/database	1.182s
ok  	github.com/shurco/goxero/internal/handlers	6.623s
ok  	github.com/shurco/goxero/internal/logger	1.571s
ok  	github.com/shurco/goxero/internal/middleware	1.800s
ok  	github.com/shurco/goxero/internal/models	1.792s
ok  	github.com/shurco/goxero/internal/repository	2.817s
?   	github.com/shurco/goxero/internal/router	[no test files]
?   	github.com/shurco/goxero/internal/testutil	[no test files]
```

```
$ go test ./internal/handlers/ -run TestHTTP_Reports -count=1 -v
--- PASS: TestHTTP_Reports_XeroShape
--- PASS: TestHTTP_Reports
--- PASS: TestHTTP_Reports_ComparativeColumnsAreCorrect
--- PASS: TestHTTP_Reports_ExecutiveSummaryMatchesAgedTotals
--- PASS: TestHTTP_Reports_AgedBucketsPartitionTheTotal
--- PASS: TestHTTP_Reports_DateWindowsNarrowTheNumbers
--- PASS: TestHTTP_Reports_AccountingIdentities
--- PASS: TestHTTP_Reports_IndexIsACatalogue
--- PASS: TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope
--- PASS: TestHTTP_Reports_Values
```

Tests ran against the pgtestdb instance on **5433**, never the dev database on 5432. The dev server
on :8080 was left running and untouched.
