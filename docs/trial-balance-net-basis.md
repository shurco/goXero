# Trial Balance: the net basis, and the query strings three endpoints ignored

Two defects were fixed in this worktree on 2026-09-11, both in the report layer.
Every figure below was read from the running API against the demo organisation
`6823b27b-c48f-4099-bb27-4202a4f496a2` (Demo Company (Global), Xero short code
`!!6Sp3`), from the tree as built by `/tmp/gx-restart-server.sh` at the timestamps
given. `docs/xero-reference/**` is a frozen capture and was read, never edited.

---

## Defect 1 — the Trial Balance printed gross movement where Xero prints one net figure per account

### What Xero does

Xero's Trial Balance shows **one net figure per account**, on the side it falls.
The capture proves it three times over:

| account | the ledger (GL detail) | Xero's Trial Balance |
|---|---|---|
| `200` Sales | Dr 1,019.95 / Cr 30,559.13 | Credit **29,539.18** = 30,559.13 − 1,019.95 |
| `425` Freight & Courier | Dr 115.50 / Cr 10.00 | Debit **105.50** |
| `453` Office Expenses | Dr 1,135.98 / Cr 273.50 | Debit **862.48** |

### What we did

`internal/repository/report.go`, the `TrialBalance` query, summed four independent
gross columns:

```sql
SUM(CASE WHEN … AND l.net_amount > 0 THEN  l.net_amount END) AS debit
SUM(CASE WHEN … AND l.net_amount < 0 THEN -l.net_amount END) AS credit
… the same two again for the year-to-date window
```

Every posting is a debit or a credit, so a double-entry ledger gives **equal gross
totals either way** — which is exactly what made the defect invisible: the report
balanced, and printed 108,392.54 where Xero prints 42,595.46. An account that moved
both ways read as the *sum* of its movement (Sales read 31,579.08).

The query now sums each window **signed** — one net figure per account — and
`SplitSigned` (`internal/repository/report.go`) splits it into the Debit/Credit
pair: the net on the side it falls, `0.00` on the other. The balance-sheet measure
(`ClosingBalance`) is split by the same helper, so "net on the side it falls" is
written once for the whole report.

Nothing about `YTD Debit`/`YTD Credit` as a *measure* changed: a profit-and-loss
account still carries its year-to-date movement, a balance-sheet account still
carries its balance as at the report date. Only the basis changed, from two gross
columns to one net figure.

### Before and after

Rendered Total row of `GET /api/v1/reports/trial-balance?fromDate=2026-01-01&date=2026-12-31`:

| read at | Account | Debit | Credit | YTD Debit | YTD Credit |
|---|---|---|---|---|---|
| 2026-09-11T17:22:39Z (before) | Total | 108392.54 | 108392.54 | 44148.91 | 44148.91 |
| 2026-09-11T17:24:55Z (after) | Total | 42845.46 | 42845.46 | **42845.46** | **42845.46** |

The acceptance figure is 42,845.46 / 42,845.46: Xero's printed 42,595.46 plus the
250.00 contamination described at the end of this note.

### The rendered report, row by row (2026-09-11T17:27:59Z)

```
TITLE: Trial Balance
TITLE: Demo Company (Global)
TITLE: From 1 January 2026 To 31 December 2026
TITLE: Debit/Credit: net movement in the period, on the side it falls. YTD Debit/YTD Credit: netted the same way, from the year-to-date movement for profit and loss accounts and from the balance carried as at the report date for balance sheet accounts.
HDR Account | Debit | Credit | YTD Debit | YTD Credit
SEC Revenue
    Sales (200) | 0.00 | 29539.18 | 0.00 | 29539.18
    Total Revenue | 0.00 | 29539.18 | 0.00 | 29539.18
SEC Less Cost of Sales
    Purchases (300) | 775.98 | 0.00 | 775.98 | 0.00
    Total Less Cost of Sales | 775.98 | 0.00 | 775.98 | 0.00
SEC Less Operating Expenses
    Advertising (400) | 9907.05 | 0.00 | 9907.05 | 0.00
    Bank Fees (404) | 30.00 | 0.00 | 30.00 | 0.00
    Cleaning (408) | 1110.00 | 0.00 | 1110.00 | 0.00
    Consulting & Accounting (412) | 87.00 | 0.00 | 87.00 | 0.00
    Entertainment (420) | 1553.60 | 0.00 | 1553.60 | 0.00
    Freight & Courier (425) | 105.50 | 0.00 | 105.50 | 0.00
    General Expenses (429) | 166.28 | 0.00 | 166.28 | 0.00
    Light, Power, Heating (445) | 335.82 | 0.00 | 335.82 | 0.00
    Motor Vehicle Expenses (449) | 654.36 | 0.00 | 654.36 | 0.00
    Office Expenses (453) | 862.48 | 0.00 | 862.48 | 0.00
    Printing & Stationery (461) | 94.41 | 0.00 | 94.41 | 0.00
    Rent (469) | 3273.66 | 0.00 | 3273.66 | 0.00
    Repairs and Maintenance (473) | 1896.70 | 0.00 | 1896.70 | 0.00
    Telephone & Internet (489) | 236.37 | 0.00 | 236.37 | 0.00
    Travel - National (493) | 433.24 | 0.00 | 433.24 | 0.00
    Total Less Operating Expenses | 20746.47 | 0.00 | 20746.47 | 0.00
SEC Assets
    Business Bank Account (090) | 7430.22 | 0.00 | 7430.22 | 0.00
    Business Savings Account (091) | 0.00 | 250.00 | 0.00 | 250.00
    Accounts Receivable (610) | 9194.51 | 0.00 | 9194.51 | 0.00
    Office Equipment (710) | 923.79 | 0.00 | 923.79 | 0.00
    Computer Equipment (720) | 3774.49 | 0.00 | 3774.49 | 0.00
    Total Assets | 21323.01 | 250.00 | 21323.01 | 250.00
SEC Liabilities
    Accounts Payable (800) | 0.00 | 8386.76 | 0.00 | 8386.76
    Unpaid Expense Claims (801) | 0.00 | 115.95 | 0.00 | 115.95
    Sales Tax (820) | 0.00 | 422.59 | 0.00 | 422.59
    Historical Adjustment (840) | 0.00 | 4130.98 | 0.00 | 4130.98
    Total Liabilities | 0.00 | 13056.28 | 0.00 | 13056.28
ROW Total | 42845.46 | 42845.46 | 42845.46 | 42845.46
```

This window is the whole financial year, so the period pair and the YTD pair
coincide; with a narrower window they net independently (see
`TestHTTP_Reports_TrialBalanceNetsEachAccountBeforeTotalling`).

### Every account in the capture, against Xero (2026-09-11T17:27:59Z)

| # | Account | Xero YTD Dr | Xero YTD Cr | ours YTD Dr | ours YTD Cr | verdict | difference |
|---|---|---|---|---|---|---|---|
| 1 | `200` Sales | — | 29,539.18 | 0.00 | 29539.18 | match | — |
| 2 | `300` Purchases | 775.98 | — | 775.98 | 0.00 | match | — |
| 3 | `400` Advertising | 9,657.05 | — | 9907.05 | 0.00 | **differs** | +250.00 against Xero |
| 4 | `404` Bank Fees | 30.00 | — | 30.00 | 0.00 | match | — |
| 5 | `408` Cleaning | 1,110.00 | — | 1110.00 | 0.00 | match | — |
| 6 | `412` Consulting & Accounting | 87.00 | — | 87.00 | 0.00 | match | — |
| 7 | `420` Entertainment | 1,553.60 | — | 1553.60 | 0.00 | match | — |
| 8 | `425` Freight & Courier | 105.50 | — | 105.50 | 0.00 | match | — |
| 9 | `429` General Expenses | 166.28 | — | 166.28 | 0.00 | match | — |
| 10 | `445` Light, Power, Heating | 335.82 | — | 335.82 | 0.00 | match | — |
| 11 | `449` Motor Vehicle Expenses | 654.36 | — | 654.36 | 0.00 | match | — |
| 12 | `453` Office Expenses | 862.48 | — | 862.48 | 0.00 | match | — |
| 13 | `461` Printing & Stationery | 94.41 | — | 94.41 | 0.00 | match | — |
| 14 | `469` Rent | 3,273.66 | — | 3273.66 | 0.00 | match | — |
| 15 | `473` Repairs and Maintenance | 1,896.70 | — | 1896.70 | 0.00 | match | — |
| 16 | `489` Telephone & Internet | 236.37 | — | 236.37 | 0.00 | match | — |
| 17 | `493` Travel - National | 433.24 | — | 433.24 | 0.00 | match | — |
| 18 | `090` Business Bank Account | 7,430.22 | — | 7430.22 | 0.00 | match | — |
| 19 | `610` Accounts Receivable | 9,194.51 | — | 9194.51 | 0.00 | match | — |
| 20 | `710` Office Equipment | 923.79 | — | 923.79 | 0.00 | match | — |
| 21 | `720` Computer Equipment | 3,774.49 | — | 3774.49 | 0.00 | match | — |
| 22 | `800` Accounts Payable | — | 8,386.76 | 0.00 | 8386.76 | match | — |
| 23 | `801` Unpaid Expense Claims | — | 115.95 | 0.00 | 115.95 | match | — |
| 24 | `820` Sales Tax | — | 422.59 | 0.00 | 422.59 | match | — |
| 25 | `840` Historical Adjustment | — | 4,130.98 | 0.00 | 4130.98 | match | — |
| — | `091` — extra account, not in the capture | — | — | 0.00 | 250.00 | **extra row** | the 250.00 contamination's other leg |

25 capture accounts: 24 match Xero to the cent in the YTD pair. The two
non-matches are both the contamination and neither is a defect of this change:

* `400` Advertising reads 9,907.05 against Xero's 9,657.05 — the +250.00 spend.
* `091` Business Savings Account is an **extra** row, at 0.00 / 250.00 — that
  account does not exist in Xero's chart at all, and its 250.00 credit is the
  other leg of the same journal.

---

## Defect 2 — three endpoints ignored part of their query string

`internal/handlers/report.go`:

| endpoint | read | ignored | now |
|---|---|---|---|
| `GET /reports/trial-balance` | `date`, `fromDate` | `toDate` | `date` → `toDate` → today |
| `GET /reports/executive-summary` | `date`, `fromDate` | `toDate` | `date` → `toDate` → today |
| `GET /reports/budget-summary` | `date` | `fromDate`, `toDate` | `date` → `toDate` → today, plus `fromDate` in the title |

The as-at date now comes from `asAtParam`: **`date` wins when both it and `toDate`
are present** (Xero's own `date` is authoritative), otherwise `toDate` — the end of
the period the caller asked for — otherwise today. `fromDate` keeps its existing
meaning on the two reports that have a period, and on the Budget Summary — which
has no rows to date — an explicit window is named in the report's own title.

### The four calls, and one proving the default is untouched

```
$ GET /api/v1/reports/executive-summary?toDate=2026-12-31
  ReportDate: 31 December 2026
  Titles: Executive Summary || Demo Company (Global) || For the period ending 31 December 2026

$ GET /api/v1/reports/executive-summary?fromDate=2026-01-01&toDate=2026-12-31
  ReportDate: 31 December 2026
  Titles: Executive Summary || Demo Company (Global) || For the period ending 31 December 2026
  column heading: 1 January 2026 - 31 December 2026

$ GET /api/v1/reports/budget-summary?toDate=2026-12-31
  ReportDate: 31 December 2026
  Titles: Budget Summary || Demo Company (Global) || For the year to 31 December 2026 || No budget is stored: …

$ GET /api/v1/reports/budget-summary?fromDate=2026-01-01&toDate=2026-12-31
  ReportDate: 31 December 2026
  Titles: Budget Summary || Demo Company (Global) || From 1 January 2026 To 31 December 2026 || No budget is stored: …

$ GET /api/v1/reports/trial-balance?toDate=2026-12-31
  ReportDate: 31 December 2026
  Titles: Trial Balance || Demo Company (Global) || From 1 December 2026 To 31 December 2026 || Debit/Credit: net movement in the period, …

$ GET /api/v1/reports/executive-summary          # no parameters — unchanged
  ReportDate: 11 September 2026
  Titles: Executive Summary || Demo Company (Global) || For the period ending 11 September 2026

$ GET /api/v1/reports/budget-summary              # no parameters — unchanged
  ReportDate: 11 September 2026
  Titles: Budget Summary || Demo Company (Global) || For the year to 11 September 2026

$ GET /api/v1/reports/trial-balance               # no parameters — unchanged
  ReportDate: 11 September 2026
  Titles: Trial Balance || Demo Company (Global) || From 1 September 2026 To 11 September 2026
```

The two titles quoted in the diagnosis now read the requested period:
"For the period ending 11 September 2026" is gone, and the Budget Summary for
FY 2026 now reads "For the year to 31 December 2026". Where `date` and `toDate`
disagree, `date` wins:

```
$ GET /api/v1/reports/trial-balance?date=2026-03-31&toDate=2025-12-31
  ReportDate: 31 March 2026
  Titles: Trial Balance || Demo Company (Global) || From 1 March 2026 To 31 March 2026 || …
```

---

## Decisions on the two things the task asked me to rule on

**`trialBalanceMeasure` was kept, not simplified away.** Its special case is not
redundant: netting changes the *basis* of the pair, not *which measure* the pair
carries. A profit-and-loss account is still reported at its year-to-date movement
and a balance-sheet account at its balance as at the report date, and that is the
line Xero's own table draws. Removing it would drop `840` Historical Adjustment
(journals entirely in the prior year, so year-to-date movement 0.00) from the
report. Its balance-sheet branch now delegates to `repository.SplitSigned` instead
of repeating the split, so "net on the side it falls" is written once.

**`trialBalanceTitleNote` was changed.** A reader who reconciles against the
ledger needs to know the pair is a net, not two sides of a gross one — the GL
detail shows Sales on both sides of the column and the Trial Balance now shows it
once. It now reads: *"Debit/Credit: net movement in the period, on the side it
falls. YTD Debit/YTD Credit: netted the same way, from the year-to-date movement
for profit and loss accounts and from the balance carried as at the report date
for balance sheet accounts."*

---

## Tests

**No existing test asserted the gross basis** — that is why the defect survived.
`go test ./internal/... -count=1` passed both before and after the query change,
so there was no assertion to update deliberately: nothing was weakened, and no
test was changed to accommodate the fix. The reports suite pinned the *shape*
(Total == sum of the rows rendered above it) and pinned gross figures only for
one-sided accounts, where netting changes nothing.

Two regression tests were added, both in
`internal/handlers/reports_backend_fixes_integration_test.go`:

* `TestHTTP_Reports_TrialBalanceNetsEachAccountBeforeTotalling` — posts a sale of
  100 and a refund of 30 to one account in one window and pins the row at
  0.00 / 70.00 (both pairs), the total at 70.00 rather than the gross 130, and the
  invariant that no account reads two-sided in either pair.
* `TestHTTP_Reports_ToDateSetsThePeriod` — probes all three endpoints with
  `toDate` alone, with `fromDate`+`toDate`, with a contradictory `date`, and with
  no parameters at all.

Both were checked against the old behaviour by temporarily restoring the gross
`CASE` expression and the old `dateParam(c, "date", …)` line: each fails there
(the Executive Summary reverting to *"For the period ending 11 September 2026"*),
and both pass on the fixed tree.

## Verification

```
$ gofmt -l .
(no output)

$ go build ./...
(no output; exit 0)

$ go test ./internal/... -count=1
ok  	github.com/shurco/goxero/internal/bankcoding	0.255s
ok  	github.com/shurco/goxero/internal/bankfeed	0.426s
ok  	github.com/shurco/goxero/internal/bankrules	0.666s
ok  	github.com/shurco/goxero/internal/bankstatement	0.795s
ok  	github.com/shurco/goxero/internal/config	0.939s
ok  	github.com/shurco/goxero/internal/database	1.287s
ok  	github.com/shurco/goxero/internal/handlers	8.837s
ok  	github.com/shurco/goxero/internal/logger	1.201s
ok  	github.com/shurco/goxero/internal/middleware	2.087s
ok  	github.com/shurco/goxero/internal/models	1.905s
ok  	github.com/shurco/goxero/internal/repository	2.948s
?   	github.com/shurco/goxero/internal/router	[no test files]
?   	github.com/shurco/goxero/internal/testutil	[no test files]
```

## The page on :5173

Read in the orca browser at 2026-09-11T17:27:50Z, page id
`d11166b0-ed1e-4d33-8ad6-0cb196b7e447`, after re-checking `orca tab list --json`
and after typing 31/12/2026 into *As of* and 01/01/2026 into *From* and pressing
Run — the same row as the API, on screen:

```
Run
Trial Balance
Trial Balance · Demo Company (Global) · From 1 January 2026 To 31 December 2026 · Debit/Credit: net movement in the period, on the side it falls. …
ACCOUNT   DEBIT   CREDIT   YTD DEBIT   YTD CREDIT
…
Total
42845.46
42845.46
42845.46
42845.46
```

No front-end change was needed or made: `web/` was not touched.

## The 250.00 contamination (left in place, as instructed)

A 250.00 spend on 2026-03-05 to `400` Advertising against `091` Business Savings
Account (journal 52, narration "savings demo") belongs to another session sharing
this worktree and database. It was not touched. It is why:

* the Total row reads 42,845.46 rather than Xero's 42,595.46 — +250.00 on each side;
* `400` Advertising reads 9,907.05 rather than 9,657.05;
* `091` Business Savings Account appears at all, at 0.00 / 250.00.

The database drifts while other sessions work in it; every figure above carries the
UTC timestamp at which it was read.
