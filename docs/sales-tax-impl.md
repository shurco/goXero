# Sales Tax: the backfill and the three report fixes — implementation record

Task `task_16638136a4eb`. Everything below was measured on 2026-09-11 between 19:46 and 19:55
local time (the host's clock reads +02; the API's own `DateTimeUTC` stamps are quoted where they
matter). The dev database is shared: another session writes to organisation
`6823b27b-c48f-4099-bb27-4202a4f496a2`. Nothing of theirs was deleted, edited or reverted, and no
figure below was tuned to be independent of their rows — section 7 reports what their rows do to
the live reading.

## AMENDMENT (coordinator, 2026-09-11 ~21:45 local) — read this before the rest

The worker that wrote this record stopped after its edits landed; I amended one of its decisions after
measuring the live database, and the amendment **supersedes** parts of sections 2, 3, 5, 6 and 10
below. Where a "before/after" capture or a stated expectation in those sections disagrees with this
section, this section is what the code now does.

**What was wrong with the decision.** The rule was: a rate whose side has an undetermined line keeps
that cell empty rather than carrying "a partial sum" (§2 fix (3), §3, §5). The premise is false. An
undetermined line is one whose `tax_amount` is 0.00 and whose rate is not 0% (`internal/repository/
report.go`, the `coded` CTE), so it contributes exactly 0.00 to its rate's sum: the sum of the
determined lines *is* the whole of the tax the ledger records under that rate. There was no partial
sum to withhold. Emptying the cell therefore did not hide a missing figure — it hid every other figure
under the rate. Section 6 measures the cost on the live database: one other-session line recording
0.00 suppressed 1,971.46 of recorded tax and printed Tax Paid 43.75 where the ledger holds 2,015.21.

**What the report does now.** It prints the ledger's own figure on every side a rate posted to
(empty only where the rate has no lines on that side at all), prints each rate's Net Tax — so the
rates' nets add up to the total — and states the ledger's gap in its own body: the Total row is
labelled `Total (<n> tax-typed line(s) record no tax)` when there is one, and the caveat names the
column, the line count and the rates. No figure is adjusted for the gap; the caveat says explicitly
that the missing figure, if there is one, is the ledger's and not the report's.

**The live reading, verbatim.** `GET /api/v1/reports/sales-tax?fromDate=2026-01-01&toDate=2026-12-31`,
tenant `6823b27b-c48f-4099-bb27-4202a4f496a2`, 2026-09-11 ~21:40 local:

```
Header      Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax
Row         Tax on Purchases (8.25%) | 0.00 | 19214.35 |  | 1971.46 | -1971.46
Row         Tax Exempt (0%) | 0.00 | 1838.60 |  | 0.00 | 0.00
Row         Tax on Consulting (8.25%) | 29375.43 | 0.00 | 2423.47 |  | 2423.47
Row         Tax on Goods (8.75%) | 163.75 | 500.00 | 14.33 | 43.75 | -29.42
SummaryRow  Total (1 tax-typed line records no tax) | 29539.18 | 21552.95 | 2437.80 | 2015.21 | 422.59
Row         Tax Collected and Tax Paid are the tax the period's posted journal lines record, line by line: a 0.00 is printed where the ledger records one, a 0% rate's own answer among them. The ledger records no tax at all for Tax Paid: 1 line naming a rate, under Tax on Purchases (8.25%). Those lines contribute nothing to the figures above, which are therefore the whole of the tax the ledger records; where one of them should have carried tax, that column is wrong by it and the missing figure is the ledger's, not the report's.
```

So the acceptance now holds **on the live database as it stands**, the other session's row included:
Collected 2,437.80 / Paid 2,015.21 / Net 422.59, matching `docs/xero-reference/
general-ledger-detail.txt` line 387 (`Total Sales ... (2,437.80)`) and line 499 (`Total Sales Tax
... (422.59)`), and matching account 820's own balance for the period:

```
code | name      | type     | balance
820  | Sales Tax | CURRLIAB | -422.5900
```

The other session's `INPUT` line (journal 1614) is still stated, and it is still not excluded: its
0.00 is in the column like every other recorded 0.00, and the caveat names it. Nothing was adjusted
on either side.

**Where the change is.** `internal/repository/report.go`: `SalesTaxRow.TaxCollected` and `.TaxPaid`
are plain `decimal.Decimal` (the nil-ing loop is gone) and the doc comments say what the sums are.
`internal/handlers/report_render.go`: `renderSalesTax` rewritten; `taxSideGap`/`totalLabel` replaced
by `taxLedgerGap`/`taxSideGap`/`taxGapClause`/`totalLabel` and `taxColumnsCaveat` reworded.
`internal/handlers/sales_tax_tax_amount_integration_test.go`:
`TestHTTP_Reports_SalesTaxShortRateIsStatedNotAdjusted` replaces
`TestHTTP_Reports_SalesTaxShortRateIsCoveredAndNamed`, and the acceptance test's per-rate table now
expects a Net Tax on every rate. `internal/handlers/reports_backend_fixes_integration_test.go`: the
three-rate month block asserts the nets sum to the total instead of asserting empty cells. The
migration (00025), `SalesTaxByRate`'s SQL and every other report are untouched.

Gates re-run after the amendment: `gofmt -l .` empty, `go build ./...`, `go vet ./...`,
`go test ./internal/... -count=1` all ok.

**Amendment, later again (coordinator, 2026-09-11 ~22:00 local): the entry path that produced journal
1614's shape now derives the tax.** Section 6 and `docs/reports-xero-parity-summary.md` section 6
recorded, and left open, the fact that the bank-coding path accepts a rate with no tax amount: the
reconcile screen sends `TaxType` and has no field for an amount (`web/src/lib/api.ts`, the
`statementLines.create` payload), so every line coded there with a rate reached the ledger as
`tax_type` *rate* with `tax_amount` 0.00 — the shape of the import defect of `docs/sales-tax-tax-
amount.md` §5.1, arriving from the application instead of from a migration. That is now fixed at the
one place every bank transaction is written: `internal/repository/bank_transaction.go` resolves the
rate from `tax_rates.effective_rate` and fills the amount in (`resolveLineTaxes`, `lineTax`), and
`internal/repository/gl.go`'s `postBankTransactionJournal` takes the tax out of the line when
`LineAmountTypes` is `Inclusive` instead of adding it on. Journal 1614 itself is another session's row
and is untouched; the report states it exactly as this amendment's capture above shows.

---

## 0. What changed, and where

| File | Lines | What |
| --- | --- | --- |
| `migrations/00025_document_line_tax_amounts.sql` | new, 188 lines; Up body 1–126, Down body 128–188 | backfills `gl_journal_lines.tax_amount` for document lines |
| `internal/repository/report.go` | `SalesTaxRow` 791–814, `SalesTaxByRate` 836–910, key lines 840, 847–851, 863–869, 901, 905 | fixes (2) and (4) |
| `internal/handlers/report_render.go` | `renderSalesTax` 916–1015, `taxSideGap` 1017–1026, `totalLabel` 1028–1040, `taxRateLabel` 1042–1052, `taxColumnsCaveat` 1054–1078, `pluralLines` 1080, `joinLabels` 1088 | fix (3) |
| `internal/handlers/sales_tax_tax_amount_integration_test.go` | new, 218 lines | the four regression tests and the acceptance test |
| `internal/handlers/reports_backend_fixes_integration_test.go` | 460–510 | the existing sales-tax test updated to the new visible behaviour |

No other report function was touched — `TrialBalance`, `CashSummary`, the journal feed ordering and
every other `SalesTax*` helper are exactly as they were. No file under `web/` was changed: the
renderer already prints an empty `ReportCell` as an empty cell (the "before" capture in section 3
has empty cells too), so the new empty-cell rule needs no frontend work.

---

## 1. The migration

### 1.1 What it writes

One column, `gl_journal_lines.tax_amount`, on the document lines that migration 00024 posted from
`INVOICE`, `CREDITNOTE` and `EXPENSECLAIM` journals. A *document line* is a line that names a rate
(`COALESCE(tax_type,'') <> ''`) and is not the tax leg itself (`accounts.code <> '820'`). The nine
`NONE` lines are candidates too — their source tax is 0.00, which is the right answer for a 0% rate.

The value has three sources, joined by the keys `docs/sales-tax-tax-amount.md` section 6.2 fixed:

* `invoice_line_items.tax_amount`, keyed on the journal's `source_id`, the line's `account_id`, its
  absolute amount and its description;
* `credit_note_line_items.tax_amount`, keyed on the source credit note, the account **code**, the
  absolute amount and the description;
* for the three `EXPENSECLAIM` journals — which 00024 wrote with a `NULL` `source_id` and no receipt
  rows — the sibling `820` leg of the same journal, paired by the ordinal 00024 laid the journal out
  with (`line/N` beside `tax/N`).

and the sign is

```sql
COALESCE(il.tax_amount, cnl.tax_amount, cp.tax)
  * CASE WHEN a.type IN ('REVENUE','SALES') THEN sign(-l.net_amount) ELSE sign(l.net_amount) END
```

so a customer credit note subtracts from Tax Collected instead of adding to it.

The predicate also **requires a resolved source** (`il.line_item_id IS NOT NULL OR
cnl.line_item_id IS NOT NULL OR cp.doc_line_id IS NOT NULL`), because an unsourced line is
unmeasured, not zero-tax. That is the migration's own statement that it cannot source every line in
principle; in this dataset it excludes nothing.

### 1.2 What it cannot touch

`tax_amount` is the only column in the `SET`. No `net_amount`, `gross_amount`, `account_id`, journal
row or document row is written, so no account balance can move and account 820 — the tax control
account — is not written at all. It cannot touch a line whose journal is not a document journal: the
four bank journals the other session created (1612–1615) are `BANKTRANSACTION` journals and the
predicate does not select them, in either direction.

### 1.3 The run, and the checks I ran on it

`./scripts/migrate up`, applied `2026-09-11 19:50:05` local (`goose_db_version.version_id = 25`,
`is_applied = t`).

| Check | Before (`19:49:57`) | After (`19:50:05`) |
| --- | --- | --- |
| `gl_journal_lines` rows | 439 | 439 |
| rows with non-zero `tax_amount` | 33 | 114 |
| `SUM(tax_amount)` over all lines | 74.16 | **4,453.01** |
| `md5` over every column of every line **except** `tax_amount` | `cedd0aefdc9ff3c1e16471664eb8358c` | `cedd0aefdc9ff3c1e16471664eb8358c` |
| per-account net balances (all 59 accounts) | listed | **identical, account by account** |
| account 820 net balance, 2026-01-01..2026-12-31 | 422.59 | 422.59 |

**This is the check that no account balance moved**, and it is the one the task asked for: the hash
covers `journal_id, line_id, account_id, net_amount, gross_amount, description, tax_type`,
`created_date_utc` and the rest, and it did not change, so only `tax_amount` was written; the
per-account balance list before and after is byte-identical, so nothing that sums a column other than
`tax_amount` can have moved. 4,453.01 is exactly the total `docs/sales-tax-tax-amount.md` section 6.5
predicted for this backfill.

**Row counts.** Re-running the migration's own `doc` CTE read-only at `19:53:21`:

* **83 rows selected** — 74 `INVOICE`, 5 `CREDITNOTE`, 4 `EXPENSECLAIM`.
* **83 rows written**; **81 of them now carry a non-zero value** (summing to 4,378.85), so **81 rows
  changed and 2 were no-ops**. The two are `INVOICE` journals 1534 and 1597 (accounts 420 and 425,
  rate `NONE`, source tax 0.00) — they were 0.00 and are correctly 0.00.
* **0 candidate lines could not be sourced.** Every one of the 83 resolves. The three `EXPENSECLAIM`
  journals 1570–1572 (lines 1.29 / 2.25 / 8.84) look unsourced under a naive join because their
  `source_id` is `NULL`; the migration's `claim_pairs` pairing recovers them from the sibling `820`
  leg.
* Proof that every written row was 0.00 beforehand, which is what makes `down` exact:
  4,453.01 − 4,378.85 = **74.16**, exactly the pre-migration non-zero total, so the 33 pre-existing
  non-zero lines are disjoint from the 83 rows the migration writes.
* Idempotent in effect: the value expression never reads `tax_amount`, so a second `up` recomputes the
  identical number for the identical row.

### 1.4 `down`

`-- +goose Down` repeats the identical candidate CTE and writes `SET tax_amount = 0.00` to the same
83 rows. The file's comment explains why that is exact and touches nothing else: 00024 wrote every
one of those rows with 0.00, the predicate is a function of the documents (never of `tax_amount`),
and the rows it selects are therefore exactly the rows `up` changed — so restoring them to 0.00
restores the pre-migration state and can restore nothing else. The 33 lines that were non-zero before
the migration are not in the predicate and are not written by `down` either.

**I did not run `migrate down` or `migrate reset`** (instructed not to), so this is argued from the
predicate and from the before/after evidence in 1.3 rather than observed.

---

## 2. The three report fixes

### (2) "measured" means the source supplied one — `internal/repository/report.go:851`

```sql
(l.tax_amount <> 0 OR COALESCE(rt.zero_rated, false)) AS determined
```

with `rates` (`:838–841`) resolving each `tax_type` to its own rate:

```sql
SELECT DISTINCT ON (tr.tax_type) tr.tax_type,
       (COALESCE(tr.effective_rate, 0) = 0) AS zero_rated
  FROM tax_rates tr WHERE tr.organisation_id = $1
 ORDER BY tr.tax_type, tr.name
```

A line is measured when its tax was determined: the document supplied one (`tax_amount <> 0`), **or**
its rate is 0% so zero is the answer. The guard that decides whether a side may be printed
(`:901`, `:905`) is unchanged in shape — a side totals when `Measured > 0 AND Measured == Lines` — but
`Lines` and `Measured` are new columns in `SalesTaxRow` (`:806–813`), so it now counts *lines*, not
*rates with a non-zero sum*. A `tax_type` with no `tax_rates` row is treated as non-zero-rated: its
lines print only when they supplied a tax. That is the conservative direction.

### (3) a side totals when every rate that has lines on that side measured it — `internal/handlers/report_render.go:916`

`renderSalesTax` accumulates two `taxSideGap` values (`:1017`), one per side, while it walks the rows:

* a rate with lines on the side and every one of them measured: counted in `measured`;
* a rate with lines on the side and at least one undetermined line: its label is added to `rates` and
  the shortfall `lines - measured` to `lines`, and **its cell is left empty — never a partial sum**.

The Total row prints a side's sum when that side's `measured > 0` (`:989–996`), and its label comes
from `totalLabel` (`:1028`), which appends the coverage of each side that has a gap:

```
Total (Tax Paid covers 2 of 3 rates)
```

A total is never printed as if it were complete: the label carries the coverage, and the caveat
below the table states the gap in *lines* — which rates are short, how many lines under them carry no
determined tax, and that their tax is not in the total. Where every rate's lines on a side are
determined, the label is the plain `Total` and the caveat says so instead. No cell ever reads `0.00`
as a stand-in for "not measured": an unmeasured side is empty, and a measured zero (a 0% rate) prints
`0.00`.

Row Net Tax needs both of its own cells (`:975–983`), so a row whose collected or paid side is short
leaves Net Tax empty as well.

### (4) the tax columns count every posted line that carries a rate — `internal/repository/report.go:848, 863–869`

`coded` now separates the P&L side from the tax side:

```sql
a.type IN ('REVENUE','SALES')                        AS is_income,
a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS',
           'DEPRECIATN','WAGES')                     AS is_cost,
```

* **Net Sales** `SUM(CASE WHEN is_income THEN -net_amount END)` — unchanged;
* **Net Purchases** `SUM(CASE WHEN is_cost THEN net_amount END)` — unchanged (`costAccountTypes`, no
  `FIXED`, so the capital accounts 710/720 stay out);
* **Tax Collected** `SUM(CASE WHEN is_income THEN tax_amount END)` and **Tax Paid**
  `SUM(CASE WHEN NOT is_income THEN tax_amount END)` — widened to every account class, so the two
  columns together cover every posted line that carries a rate.

The row key stays `tax_type`. That widening is the 387.60 of input tax on accounts 710 Computer
Equipment (`Xero GL` section total 311.39) and 720 Office Equipment (76.21) that the old
`costAccountTypes` filter dropped, which is exactly the gap the task named. Note the two fixes then
interact as intended: Tax Paid widens to all classes **and** a short rate still empties its cell, so
the widening does not smuggle an undetermined rate into a total.

---

## 3. The rendered report, before and after

Both blocks below are the API's own `Cells[].Value`, pasted unedited. The **before** capture is from
the pre-fix binary then holding `:8080` (`DateTimeUTC 2026-09-11T17:46:37.602205Z`); the **after**
capture is from the fixed build on `:8137` (`DateTimeUTC 2026-09-11T17:51:56.895761Z`), with all of
the other session's rows present. `GET /api/v1/reports/sales-tax?fromDate=2026-01-01&toDate=2026-12-31`.

**Before:**

```
Header      Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax
Row         Tax on Purchases (8.25%) | 0.00 | 19214.35 |  |  | 
Row         Tax Exempt (0%) | 0.00 | 1838.60 |  |  | 
Row         Tax on Consulting (8.25%) | 29375.43 | 0.00 |  |  | 
Row         Tax on Goods (8.75%) | 163.75 | 500.00 |  |  | 
SummaryRow  Total | 29539.18 | 21552.95 |  |  | 
Row         Tax Collected, Tax Paid and Net Tax are left empty: no tax rate carries a tax amount on every one of its posted lines, and this report will not total part of a rate. Some lines do record a tax amount; they are not summed here because the rest of their rate's lines do not.
```

**After:**

```
Header      Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax
Row         Tax on Purchases (8.25%) | 0.00 | 19214.35 |  |  | 
Row         Tax Exempt (0%) | 0.00 | 1838.60 |  | 0.00 | 
Row         Tax on Consulting (8.25%) | 29375.43 | 0.00 | 2423.47 |  | 
Row         Tax on Goods (8.75%) | 163.75 | 500.00 | 14.33 | 43.75 | -29.42
SummaryRow  Total (Tax Paid covers 2 of 3 rates) | 29539.18 | 21552.95 | 2437.80 | 43.75 | 2394.05
Row         Tax Collected and Tax Paid count a line only where its tax is determined: the document behind the line supplied one, or the rate is 0% so zero is the answer. Tax Paid leaves out 1 line with no determined tax, under Tax on Purchases (8.25%); a short rate's cell stays empty rather than carrying a partial sum, and the Tax Total row is labelled with the coverage. Where every line under a rate is determined, that rate's cells and the total over it are complete.
```

`GET /api/v1/reports/bas` prints the same five rows and the same caveat (checked at
`17:54:31.471770Z`): it is the same `renderSalesTax` behind a different report name.

Read the after block as the two things it is meant to say at once. **Tax Collected 2,437.80 is
complete** — every rate with sales lines measured them, so the label names only the paid side's gap;
**Tax Paid 43.75 is not**, because one line on the `INPUT` rate has no determined tax, so `INPUT`'s
cell stays empty rather than showing a partial 1,971.46, and the label says
`Tax Paid covers 2 of 3 rates`. That gap is the other session's journal 1614, and section 7 shows the
same report over the reference dataset — where the line does not exist — printing the complete
2,015.21 / 422.59.

### The caveat, and the four states it can be in

`taxColumnsCaveat` (`report_render.go:1054`) writes one sentence in one of four states:

1. **No rate rows at all** — nothing to total.
2. **No gap on either side** — the caveat ends: *"Where every line under a rate is determined, that
   rate's cells and the total over it are complete."* The label is the plain `Total`. (This is the
   state of the reference dataset and of the month-window test.)
3. **One side short** — the live state quoted above.
4. **Both sides short** — both are named, each with its own line count and rate labels.

The report says what it left out, in lines, in the words a reader can check against the ledger:
*"Tax Paid leaves out 1 line with no determined tax, under Tax on Purchases (8.25%)"*.

---

## 4. The per-rate reading over the reference dataset, against Xero's capture

The live database contains the other session's four bank journals, so the *Xero check* is run against
the reference dataset — the ledger exactly as 00023/00024 imported it, with no other writer. That is
`docs/sales-tax-tax-amount.md` section 6.6's read-only simulation, restricted to the report's own
measure, run at **2026-09-11 19:52:54** (the same query, with fix (2)'s `determined` and fix (4)'s
class split, and with journals 1612–1615 excluded):

| `tax_type` | rate | Net Sales | Net Purchases | lines S / P | measured S / P | Tax Collected | Tax Paid | **Net** |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `OUTPUT` | Tax on Consulting (8.25%) | 29,375.43 | — | 35 / 0 | 35 / 0 | **2,423.47** | — | **2,423.47** |
| `OUTPUT2` | Tax on Goods (8.75%) | 163.75 | 500.00 | 5 / 1 | 5 / 1 | **14.33** | **43.75** | **−29.42** |
| `INPUT` | Tax on Purchases (8.25%) | — | 19,198.85 | 0 / 73 | 0 / 73 | — | **1,971.46** | **1,971.46** |
| `NONE` | Tax Exempt (0%) | — | 1,823.60 | 0 / 8 | 0 / 8 | — | **0.00** | **0.00** |
| | **Total** | **29,539.18** | **21,522.45** | | | **2,437.80** | **2,015.21** | **422.59** |

Figure by figure against `docs/xero-reference/general-ledger-detail.txt` (lines quoted from the frozen
capture, which was not edited):

| Figure | The report now prints | Xero's captured figure | Where | Agrees |
| --- | --- | --- | --- | --- |
| Tax Collected | **2,437.80** | 2,437.80 | the `Total Sales` row's tax column, line 387 | **yes, to the cent** |
| Tax Paid | **2,015.21** | 2,015.21 | the tax column summed over the expense and asset sections (`Advertising` 796.70, `Cleaning` 91.58, `Computer Equipment` 311.39, `Consulting & Accounting` 7.17, `Freight & Courier` 9.53, `General Expenses` 13.72, `Light, Power, Heating` 27.71, `Motor Vehicle Expenses` 53.99, `Office Equipment` 76.21, `Office Expenses` 73.66, `Printing & Stationery` 7.79, `Purchases` 64.02, `Rent` 270.09, `Repairs and Maintenance` 156.49, `Telephone & Internet` 19.50, `Travel - National` 35.66) | **yes, to the cent** |
| Net Tax | **422.59** | 422.59 | the `Total Sales Tax` row, line 499; `trial-balance.txt:33`; `balance-sheet.txt:26` | **yes, to the cent** |
| `OUTPUT` net | **2,423.47** | 2,423.47 | line 387's own rate column, `Tax on Consulting` | **yes, to the cent** |
| `OUTPUT2` net | **−29.42** | −29.42 | `Tax on Goods`: Xero files it as 16.08 credited / 45.50 debited | **yes, to the cent — presentation differs, see below** |
| `INPUT` net | **1,971.46** | 1,971.46 | `Tax on Purchases`: Xero files it as 22.57 credited / 1,994.03 debited | **yes, to the cent — presentation differs, see below** |
| `NONE` | **0.00** | 0.00 | `Tax Exempt`, 9 lines | **yes** |
| Net Sales | **29,539.18** | 29,539.18 | the `Total Sales` row's net column | **yes, to the cent** |
| the tax Paid **would** have been (old class split) | 1,627.61 | 2,015.21 | as above | **no — 387.60 short**, and the 387.60 is the two capital sections named in the capture |
| Net Tax **would** have been (old class split) | 810.19 | 422.59 | as above | **no — 387.60 short** |

Two rows of that table differ in *presentation* and not in their net, and it is worth being exact
about why rather than calling it a discrepancy. Xero's GL Detail groups a rate's tax by the direction
of the `820` movement (so a credit note's input tax appears under `Tax on Purchases` as a *credit* of
22.57, and the tax on the credit note's goods line as a credit of 16.08 under `Tax on Goods`); this
report groups by the class of the account the line hit (a credit note's expense line is a purchase
line, so its reversed tax reduces Tax Paid). `1,994.03 − 22.57 = 1,971.46` and `45.50 − 16.08 =
29.42`, and the nets — the figures that reconcile to 820 — are identical either way. The per-rate nets
above are therefore Xero's own numbers to the cent, which is the strongest check available: no *Sales
Tax Report* was captured (`docs/xero-reference/README.md` item 8), so there is no captured rendered
report to lay side by side.

**The identity the report exists to satisfy, checked independently of the report.** At
`19:53:47`, `gl_journal_lines` for the period, account `820`:

```
round(sum(l.tax_amount) filter (where a.code='820'),2)  =    0.00   (the migration did not write 820)
round(-sum(l.net_amount) filter (where a.code='820'),2) =  422.59   over 107 lines
```

Net Tax = **422.59** = account 820's own balance for 2026-01-01..2026-12-31, read from the ledger and
not from the report. `820`'s `tax_amount` is 0.00 because the migration writes only document lines
whose account code is not 820 — the tax legs keep the values 00024 gave them.

---

## 5. The tests, and the proof each fails without its fix

`internal/handlers/sales_tax_tax_amount_integration_test.go`, using the existing harness
(`newHarness`, `resetLedger`, `postManualJournal`, the `taxedJournalLine` line helper):

| Test | Line | What it pins |
| --- | --- | --- |
| `TestHTTP_Reports_SalesTaxZeroRatedLineIsMeasured` | 32 | a 0%-rated line with `tax_amount` 0 **is** measured, prints `0.00`, and does not block its side |
| `TestHTTP_Reports_SalesTaxTotalSpansRatesWithLinesOnTheSide` | 63 | a rate with no lines on a side does not block that side's total |
| `TestHTTP_Reports_SalesTaxCapitalInputTaxIsTaxPaid` | 88 | input tax on a `FIXED`-class account reaches Tax Paid while Net Purchases still excludes it |
| `TestHTTP_Reports_SalesTaxShortRateIsCoveredAndNamed` | 113 | one undetermined line leaves its cell empty, the total is labelled with its coverage, and the caveat names the shortfall in lines |
| `TestHTTP_Reports_SalesTaxMatchesTheReferenceDataset` | 170 | the acceptance figures of section 4, plus Net Tax == account 820's balance read from the ledger |

`reports_backend_fixes_integration_test.go:460` `TestHTTP_Reports_SalesTaxRowsFollowTheLedger` was
updated: its month window is complete under the new rule, so it now asserts the plain `Total` label,
17.00 / 8.24 / 8.76, and the "every line under every rate has a determined tax" caveat that replaced
the old "support Tax Collected" wording.

Each fix was reverted on its own and the tests re-run; the messages below are pasted from those runs.

**Without fix (2)** (`determined` back to `l.tax_amount <> 0`):

```
--- FAIL: TestHTTP_Reports_SalesTaxZeroRatedLineIsMeasured (0.11s)
        	Messages:   	a 0% rate's tax is a measured zero and must be printed, not left empty
        	Messages:   	a 0% rate's tax is a measured zero and must be printed, not left empty
        	Messages:   	both sides of a 0% rate are measured
        	Messages:   	no report row labelled "Total"
FAIL
```
— the `NONE` row's `Tax Paid` cell goes empty again, its total cannot print, and the Total row stops
existing. **Breakage mechanism:** `tax_amount = 0` on a 0%-rated line is indistinguishable from an
undetermined line, which is the whole defect.

**Without fix (3)** (a short side prints its partial sum, label plain `Total`):

```
--- FAIL: TestHTTP_Reports_SalesTaxTotalSpansRatesWithLinesOnTheSide (0.13s)
        	Messages:   	tax collected over the one rate that has sales
        	Messages:   	tax paid over the one rate that has purchases
        	Messages:   	net tax
--- FAIL: TestHTTP_Reports_SalesTaxShortRateIsCoveredAndNamed (0.07s)
        	Messages:   	a total is never printed as if it were complete
        	Messages:   	tax collected over the rate that measured it
        	Messages:   	tax paid over the rates that measured it, short rate left out
        	Messages:   	net tax over the measured rates
FAIL
```
— a rate with no lines on a side keeps the side from totalling at all, and a short rate's partial sum
gets printed as if it were the whole side. **Both directions of the defect are caught.**

**Without fix (4)** (`is_cost` used for the tax columns again):

```
--- FAIL: TestHTTP_Reports_SalesTaxCapitalInputTaxIsTaxPaid (0.08s)
        	            	expected: "82.50"
        	            	actual  : "0.00"
        	Messages:   	the input tax on a capital account is still tax paid
        	            	expected: "82.50"
        	            	actual  : "0.00"
        	Messages:   	the capital input tax reaches the total
FAIL
```
— the test's `FIXED`-class line carries 82.50 of input tax; the tax column reads 0.00 while the same
test's Net Purchases assertion still passes, which is the exact asymmetry fix (4) removes.

**The acceptance test, without (2)+(4)** — `1,627.61` / `810.19`, the class-split figures
`docs/sales-tax-tax-amount.md` section 6.5 predicted:

```
--- FAIL: TestHTTP_Reports_SalesTaxMatchesTheReferenceDataset (0.10s)
        	            	expected: "Total"
        	            	actual  : "Total (Tax Paid covers 2 of 3 rates)"
        	Messages:   	every rate that posted to a side measures it, so the total is complete
        	            	expected: "2015.21"
        	            	actual  : "1627.61"
        	Messages:   	tax paid
        	            	expected: "422.59"
        	            	actual  : "810.19"
        	Messages:   	net tax
        	            	expected: "1971.46"
        	            	actual  : "1583.86"
        	Messages:   	INPUT Tax Paid
        	            	expected: "0.00"
        	            	actual  : ""
        	Messages:   	NONE Tax Paid
        	Messages:   	Net Tax == the credit in account 820: got 810.19, want 422.59
FAIL
```

**The acceptance test, without (3)** — every total goes to `0.00`:

```
--- FAIL: TestHTTP_Reports_SalesTaxMatchesTheReferenceDataset (0.08s)
        	            	expected: "2437.80"   actual: "0.00"   Messages: tax collected
        	            	expected: "2015.21"   actual: "0.00"   Messages: tax paid
        	            	expected: "422.59"    actual: "0.00"   Messages: net tax
FAIL
```

After each experiment the file was restored from the copy taken immediately before it, and the full
suite re-run green.

---

## 6. The other session's rows in the live reading

No write from the other session arrived after `2026-09-11 19:30:53`; the highest journal number at
`19:53:47` was **1615**. Their rows do not change Tax Collected — the live and reference figures are
both **2,437.80** — but they do change Tax Paid and Net Tax, and here is exactly why:

| | Live (`1612`–`1615` present) | Reference dataset (excluded) |
| --- | --- | --- |
| Tax Collected | 2,437.80 | 2,437.80 |
| Tax Paid | **43.75** | **2,015.21** |
| Net Tax | **2,394.05** | **422.59** |
| Total label | `Total (Tax Paid covers 2 of 3 rates)` | `Total` |
| Net Purchases | 21,552.95 | 21,522.45 |

The cause is one row, and it is theirs:

```
journal 1614 | BANKTRANSACTION | account 453 (EXPENSE) | tax_type INPUT | net 15.50 | tax_amount 0.00
             | "7-Eleven" | created_date_utc 2026-09-11 19:30:50.762977+02
```

It is a bank line with a rate but no source document and no `820` sibling, so its tax is **not
determined** — it is exactly the case fix (2) refuses to call a measured zero. It sits on the `INPUT`
rate, which has 73 determined lines and this one undetermined line, so `INPUT`'s Tax Paid cell stays
empty (never a partial 1,971.46) and the total over the measured rates is 43.75 — the `OUTPUT2` paid
cell alone. The caveat says *"Tax Paid leaves out 1 line with no determined tax, under Tax on
Purchases (8.25%)"*, which is the line above. **I did not touch it, code it to a document, or exclude
it from the report**; the same report over the reference dataset prints 2,015.21 / 422.59.

Two of the other session's rows move Net Purchases by 30.50 — journal **1613** (rate `NONE`, 15.00)
and journal **1614** (rate `INPUT`, 15.50) — from 21,522.45 to 21,552.95. That column is not one of
the three this task widens, and it is unchanged by this work.

Reading of `GET /api/v1/reports/sales-tax` and `/api/v1/reports/bas` at
`DateTimeUTC 2026-09-11T17:54:31Z` (19:54:31 local), both 200, both identical:

```
Header      Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax
Row         Tax on Purchases (8.25%) | 0.00 | 19214.35 |  |  | 
Row         Tax Exempt (0%) | 0.00 | 1838.60 |  | 0.00 | 
Row         Tax on Consulting (8.25%) | 29375.43 | 0.00 | 2423.47 |  | 
Row         Tax on Goods (8.75%) | 163.75 | 500.00 | 14.33 | 43.75 | -29.42
SummaryRow  Total (Tax Paid covers 2 of 3 rates) | 29539.18 | 21552.95 | 2437.80 | 43.75 | 2394.05
Row         Tax Collected and Tax Paid count a line only where its tax is determined: the document behind the line supplied one, or the rate is 0% so zero is the answer. Tax Paid leaves out 1 line with no determined tax, under Tax on Purchases (8.25%); a short rate's cell stays empty rather than carrying a partial sum, and the Tax Total row is labelled with the coverage. Where every line under a rate is determined, that rate's cells and the total over it are complete.
```

So: **on the live database the acceptance figures are Tax Collected 2,437.80, Tax Paid 43.75, Net Tax
2,394.05; on the reference dataset they are 2,437.80 / 2,015.21 / 422.59, and 422.59 equals account
820's own balance.** The difference is one other-session row, named above, and nothing was adjusted to
make the two agree.

---

## 7. What this spec did not anticipate

1. **Someone else's pre-fix server was holding a port as I started.** `:8080` was occupied by
   `/tmp/goxero-server` (PID 17998, started 19:24:54 — the evidence worker's build of the *unfixed*
   tree) and `:8099` by `gx-server` (PID 71891). I did not kill either process. I built the fixed
   tree to `/tmp/st/goxero-fixed` and served it on `:8137`, which is why the "before" and "after"
   captures in section 3 come from two different ports. **A reader who curls `:8080` will get the
   pre-fix report** and should use a freshly built binary.
2. **The `EXPENSECLAIM` journals have `NULL` `source_id` and no receipt rows.** Three of the 83
   migration candidates (journals 1570–1572, tax 1.29 / 2.25 / 8.84) are invisible to the
   `invoice_line_items` / `credit_note_line_items` joins; 00024's own `820` leg is the only surviving
   record. A naive audit query that pairs claim lines with the `820` legs *without* excluding the
   `801` control leg shifts the ordinals and reports these three as unsourced. The migration excludes
   `('801','820')` from the document side of the pairing and sources all four of them; I hit and
   corrected exactly this in my own verification, and it is worth knowing before re-deriving the
   numbers.
3. **A rate with no lines on a side is not a rate that "measured zero".** `NONE` has 8 purchase
   lines and no sales line, so the reference reading prints `NONE` Tax Paid `0.00` and leaves its Tax
   Collected cell empty. Empty means "no lines here"; `0.00` means "lines here, their tax is zero".
   That distinction is load-bearing for fix (3) and was not spelled out in the spec.
4. **The `Total` label is not a stable string any more.** A test or a UI that looks up the row
   labelled `Total` must allow `Total (… covers N of M rates)`; `salesTaxTotalRow` in the new test
   file finds the single six-cell `SummaryRow` instead. The month-window test in
   `reports_backend_fixes_integration_test.go` had to be updated for this reason.
5. **Xero's GL tags 9 `Tax Exempt` lines; the ledger has 8 of its own plus the other session's 1613.**
   The two lines Xero tags and the ledger does not carry `tax_type` NULL on balance-sheet accounts
   (the Conversion Balance Journal pair, journal 1603) and cannot move any figure. It becomes visible
   only because a 0% rate's lines are now *measured*, which is worth stating since it is a difference
   between the two datapoints on a line that now prints `0.00` instead of staying empty.
6. **`tax_amount` is nullable in the schema but no line is NULL** (checked: 0 NULLs at 19:53:12). The
   migration's value expression is `NULL`-safe in the sense that a candidate with no source is
   excluded rather than set to NULL, which is why its predicate requires a source.

## 8. What I could not determine, stated plainly

* **Whether the live report will reach 2,015.21 / 422.59 depends on the other session**, not on this
  code: it happens when journal 1614's `INPUT` line is given a determined tax (coded to a document) or
  removed. As of `19:54:31` it has not been. I did not touch their row.
* **`down` was not executed** (instructed not to run `migrate down`/`migrate reset`), so its
  row-exactness is argued from the predicate plus the disjointness proof in 1.3 rather than observed
  in a live run.
* **No captured Xero *Sales Tax Report* exists** to check the empty-cell and coverage-label rules
  themselves (`docs/xero-reference/README.md` item 8). Xero's GL tax column confirms every *figure*
  to the cent, but "a short rate's cell stays empty and the total is labelled with its coverage" is
  the coordinator's product decision, not something the capture can corroborate.
* **The web app was not rendered** (no browser CLI per the task, and no iframe anywhere). The gates
  for `web/` were run and pass, and the renderer's empty-cell handling is unchanged from the "before"
  state, but I did not look at the printed page.
* **I cannot say what the other session intends for journals 1612 and 1615**: they add no tax-typed
  line to the report's rows (1612 and 1615 are not in section 6's table). Only 1613 and 1614 move
  anything this report prints, and by the amounts given.

## 9. Gates

```
gofmt -l .            (empty)
go build ./...        OK
go vet ./...          OK
go test ./internal/... -count=1    all packages ok
cd web && bun run check           0 errors, 0 warnings
cd web && bun run build           OK
```

## 10. Reproducing

```sh
./scripts/migrate up                       # applies 00025 (already applied here at 19:50:05)
go build -o /tmp/goxero-fixed ./cmd/server && /tmp/goxero-fixed   # not :8080 if another session holds it
TOK=$(curl -s -X POST localhost:8137/api/auth/login -H 'Content-Type: application/json' \
      -d '{"email":"admin@demo.local","password":"admin123"}' | jq -r .token)
curl -s -H "Authorization: Bearer $TOK" \
  "localhost:8137/api/v1/reports/sales-tax?fromDate=2026-01-01&toDate=2026-12-31&organisationId=6823b27b-c48f-4099-bb27-4202a4f496a2"
go test ./internal/handlers/ -run 'SalesTax' -count=1
```

The reference-dataset reading of section 4 is the same measure with
`AND j.journal_number NOT IN (1612,1613,1614,1615)` added to the line query — read-only, nothing
written.
