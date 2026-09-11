# The aged reports onto Xero's, and the Budget Summary made honest

TASK I (`task_5da7c9cda15b` / `ctx_d96e881ce86d`), 2026-09-11.

`docs/reports-backend-fixes.md` § *Coordinator follow-ups* → *The aged reports:
the labels, the percentage row, the payables sections* left three things undone,
on the grounds that the *partition* was wrong rather than the wording, and that
nothing could be verified against live data because the dev database held no
invoices at all. Both premises have changed: the partition is now the reference's
own, and `migrations/00024_xero_reference_source_documents.sql` has since
imported the reference dataset, so this work is checkable cell by cell against
the captures in `docs/xero-reference/`, which is what this report does.

Everything below was re-derived here rather than taken from the follow-up note:
the note's reproducer was run again, the reference's own column totals were
re-added by hand, and the whole comparison was redone against the live report
payloads rather than against the CSVs. Two of the note's claims did not survive
that: the reference dataset *is* now in the database (the note reported
`select count(*) from invoices` → 0), and the Expense Claims block is not merely
representable from the schema but present in it, once 00024's claim import is in
(§5). The one claim that did survive is the important one: the labels and the
partition had to move together.

## Summary of what changed

| | before | after |
|---|---|---|
| Columns | `Current \| 1-30 \| 31-60 \| 61-90 \| > 90` | `< 1 Month \| 1 Month \| 2 Months \| 3 Months \| Older` |
| Boundaries | not yet due separate; `> 90` in one column | `≤ 30 / 31-60 / 61-90 / 91-120 / > 120` days past due, not-yet-due inside `< 1 Month` |
| `Percentage of total` | absent | summary reports only, computed from the rows the report printed; empty over an empty report |
| Payables blocks | one table | `Aged Payables` + `Expense Claims` + a `Total` that is the sum of both |
| Budget Summary | header + a `Total 0.00` row | header only, with the gap stated in `ReportTitles[3]` |

Files: `internal/repository/report.go` (`Aged`, `AgedByContact`, the new
`AgedExpenseClaims`), `internal/handlers/report_render.go` (`renderAged`,
`renderAgedByContact`, `renderBudgetSummary` and their helpers),
`internal/handlers/report.go` (the two aged handlers load what the renderer
needs), the aged tests, and this file. Nothing else was touched: not
`internal/router/router.go`, not `migrations/**`, not `docs/xero-reference/**`,
not `web/**`.

## 1. The columns

Xero's five amount columns, from
`docs/xero-reference/aged-receivables-summary.txt` and `aged-payables-summary.txt`
(both carry the same heading line, which is why one table serves both reports):

```
Contact	< 1 Month	1 Month	2 Months	3 Months	Older	Total
```

Read as days past due — the captures' own fifth line says `Ageing by due date` —
that is `≤ 30 / 31-60 / 61-90 / 91-120 / > 120`, with everything not yet due
folded into `< 1 Month`. goXero's old columns were `Current / 1-30 / 31-60 /
61-90 / > 90`: a different partition, not a different spelling. Renaming alone
would have published the whole receivables balance under `> 90` where Xero splits
it at 120 days.

`internal/repository/report.go` now ages with:

```sql
CASE WHEN $3::date - COALESCE(i.due_date, i.date) <= 30              THEN i.amount_due END  -- "< 1 Month"
CASE WHEN $3::date - COALESCE(i.due_date, i.date) BETWEEN 31 AND 60  THEN i.amount_due END  -- "1 Month"
CASE WHEN $3::date - COALESCE(i.due_date, i.date) BETWEEN 61 AND 90  THEN i.amount_due END  -- "2 Months"
CASE WHEN $3::date - COALESCE(i.due_date, i.date) BETWEEN 91 AND 120 THEN i.amount_due END  -- "3 Months"
CASE WHEN $3::date - COALESCE(i.due_date, i.date) > 120              THEN i.amount_due END  -- "Older"
```

The five arms are exhaustive and disjoint — a negative difference (not yet due)
falls into the first, and `BETWEEN`/`>` leave no gap at 121 — so every
outstanding invoice is in exactly one column and the columns are the row's total.
`AgedByContact` uses the same five arms and the same `COALESCE(due_date, date)`
fallback, so the summary and the drill-down cannot disagree about an invoice's
age. The nullable due date is still handled: an invoice with no due date ages
from its invoice date, exactly as the capture's `Ageing by due date` line implies
when there is no due date to age from.

## 2. Cell by cell against the captures, as at 31 December 2026

### Where the payloads came from

```sh
$ go build -o /tmp/gx-server2 ./cmd/server          # this worktree
$ SERVER_PORT=8099 /tmp/gx-server2 &                # 8080 belongs to the running app
$ T=$(cat /tmp/gx.token); H="Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2"
$ curl -s -H "Authorization: Bearer $T" -H "$H" \
      "http://localhost:8099/api/v1/reports/aged-receivables?date=2026-12-31" -o /tmp/gx-live-ar.json
$ curl -s -H "Authorization: Bearer $T" -H "$H" \
      "http://localhost:8099/api/v1/reports/aged-payables?date=2026-12-31" -o /tmp/gx-live-ap.json
```

The database is the shared dev one (`:5432`, `bonefish-postgres-1`), which 00024
has filled with the reference source documents; the test database on `:5433` was
not touched by any of this. The two sides are put next to each other by
`/tmp/gx-aged-flatten.py` (appendix B): it flattens the payload to
tab-separated lines, printing a `Section`'s `Title` on its own line the way
Xero's export prints a block heading, and marks every cell that differs as
printed.

### Aged Receivables Summary

```sh
$ python3 /tmp/gx-aged-flatten.py /tmp/gx-live-ar.json docs/xero-reference/aged-receivables-summary.txt
```

```text
goXero (flattened payload)                                      | Xero (reference export)
----------------------------------------------------------------|----------------------------------------
  Contact	< 1 Month	1 Month	2 Months	3 Months	Older	Total       | Contact	< 1 Month	1 Month	2 Months	3 Months	Older	Total
!!Basket Case	0.00	0.00	0.00	914.55	0.00	914.55                 | Basket Case	-	-	-	914.55	-	914.55
!!Bayside Club	0.00	0.00	0.00	234.00	0.00	234.00                | Bayside Club	-	-	-	234.00	-	234.00
!!City Limousines	0.00	0.00	0.00	703.63	488.20	1191.83          | City Limousines	-	-	-	703.63	488.20	1,191.83
!!DIISR - Small Business Services	0.00	0.00	0.00	0.00	270.63	270.63| DIISR - Small Business Services	-	-	-	-	270.63	270.63
!!Marine Systems	0.00	0.00	0.00	396.00	0.00	396.00              | Marine Systems	-	-	-	396.00	-	396.00
!!Ridgeway University	0.00	0.00	0.00	6187.50	0.00	6187.50       | Ridgeway University	-	-	-	6,187.50	-	6,187.50
!!Total	0.00	0.00	0.00	8435.68	758.83	9194.51                   | Total	-	-	-	8,435.68	758.83	9,194.51
!!Percentage of total	0.00%	0.00%	0.00%	91.75%	8.25%	100.00%    | Percentage of total	-	-	-	91.75%	8.25%	100.00%

34 differing cell(s) of 9 row(s) compared
  row 2 col 2: goXero '0.00' vs Xero '-'
  row 2 col 3: goXero '0.00' vs Xero '-'
  row 2 col 4: goXero '0.00' vs Xero '-'
  row 2 col 6: goXero '0.00' vs Xero '-'
  row 3 col 2: goXero '0.00' vs Xero '-'
  row 3 col 3: goXero '0.00' vs Xero '-'
  row 3 col 4: goXero '0.00' vs Xero '-'
  row 3 col 6: goXero '0.00' vs Xero '-'
  row 4 col 2: goXero '0.00' vs Xero '-'
  row 4 col 3: goXero '0.00' vs Xero '-'
  row 4 col 4: goXero '0.00' vs Xero '-'
  row 4 col 7: goXero '1191.83' vs Xero '1,191.83'
  row 5 col 2: goXero '0.00' vs Xero '-'
  row 5 col 3: goXero '0.00' vs Xero '-'
  row 5 col 4: goXero '0.00' vs Xero '-'
  row 5 col 5: goXero '0.00' vs Xero '-'
  row 6 col 2: goXero '0.00' vs Xero '-'
  row 6 col 3: goXero '0.00' vs Xero '-'
  row 6 col 4: goXero '0.00' vs Xero '-'
  row 6 col 6: goXero '0.00' vs Xero '-'
  row 7 col 2: goXero '0.00' vs Xero '-'
  row 7 col 3: goXero '0.00' vs Xero '-'
  row 7 col 4: goXero '0.00' vs Xero '-'
  row 7 col 5: goXero '6187.50' vs Xero '6,187.50'
  row 7 col 6: goXero '0.00' vs Xero '-'
  row 7 col 7: goXero '6187.50' vs Xero '6,187.50'
  row 8 col 2: goXero '0.00' vs Xero '-'
  row 8 col 3: goXero '0.00' vs Xero '-'
  row 8 col 4: goXero '0.00' vs Xero '-'
  row 8 col 5: goXero '8435.68' vs Xero '8,435.68'
  row 8 col 7: goXero '9194.51' vs Xero '9,194.51'
  row 9 col 2: goXero '0.00%' vs Xero '-'
  row 9 col 3: goXero '0.00%' vs Xero '-'
  row 9 col 4: goXero '0.00%' vs Xero '-'
```

### Aged Payables Summary

```sh
$ python3 /tmp/gx-aged-flatten.py /tmp/gx-live-ap.json docs/xero-reference/aged-payables-summary.txt
```

```text
goXero (flattened payload)                                      | Xero (reference export)
----------------------------------------------------------------|----------------------------------------
  Contact	< 1 Month	1 Month	2 Months	3 Months	Older	Total       | Contact	< 1 Month	1 Month	2 Months	3 Months	Older	Total
  Aged Payables                                                 | Aged Payables
!!Bayside Club	0.00	0.00	0.00	130.00	0.00	130.00                | Bayside Club	-	-	-	130.00	-	130.00
!!Bayside Wholesale	0.00	0.00	0.00	840.00	0.00	840.00           | Bayside Wholesale	-	-	-	840.00	-	840.00
!!Capital Cab Co	0.00	0.00	0.00	242.00	0.00	242.00              | Capital Cab Co	-	-	-	242.00	-	242.00
!!Central Copiers	0.00	0.00	0.00	0.00	163.56	163.56             | Central Copiers	-	-	-	-	163.56	163.56
!!Net Connect	0.00	0.00	0.00	54.13	0.00	54.13                   | Net Connect	-	-	-	54.13	-	54.13
!!PC Complete	0.00	0.00	2132.51	0.00	0.00	2132.51               | PC Complete	-	-	2,132.51	-	-	2,132.51
!!PowerDirect	0.00	0.00	0.00	108.60	0.00	108.60                 | PowerDirect	-	-	-	108.60	-	108.60
!!SMART Agency	0.00	0.00	0.00	2500.00	2000.00	4500.00           | SMART Agency	-	-	-	2,500.00	2,000.00	4,500.00
!!Swanston Security	0.00	0.00	0.00	0.00	59.54	59.54             | Swanston Security	-	-	-	-	59.54	59.54
!!Xero	0.00	0.00	0.00	31.39	0.00	31.39                          | Xero	-	-	-	31.39	-	31.39
!!Young Bros Transport	0.00	0.00	0.00	125.03	0.00	125.03        | Young Bros Transport	-	-	-	125.03	-	125.03
!!Total Aged Payables	0.00	0.00	2132.51	4031.15	2223.10	8386.76 | Total Aged Payables	-	-	2,132.51	4,031.15	2,223.10	8,386.76
  Expense Claims                                                | Expense Claims
!!Xero Demo	0.00	0.00	0.00	115.95	0.00	115.95                   | Adam Michkevich	-	-	-	115.95	-	115.95
!!Total Expense Claims	0.00	0.00	0.00	115.95	0.00	115.95        | Total Expense Claims	-	-	-	115.95	-	115.95
!!Total	0.00	0.00	2132.51	4147.10	2223.10	8502.71               | Total	-	-	2,132.51	4,147.10	2,223.10	8,502.71
!!Percentage of total	0.00%	0.00%	25.08%	48.77%	26.15%	100.00%  | Percentage of total	-	-	25.08%	48.77%	26.15%	100.00%

71 differing cell(s) of 19 row(s) compared
  row 3 col 2: goXero '0.00' vs Xero '-'
  row 3 col 3: goXero '0.00' vs Xero '-'
  row 3 col 4: goXero '0.00' vs Xero '-'
  row 3 col 6: goXero '0.00' vs Xero '-'
  row 4 col 2: goXero '0.00' vs Xero '-'
  row 4 col 3: goXero '0.00' vs Xero '-'
  row 4 col 4: goXero '0.00' vs Xero '-'
  row 4 col 6: goXero '0.00' vs Xero '-'
  row 5 col 2: goXero '0.00' vs Xero '-'
  row 5 col 3: goXero '0.00' vs Xero '-'
  row 5 col 4: goXero '0.00' vs Xero '-'
  row 5 col 6: goXero '0.00' vs Xero '-'
  row 6 col 2: goXero '0.00' vs Xero '-'
  row 6 col 3: goXero '0.00' vs Xero '-'
  row 6 col 4: goXero '0.00' vs Xero '-'
  row 6 col 5: goXero '0.00' vs Xero '-'
  row 7 col 2: goXero '0.00' vs Xero '-'
  row 7 col 3: goXero '0.00' vs Xero '-'
  row 7 col 4: goXero '0.00' vs Xero '-'
  row 7 col 6: goXero '0.00' vs Xero '-'
  row 8 col 2: goXero '0.00' vs Xero '-'
  row 8 col 3: goXero '0.00' vs Xero '-'
  row 8 col 4: goXero '2132.51' vs Xero '2,132.51'
  row 8 col 5: goXero '0.00' vs Xero '-'
  row 8 col 6: goXero '0.00' vs Xero '-'
  row 8 col 7: goXero '2132.51' vs Xero '2,132.51'
  row 9 col 2: goXero '0.00' vs Xero '-'
  row 9 col 3: goXero '0.00' vs Xero '-'
  row 9 col 4: goXero '0.00' vs Xero '-'
  row 9 col 6: goXero '0.00' vs Xero '-'
  row 10 col 2: goXero '0.00' vs Xero '-'
  row 10 col 3: goXero '0.00' vs Xero '-'
  row 10 col 4: goXero '0.00' vs Xero '-'
  row 10 col 5: goXero '2500.00' vs Xero '2,500.00'
  row 10 col 6: goXero '2000.00' vs Xero '2,000.00'
  row 10 col 7: goXero '4500.00' vs Xero '4,500.00'
  row 11 col 2: goXero '0.00' vs Xero '-'
  row 11 col 3: goXero '0.00' vs Xero '-'
  row 11 col 4: goXero '0.00' vs Xero '-'
  row 11 col 5: goXero '0.00' vs Xero '-'
  row 12 col 2: goXero '0.00' vs Xero '-'
  row 12 col 3: goXero '0.00' vs Xero '-'
  row 12 col 4: goXero '0.00' vs Xero '-'
  row 12 col 6: goXero '0.00' vs Xero '-'
  row 13 col 2: goXero '0.00' vs Xero '-'
  row 13 col 3: goXero '0.00' vs Xero '-'
  row 13 col 4: goXero '0.00' vs Xero '-'
  row 13 col 6: goXero '0.00' vs Xero '-'
  row 14 col 2: goXero '0.00' vs Xero '-'
  row 14 col 3: goXero '0.00' vs Xero '-'
  row 14 col 4: goXero '2132.51' vs Xero '2,132.51'
  row 14 col 5: goXero '4031.15' vs Xero '4,031.15'
  row 14 col 6: goXero '2223.10' vs Xero '2,223.10'
  row 14 col 7: goXero '8386.76' vs Xero '8,386.76'
  row 16 col 1: goXero 'Xero Demo' vs Xero 'Adam Michkevich'
  row 16 col 2: goXero '0.00' vs Xero '-'
  row 16 col 3: goXero '0.00' vs Xero '-'
  row 16 col 4: goXero '0.00' vs Xero '-'
  row 16 col 6: goXero '0.00' vs Xero '-'
  row 17 col 2: goXero '0.00' vs Xero '-'
  row 17 col 3: goXero '0.00' vs Xero '-'
  row 17 col 4: goXero '0.00' vs Xero '-'
  row 17 col 6: goXero '0.00' vs Xero '-'
  row 18 col 2: goXero '0.00' vs Xero '-'
  row 18 col 3: goXero '0.00' vs Xero '-'
  row 18 col 4: goXero '2132.51' vs Xero '2,132.51'
  row 18 col 5: goXero '4147.10' vs Xero '4,147.10'
  row 18 col 6: goXero '2223.10' vs Xero '2,223.10'
  row 18 col 7: goXero '8502.71' vs Xero '8,502.71'
  row 19 col 2: goXero '0.00%' vs Xero '-'
  row 19 col 3: goXero '0.00%' vs Xero '-'
```

### The differing cells, each with its cause

Column numbers below are 1-based, `row N col M` as the flattener numbers them.

**Aged Receivables — 34 differing cells of 9 rows, none of them a value.**

| cells | goXero | Xero | cause |
|---|---|---|---|
| 26 cells | `0.00` | `-` | a nil is printed as zero. The report prints money through the package's one `money()` helper (`0.00`), Xero's export prints `-` for a nil. `0.00` and `-` are the same value; see the decision below. |
| 5 cells | `1191.83`, `6187.50` (twice), `8435.68`, `9194.51` | `1,191.83`, `6,187.50`, `8,435.68`, `9,194.51` | thousands separators. `money()` groups nothing; Xero's export groups thousands. Same value. |
| 3 cells (row 9, cols 2-4) | `0.00%` | `-` | the Percentage of total row over the three empty columns: a share of nothing is nil, and `0.00%` is how this renderer prints nil in that row. Same value. |

**Aged Payables — 71 differing cells of 19 rows: the 70 above, plus one that is
not a value at all.**

| cells | goXero | Xero | cause |
|---|---|---|---|
| 55 cells | `0.00` | `-` | as above |
| 13 cells | `2132.51` (twice), `2500.00`, `2000.00`, `4500.00`, `4031.15`, `2223.10`, `8386.76`, `4147.10`, `8502.71` | grouped | as above |
| 2 cells (row 19, cols 2-3) | `0.00%` | `-` | as above |
| row 16 col 1 | `Xero Demo` | `Adam Michkevich` | **the claimant's name.** The capture names a person; the database names a user. They are different names and neither is derivable from the other — §5. No amount moves with it: the row's five amount columns and its total are the capture's own. |

The `0.00`-for-nil choice was made deliberately and reversed once during this
work. Rendering `-` would have made the two tables identical as printed, but it
would have put a figure's typography at odds with every other report in the
product (`money()` is used by the Trial Balance, the P&L, the Cash Summary, and
before this change by the aged reports themselves), and — decisively — it would
have broken the front end's alignment rule without touching it:
`web/src/lib/components/ReportView.svelte` decides a column is an amount column
only when *every* non-empty cell in it matches `/^-?\d[\d,]*(\.\d+)?$/`,
which a dash does not match, so a column of dashes flips from right- to
left-aligned, and would flip back the moment the data changed. A figure that
prints one way in one report and another way in the next is a worse defect than a
report that differs from a CSV export in its typography.

### The values, compared after normalising the presentation

The same two files, compared by value rather than as printed: the comparator
reads the `-` and the `0.00` as the same nil, strips the separators, keeps the
percentages as percentages and rounds to the cent (`/tmp/gx-aged-compare.py`,
appendix B).

```sh
$ python3 /tmp/gx-aged-compare.py /tmp/gx-live-ar.json docs/xero-reference/aged-receivables-summary.txt
```

```text
row  1 Contact                          VALUES MATCH
row  2 Basket Case                      VALUES MATCH
row  3 Bayside Club                     VALUES MATCH
row  4 City Limousines                  VALUES MATCH
row  5 DIISR - Small Business Services  VALUES MATCH
row  6 Marine Systems                   VALUES MATCH
row  7 Ridgeway University              VALUES MATCH
row  8 Total                            VALUES MATCH
row  9 Percentage of total              VALUES MATCH

rows compared: 9
cells identical as printed: 29
cells different in spelling but equal in value: 34
cells different in value: 0
```

```sh
$ python3 /tmp/gx-aged-compare.py /tmp/gx-live-ap.json docs/xero-reference/aged-payables-summary.txt
```

```text
row  1 Contact                          VALUES MATCH
row  2 Aged Payables                    VALUES MATCH
row  3 Bayside Club                     VALUES MATCH
row  4 Bayside Wholesale                VALUES MATCH
row  5 Capital Cab Co                   VALUES MATCH
row  6 Central Copiers                  VALUES MATCH
row  7 Net Connect                      VALUES MATCH
row  8 PC Complete                      VALUES MATCH
row  9 PowerDirect                      VALUES MATCH
row 10 SMART Agency                     VALUES MATCH
row 11 Swanston Security                VALUES MATCH
row 12 Xero                             VALUES MATCH
row 13 Young Bros Transport             VALUES MATCH
row 14 Total Aged Payables              VALUES MATCH
row 15 Expense Claims                   VALUES MATCH
row 16 Xero Demo                        DIFFERS
row 17 Total Expense Claims             VALUES MATCH
row 18 Total                            VALUES MATCH
row 19 Percentage of total              VALUES MATCH

rows compared: 19
cells identical as printed: 50
cells different in spelling but equal in value: 70
cells different in value: 1
   (16, 'Xero Demo', 'col 1', 'Xero Demo', 'Adam Michkevich')
```

**Aged Receivables: every value in the capture, including both published
percentages, is reproduced.** **Aged Payables: every value is reproduced except
one—the claimant's name.** Every published column total, both subtotals and the
report total match to the cent: `Total Aged Payables 8,386.76`, `Total Expense
Claims 115.95`, `Total 8,502.71`, and `Percentage of total - - 25.08% 48.77%
26.15% 100.00%`.

## 3. The `Percentage of total` row

Present in the two summaries, absent from the two by-contact reports. No captured
Xero report exists for the by-contact variants (there is no
`docs/xero-reference/aged-*-by-contact*` file), so nothing is added there that a
capture does not show — and the by-contact report is not a summary, so a share of
a total would be measuring the wrong whole.

The row is computed from the amounts the `Total` row above it printed, never from
a second query:

```go
den := amounts[len(amounts)-1]      // the Total column of the row above
if den.IsZero() { /* the share of nothing: every cell empty */ }
for _, a := range amounts {
        pct := a.Div(den).Mul(decimal.NewFromInt(100))
        row.Cells = append(row.Cells, txt(pct.StringFixed(2)+"%"))
}
```

The arithmetic the captures show, to four places, and as printed:

* Aged Receivables, denominator 9,194.51 — `3 Months` 8,435.68 / 9,194.51 =
  91.7469% → `91.75%`; `Older` 758.83 / 9,194.51 = 8.2531% → `8.25%`; `Total`
  9,194.51 / 9,194.51 → `100.00%`.
* Aged Payables, denominator 8,502.71 — `2 Months` 2,132.51 / 8,502.71 = 25.0804%
  → `25.08%`; `3 Months` 4,147.10 / 8,502.71 = 48.7739% → `48.77%`; `Older`
  2,223.10 / 8,502.71 = 26.1458% → `26.15%`; `Total` → `100.00%`.

Note that the denominator is the report's *own* total, so on the payables report
it includes the expense claims: 4,031.15 of trade payables in `3 Months` is 47.41%
of 8,502.71, and the capture's 48.77% is that 4,031.15 plus the 115.95 claim,
4,147.10. A renderer that divided by `Total Aged Payables` would print a row that
sums to more than 100%.

Over an empty report the denominator is nil, so the row is printed with empty
cells rather than a row of zeroes or a division by zero: `TestHTTP_Reports_AgedEmptyOrganisationIsHonest`
asserts exactly that, cell by cell down the row.

**What this does to the front end, reported not fixed.** Those cells end in `%`,
which the `AMOUNT` regex in `ReportView.svelte` does not match, and the shape
computation there is a conjunction over every `Row` and `SummaryRow` cell in a
column. So on the two summaries, adding the percentage row flips all seven
columns to left alignment; the by-contact reports, which have no percentage row,
keep right-aligned amounts. Dropping the `%` sign would keep the alignment and is
the wrong fix — a bare `91.75` in an amount column reads as money. The alignment
rule belongs to `web/**`, which this task must not edit; the front end's owner
should teach it that a percentage is an amount-shaped cell (or skip the
percentage row) rather than have the backend print something the capture does not
show.

## 4. Which boundaries the captures demonstrate, and which are inferred

The reference dataset puts an invoice in only two of the five columns, and never
near a boundary. Every outstanding document at 31 December 2026, by days past due
(appendix A, query 3 of `gx-aged-brackets.sql` and `gx-aged-edges.sql`):

```text
=== Where the reference dataset sits relative to
unterminated quoted string
  type  | min_days | max_days | invoices 
--------+----------+----------+----------
 ACCPAY |       81 |      160 |       12
 ACCREC |       93 |      164 |        9
(2 rows)

=== The day offsets present, by Xero column
  type  | column_name | min_days | max_days | invoices |  amount   
--------+-------------+----------+----------+----------+-----------
 ACCPAY | 2 Months    |       81 |       81 |        1 | 2132.5100
 ACCPAY | 3 Months    |      101 |      115 |        8 | 4031.1500
 ACCPAY | Older       |      125 |      160 |        3 | 2223.1000
 ACCREC | 3 Months    |       93 |      112 |        5 | 8435.6800
 ACCREC | Older       |      142 |      164 |        4 |  758.8300
(5 rows)

  type  |             contact             |    doc    | days_past_due | amount_due 
--------+---------------------------------+-----------+---------------+------------
 ACCPAY | PC Complete                     |           |            81 |  2132.5100
 ACCPAY | Net Connect                     | Rpt       |           101 |    54.1300
 ACCPAY | Young Bros Transport            |           |           102 |   125.0300
 ACCPAY | PowerDirect                     | Rpt       |           102 |   108.6000
 ACCPAY | Bayside Wholesale               | GB1-White |           103 |   840.0000
 ACCPAY | Capital Cab Co                  | CS815     |           105 |   242.0000
 ACCPAY | Bayside Club                    |           |           107 |   130.0000
 ACCPAY | SMART Agency                    | SM0210    |           112 |  2500.0000
 ACCPAY | Xero                            | AP        |           115 |    31.3900
 ACCPAY | Swanston Security               | AP        |           125 |    59.5400
 ACCPAY | SMART Agency                    | SM0195    |           152 |  2000.0000
 ACCPAY | Central Copiers                 | 945-OCon  |           160 |   163.5600
 ACCREC | Bayside Club                    | INV-0028  |            93 |   234.0000
 ACCREC | City Limousines                 | INV-0024  |           102 |   703.6300
 ACCREC | Basket Case                     | INV-0026  |           102 |   914.5500
 ACCREC | Marine Systems                  | INV-0027  |           106 |   396.0000
 ACCREC | Ridgeway University             | INV-0025  |           112 |  6187.5000
 ACCREC | DIISR - Small Business Services | INV-0016  |           142 |   270.6300
 ACCREC | City Limousines                 | INV-0017  |           144 |    21.7000
 ACCREC | City Limousines                 | INV-0012  |           147 |   216.5000
 ACCREC | City Limousines                 | INV-0006  |           164 |   250.0000
(21 rows)
```

**Demonstrated by the data** (a row sits in the column, and the capture's own
column total confirms it):

* `2 Months` holds 81 days past due (PC Complete, 2,132.51 — the whole of that
  column).
* `3 Months` holds 93, 101, 102, 102, 103, 105, 106, 107, 112, 112 and 115 days
  (AR 8,435.68; AP 4,031.15; the claim's 115.95 is the 112-day row).
* `Older` holds 125, 142, 144, 147, 152, 160 and 164 days (AP 2,223.10; AR
  758.83).
* Both boundaries in play are therefore *bracketed*: the `2 Months`/`3 Months`
  edge lies in 82..101, the `3 Months`/`Older` edge in 116..125.

**Inferred, not demonstrated** (no row in the capture is anywhere near them):

* The exact edge days `30/31`, `60/61`, `90/91`, `120/121`. What the data
  constrains is only the brackets above; the *values* come from the headings —
  `< 1 Month`, `1 Month`, `2 Months`, `3 Months` name whole months past due, and
  the implementation reads a month as 30 days.
* The `≤ 30` arm's role as the not-yet-due column. **No row in either capture is
  not yet due at the as-at date**, so the fold of "not yet due" into `< 1 Month`
  is inferred from the heading (there is no `Current`, and Xero's first column
  starts below a month) and from nothing else. This is the inference that cannot
  be checked against the captures at all, which is why the test suite pins it
  with a fixture of its own (§7).
* That a month is 30 days rather than a calendar month. On these rows the two
  readings agree (no row is within 20 days of an edge), so the dataset cannot
  distinguish them; the 30-day reading is the one implemented and the one the
  tests pin, and no cell of either capture contradicts it.

**How a wrong inference would show up.** Which cell moves, and by how much:

| wrong reading | cells that move | |
|---|---|---|
| `2 Months` ends at 80 (edge in the low half of its bracket) | AP: `2 Months` 2,132.51 → **0.00**, `3 Months` 4,031.15 → **6,163.66**; `Percentage of total` `2 Months` 25.08% → **0.00%**, `3 Months` 48.77% → **73.85%** | PC Complete's invoice is the one in 81..101; the totals do not move because it stays inside the payables block. The capture's `2 Months` column is the cell that catches it. |
| `3 Months` ends at 114 (edge in the low half of its bracket) | AP: `3 Months` 4,031.15 → **3,999.76**, `Older` 2,223.10 → **2,254.49**; `Percentage of total` `3 Months` 48.77% → **48.40%**, `Older` 26.15% → **26.51%** | the 115-day invoice (Xero, 31.39) is the edge row; six cells move, no total does. |
| `Older` begins above 164, or `2 Months` begins above 101 | an entire column empties or fills: `Older` 2,223.10 → 0.00 and `3 Months` → 6,254.25 on the payables capture, `Older` 26.15% → 0.00% and `3 Months` → 73.85% | the capture's own column totals catch it immediately. |
| not-yet-due given a column of its own, or dropped | **nothing in either capture moves** — no row is not yet due. On the test fixture's own organisation it is visible: `< 1 Month` holds 10.00 (1.00 not yet due + 2.00 due today + 3.00 one day past + 4.00 thirty days past) and would hold 9.00, with the 1.00 in a sixth column or gone from the report altogether. | the reason the fixture exists: the captures cannot see this defect at all. |

## 5. The Expense Claims block, and the claimant's name

Xero's `Total 8,502.71` is not trade payables. It is `Total Aged Payables
8,386.76` plus an `Expense Claims` block: `Total Expense Claims 115.95`, one row
per claimant, and the report's `Percentage of total` row is taken against the
combined figure. An unpaid expense claim is a payable — the money sits in the
organisation's Unpaid Expense Claims account until it is reimbursed — so the
payables report carries it and the receivables report does not.

`internal/repository/report.go` gains `AgedExpenseClaims`, and
`internal/handlers/report.go` calls it for `AgedPayables` only:

```sql
SELECT COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.email, $3) AS claimant,
       COALESCE(SUM(CASE WHEN $2::date - COALESCE(ec.payment_due_date, ec.reporting_date) <= 30 THEN ec.amount_due END),0) AS lt1month,
       ... the same five arms as Aged ...
       COALESCE(SUM(ec.amount_due),0) AS total
  FROM expense_claims ec LEFT JOIN users u ON u.user_id = ec.user_id
 WHERE ec.organisation_id=$1 AND ec.status IN ('SUBMITTED','AUTHORISED') AND ec.amount_due > 0
   AND COALESCE(ec.reporting_date, ec.payment_due_date) <= $2::date
 GROUP BY u.user_id, u.first_name, u.last_name, u.email ORDER BY claimant
```

**What the block is, and is not.** `WHERE` keeps the claims Xero keeps: unpaid
claims (`SUBMITTED`, `AUTHORISED`) with something still owing (`amount_due > 0`).
In the reference dataset that is one row of three — the two `PAID` claims
(Orlena Greenville, 29.50 and 34.90, both `amount_due` 0) are excluded, and the
capture has no row for either, which is the cross-check that the filter matches
Xero's own (appendix A, query 4). The inclusion date is the document's own date
(`reporting_date`, falling back to the payment due date) and the ageing key is
the due date (`payment_due_date`, falling back to the reporting date) — the same
split the invoice query uses (`i.date` includes, `COALESCE(i.due_date, i.date)`
ages). On the reference data the two coincide: `payment_due_date` is `NULL` for
every claim, as the CSV carries no due date, so the claim ages from 10 September
2026 and lands at 112 days, in `3 Months`, where the capture puts it.

**The claimant's name.** Xero's capture says `Adam Michkevich`. The other two
captured files that describe the same claim disagree with it: the harvested row
`migrations/data/xero/expense-claims.csv` gives the claim's `contact` as
`Xero Demo`, and the general-ledger detail capture for the same claim carries the
narration `expense-claim/Xero Demo/2026-09-10`. The renderer prints
`Xero Demo`, because that is the name the database holds: the label is built from
`expense_claims.user_id → users` (`first_name` + `last_name`, else the email,
else `Unassigned expense claim` when the claim is recorded against nobody) — the
only claimant record the schema keeps, and the only one the imported data can
populate. The decision rule, stated plainly:

* It never prints a name that appears in no captured file **and** in no row of the
  organisation's own data. `Adam Michkevich` appears in exactly one captured file
  and in no table, so it is not printable from this schema without typing a
  literal into the renderer; `Xero Demo` appears in two captured files and in
  `users`, so it is.
* When the two captures disagree there is no rule that satisfies both, and a
  report that took the AP summary's name over the data's would be reporting a
  claim against a person who does not exist in the organisation.
* The name is not the amount: the row's five amount columns, its total and the
  report's totals are the capture's own, which the comparison above shows.

**The migration that makes it live.** When this task began,
`migrations/00024_xero_reference_source_documents.sql` imported the claim's
*ledger* line (the `801` Unpaid Expense Claims posting of 115.95) but no
`expense_claims` row, so the live report's block was empty and its total read
8,386.76 against Xero's 8,502.71 — the block was correct and had nothing to show.
00024 was extended during this task (another worker's file — not touched here) to
import the claims and their claimants, and the block now renders with the
capture's own amount and column. That is the state both comparisons above were
taken in.

**A receivables report gets no second block.** There is no captured receivables
report with an expense-claims section, and there could not be one: a claim is
money owed, not money owing. `renderAged` adds the block only when it has rows
*and* only for `AgedPayables`, and `TestHTTP_Reports_AgedReceivablesHasNoExpenseClaimsBlock`
files a claim and asserts it appears on the payables report and nowhere on the
receivables one.

## 6. Budget Summary

`GET /api/v1/reports/budget-summary` used to answer with the header
`["Account","Budget"]` and a single `Total | 0.00` row. There is no budget table
in `migrations/`, so that total was the sum of nothing, printed in the one place
a reader would take for a measurement. It now answers with the same envelope, the
same `ReportID`/`ReportName`/`ReportDate`, the same header row — and no data row
at all, with the gap stated where Xero states what a report cannot show: a fourth
`ReportTitles` entry, the way the Cash Summary carries its own.

```text
$ curl -s -H "Authorization: Bearer $T" -H "$H" \
      "http://localhost:8099/api/v1/reports/budget-summary?date=2026-12-31"
ReportID:   BudgetSummary
ReportName: Budget Summary
ReportDate: 31 December 2026
ReportTitles:
  [0] Budget Summary
  [1] Demo Company (Global)
  [2] For the year to 31 December 2026
  [3] No budget is stored: this organisation has no budget in goXero (there is no budget
      table in the schema), so there is no budget figure to show and none to measure
      actuals against. Xero returns the same empty report for an organisation with no budget set.
Rows: Header | Account | Budget
```

The endpoint, the `BudgetSummary` entry in `GET /api/v1/reports` and the route
are all unchanged: a client that asks for the report gets the report, and one
that asks the index for the catalogue of reports still finds it.

**Should the web catalogue keep linking it?** Reported, not edited — `web/**` is
out of scope for this task. `web/src/lib/reports-catalog.ts` lists three budget
entries:

```ts
		{
			label: 'Budget Manager',
			// No budget editor exists: `migrations/` has no budget table, so
			// there is nothing to create a budget with. The row keeps Xero's
			// own label and description, and is marked unavailable rather
			// than pointing at the Budget Summary, which can only ever show
			// the empty report. Point it at the editor the day one lands.
			href: null,
			description:
				'Create budgets to monitor business performance against your goals.'
		},
		{
			label: 'Budget Summary',
			href: '/app/reports/budget-summary',
			description: "View the budgets you've created in Budget Manager."
		},
		{
			label: 'Budget Variance',
			href: null,
			description:
				'Compare your actual figures with budgeted figures to track business performance.'
		},
```

`Budget Manager` and `Budget Variance` are already marked unavailable with the
comment *"No budget editor exists: `migrations/` has no budget table, so there is
nothing to create a budget with"* — the same reason this endpoint can only ever
return the empty report. `Budget Summary` alone still links, and its description
— *"View the budgets you've created in Budget Manager"* — is **not true today**:
there is no Budget Manager to create a budget in, so there are no budgets to
view, and a user who follows the link reads a caveat rather than a budget. The
honest options for the front end are the ones it already applies to its two
sibling rows (mark it unavailable with the same comment) or reword the
description to say it reports that no budget is stored. That is a decision for
the front-end owner; this report's job is to say the claim is no longer accurate
and to leave the file alone. The backend keeps serving the report either way —
the endpoint is honest now, and the link's honesty is a separate matter from
whether the report exists.

## 7. Tests

`internal/handlers/reports_aged_xero_integration_test.go` (new, 8 tests). No
expectation is a figure typed into a test: the columns are read out of the
capture's own heading line, the claims fixture is built from the amount and the
column the capture puts its claim in, the claimant label is read out of `users`
with the renderer's own precedence chain, the totals are read back out of
`invoices`/`expense_claims`, and the percentages are recomputed from the rows the
report printed.

| test | what it pins |
|---|---|
| `TestHTTP_Reports_AgedColumnsAreXerosOwn` | the seven headings of all four aged endpoints equal the capture's, and that the two captures agree; no `Current` column |
| `TestHTTP_Reports_AgedBucketsFollowXerosBoundaries` | one invoice at −20, 0, 1, 30, 31, 60, 61, 90, 91, 120 and 121 days past due lands in the column its age names, the five columns partition the row, the total agrees with the ledger, and the drill-down adds back to the summary |
| `TestHTTP_Reports_AgedPercentageRowIsTheRenderedTotal` | every percentage is its rendered column over the rendered total, the total column is `100.00%`, and the by-contact reports have no such row |
| `TestHTTP_Reports_AgedEmptyOrganisationIsHonest` | an organisation with nothing renders the headings, no section, no contact row, and an empty percentage row |
| `TestHTTP_Reports_AgedPayablesExpenseClaimsBlock` | the block exists with the capture's amount in the capture's column, its subtotal is its rows, `Total` is both blocks column by column, and the percentage row divides by that combined total |
| `TestHTTP_Reports_AgedReceivablesHasNoExpenseClaimsBlock` | a filed claim appears on the payables report and never on the receivables one, whose total stays its contact rows |
| `TestHTTP_Reports_AgedByContactIsNotTheSummary` | the drill-down lists invoices inside per-contact sections with their own subtotals, the summary lists contacts, and both end at the same total |
| `TestHTTP_Reports_BudgetSummaryStatesNoBudgetIsStored` | the report keeps the index's own identity and path, states the gap in `ReportTitles[3]`, prints no `Total` row, and prints no cell that could be read as a figure |

One existing comment block was corrected: `reports_parity_integration_test.go`
described the fixture in the old vocabulary (`1-30`, `Current`, `> 90`), which no
longer names a column. Its assertions were already column-agnostic (they sum
columns 1..5) and did not change.

## 8. Build, format, the whole suite

```sh
$ go build ./...
$ gofmt -l .
$ go test ./internal/... -count=1
```

`go build ./...` produced no output (success); `gofmt -l .` listed no file; the
suite:

```text
ok  	github.com/shurco/goxero/internal/bankcoding	0.231s
ok  	github.com/shurco/goxero/internal/bankfeed	0.398s
ok  	github.com/shurco/goxero/internal/bankrules	0.563s
ok  	github.com/shurco/goxero/internal/bankstatement	0.895s
ok  	github.com/shurco/goxero/internal/config	0.693s
ok  	github.com/shurco/goxero/internal/database	1.124s
ok  	github.com/shurco/goxero/internal/handlers	7.452s
ok  	github.com/shurco/goxero/internal/logger	0.249s
ok  	github.com/shurco/goxero/internal/middleware	1.543s
ok  	github.com/shurco/goxero/internal/models	0.826s
ok  	github.com/shurco/goxero/internal/repository	2.417s
?   	github.com/shurco/goxero/internal/router	[no test files]
?   	github.com/shurco/goxero/internal/testutil	[no test files]
```

(The fibre request logs each test emits are dropped above; nothing else is.)

## 9. Files changed

* `internal/repository/report.go` — `AgedRow`/`AgedDetailRow` carry the five
  Xero columns; `Aged`/`AgedByContact` age with Xero's boundaries; new
  `AgedClaimRow`, `ClaimantUnassigned` and `AgedExpenseClaims`.
* `internal/handlers/report_render.go` — `renderAged` builds Xero's payables
  blocks (Aged Payables / Expense Claims / Total) and the Percentage of total
  row; `renderAgedByContact` names the same columns and adds no percentage row;
  `renderBudgetSummary` prints no total and states the gap;
  `agedCells`/`agedRow`/`agedSummaryRow`/`agedPercentRow`/`agedAdd`/`agedAmountsOf`/`agedClaimAmountsOf`
  added.
* `internal/handlers/report.go` — the aged handlers pass the claims the renderer
  needs (`AgedPayables` only); doc comments on both aged handlers and the Budget
  Summary handler.
* `internal/handlers/reports_aged_xero_integration_test.go` — new.
* `internal/handlers/reports_parity_integration_test.go` — three comments naming
  the old buckets.
* `docs/xero-aged-parity.md` — this file.

## Appendix A — the SQL behind every number

Run as `docker exec -i bonefish-postgres-1 psql -U goxero -d goxero < /tmp/gx-aged-evidence.sql`,
with `PAGER=off`. The first three queries are the repository's own arithmetic on
the reference data; the last three are the arithmetic of the percentage row.

```sql
\echo == 1. Aged Payables Summary: the trade-payable columns (internal/repository/report.go::Aged)
SELECT
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) <= 30              THEN amount_due END),0) AS "< 1 Month",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 31 AND 60  THEN amount_due END),0) AS "1 Month",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 61 AND 90  THEN amount_due END),0) AS "2 Months",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 91 AND 120 THEN amount_due END),0) AS "3 Months",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) > 120              THEN amount_due END),0) AS "Older",
  COALESCE(SUM(amount_due),0) AS "Total Aged Payables"
FROM invoices
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  AND type = 'ACCPAY' AND status = 'AUTHORISED' AND amount_due > 0 AND date <= DATE '2026-12-31';

\echo == 2. Aged Receivables Summary: the same for ACCREC
SELECT
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) <= 30              THEN amount_due END),0) AS "< 1 Month",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 31 AND 60  THEN amount_due END),0) AS "1 Month",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 61 AND 90  THEN amount_due END),0) AS "2 Months",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 91 AND 120 THEN amount_due END),0) AS "3 Months",
  COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) > 120              THEN amount_due END),0) AS "Older",
  COALESCE(SUM(amount_due),0) AS "Total"
FROM invoices
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  AND type = 'ACCREC' AND status = 'AUTHORISED' AND amount_due > 0 AND date <= DATE '2026-12-31';

\echo == 3. The Expense Claims block (internal/repository/report.go::AgedExpenseClaims)
SELECT COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.email,
                'Unassigned expense claim') AS claimant,
       COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(ec.payment_due_date, ec.reporting_date) <= 30              THEN ec.amount_due END),0) AS "< 1 Month",
       COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(ec.payment_due_date, ec.reporting_date) BETWEEN 31 AND 60  THEN ec.amount_due END),0) AS "1 Month",
       COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(ec.payment_due_date, ec.reporting_date) BETWEEN 61 AND 90  THEN ec.amount_due END),0) AS "2 Months",
       COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(ec.payment_due_date, ec.reporting_date) BETWEEN 91 AND 120 THEN ec.amount_due END),0) AS "3 Months",
       COALESCE(SUM(CASE WHEN DATE '2026-12-31' - COALESCE(ec.payment_due_date, ec.reporting_date) > 120              THEN ec.amount_due END),0) AS "Older",
       COALESCE(SUM(ec.amount_due),0) AS "Total Expense Claims"
FROM expense_claims ec LEFT JOIN users u ON u.user_id = ec.user_id
WHERE ec.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  AND ec.status IN ('SUBMITTED','AUTHORISED') AND ec.amount_due > 0
  AND COALESCE(ec.reporting_date, ec.payment_due_date) <= DATE '2026-12-31'
GROUP BY u.user_id, u.first_name, u.last_name, u.email ORDER BY claimant;

\echo == 4. Every claim in the organisation, including the ones the block excludes
SELECT u.first_name || ' ' || COALESCE(u.last_name,'') AS claimant, ec.status, ec.reporting_date,
       ec.amount_due, DATE '2026-12-31' - coalesce(ec.payment_due_date, ec.reporting_date) AS days_past_due
FROM expense_claims ec LEFT JOIN users u ON u.user_id = ec.user_id
WHERE ec.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
ORDER BY claimant;

\echo == 5. Percentage of total: each column of the AP report divided by the report Total
SELECT column_name, amount, round(amount / 8502.71 * 100, 2) AS percent
FROM (VALUES ('< 1 Month', 0.00), ('1 Month', 0.00), ('2 Months', 2132.51),
             ('3 Months', 4147.10), ('Older', 2223.10), ('Total', 8502.71)) AS v(column_name, amount);

\echo == 6. Percentage of total: the AR report
SELECT column_name, amount, round(amount / 9194.51 * 100, 2) AS percent
FROM (VALUES ('< 1 Month', 0.00), ('1 Month', 0.00), ('2 Months', 0.00),
             ('3 Months', 8435.68), ('Older', 758.83), ('Total', 9194.51)) AS v(column_name, amount);
```

```text
== 1. Aged Payables Summary: the trade-payable columns (internal/repository/report.go::Aged)
 < 1 Month | 1 Month | 2 Months  | 3 Months  |   Older   | Total Aged Payables 
-----------+---------+-----------+-----------+-----------+---------------------
         0 |       0 | 2132.5100 | 4031.1500 | 2223.1000 |           8386.7600
(1 row)

== 2. Aged Receivables Summary: the same for ACCREC
 < 1 Month | 1 Month | 2 Months | 3 Months  |  Older   |   Total   
-----------+---------+----------+-----------+----------+-----------
         0 |       0 |        0 | 8435.6800 | 758.8300 | 9194.5100
(1 row)

== 3. The Expense Claims block (internal/repository/report.go::AgedExpenseClaims)
 claimant  | < 1 Month | 1 Month | 2 Months | 3 Months | Older | Total Expense Claims 
-----------+-----------+---------+----------+----------+-------+----------------------
 Xero Demo |         0 |       0 |        0 | 115.9500 |     0 |             115.9500
(1 row)

== 4. Every claim in the organisation, including the ones the block excludes
     claimant      |   status   | reporting_date | amount_due | days_past_due 
-------------------+------------+----------------+------------+---------------
 Orlena Greenville | PAID       | 2026-07-11     |     0.0000 |           173
 Orlena Greenville | PAID       | 2026-08-11     |     0.0000 |           142
 Xero Demo         | AUTHORISED | 2026-09-10     |   115.9500 |           112
(3 rows)

== 5. Percentage of total: each column of the AP report divided by the report Total
 column_name | amount  | percent 
-------------+---------+---------
 < 1 Month   |    0.00 |    0.00
 1 Month     |    0.00 |    0.00
 2 Months    | 2132.51 |   25.08
 3 Months    | 4147.10 |   48.77
 Older       | 2223.10 |   26.15
 Total       | 8502.71 |  100.00
(6 rows)

== 6. Percentage of total: the AR report
 column_name | amount  | percent 
-------------+---------+---------
 < 1 Month   |    0.00 |    0.00
 1 Month     |    0.00 |    0.00
 2 Months    |    0.00 |    0.00
 3 Months    | 8435.68 |   91.75
 Older       |  758.83 |    8.25
 Total       | 9194.51 |  100.00
(6 rows)
```

The bracket and edge queries quote the same `WHERE` clause the repository uses
(`status = 'AUTHORISED' AND amount_due > 0 AND date <= as-at`):

```sql
\echo === Where the reference dataset sits relative to Xero's boundaries
SELECT type,
       min(DATE '2026-12-31' - COALESCE(due_date, date)) AS min_days,
       max(DATE '2026-12-31' - COALESCE(due_date, date)) AS max_days,
       count(*) AS invoices
FROM invoices
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  AND status = 'AUTHORISED' AND amount_due > 0 AND date <= DATE '2026-12-31'
GROUP BY type;

\echo === The day offsets present, by Xero column
SELECT type,
       CASE WHEN DATE '2026-12-31' - COALESCE(due_date, date) <= 30              THEN '< 1 Month'
            WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 31 AND 60  THEN '1 Month'
            WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 61 AND 90  THEN '2 Months'
            WHEN DATE '2026-12-31' - COALESCE(due_date, date) BETWEEN 91 AND 120 THEN '3 Months'
            ELSE 'Older' END AS column_name,
       min(DATE '2026-12-31' - COALESCE(due_date, date)) AS min_days,
       max(DATE '2026-12-31' - COALESCE(due_date, date)) AS max_days,
       count(*) AS invoices, sum(amount_due) AS amount
FROM invoices
WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  AND status = 'AUTHORISED' AND amount_due > 0 AND date <= DATE '2026-12-31'
GROUP BY type, column_name ORDER BY type, min_days;

SELECT i.type, c.name AS contact, coalesce(i.invoice_number, i.reference, '') AS doc,
       DATE '2026-12-31' - COALESCE(i.due_date, i.date) AS days_past_due, i.amount_due
FROM invoices i JOIN contacts c ON c.contact_id = i.contact_id
WHERE i.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
  AND i.status = 'AUTHORISED' AND i.amount_due > 0 AND i.date <= DATE '2026-12-31'
ORDER BY i.type, days_past_due;
```

```text
=== Where the reference dataset sits relative to
unterminated quoted string
  type  | min_days | max_days | invoices 
--------+----------+----------+----------
 ACCPAY |       81 |      160 |       12
 ACCREC |       93 |      164 |        9
(2 rows)

=== The day offsets present, by Xero column
  type  | column_name | min_days | max_days | invoices |  amount   
--------+-------------+----------+----------+----------+-----------
 ACCPAY | 2 Months    |       81 |       81 |        1 | 2132.5100
 ACCPAY | 3 Months    |      101 |      115 |        8 | 4031.1500
 ACCPAY | Older       |      125 |      160 |        3 | 2223.1000
 ACCREC | 3 Months    |       93 |      112 |        5 | 8435.6800
 ACCREC | Older       |      142 |      164 |        4 |  758.8300
(5 rows)

  type  |             contact             |    doc    | days_past_due | amount_due 
--------+---------------------------------+-----------+---------------+------------
 ACCPAY | PC Complete                     |           |            81 |  2132.5100
 ACCPAY | Net Connect                     | Rpt       |           101 |    54.1300
 ACCPAY | Young Bros Transport            |           |           102 |   125.0300
 ACCPAY | PowerDirect                     | Rpt       |           102 |   108.6000
 ACCPAY | Bayside Wholesale               | GB1-White |           103 |   840.0000
 ACCPAY | Capital Cab Co                  | CS815     |           105 |   242.0000
 ACCPAY | Bayside Club                    |           |           107 |   130.0000
 ACCPAY | SMART Agency                    | SM0210    |           112 |  2500.0000
 ACCPAY | Xero                            | AP        |           115 |    31.3900
 ACCPAY | Swanston Security               | AP        |           125 |    59.5400
 ACCPAY | SMART Agency                    | SM0195    |           152 |  2000.0000
 ACCPAY | Central Copiers                 | 945-OCon  |           160 |   163.5600
 ACCREC | Bayside Club                    | INV-0028  |            93 |   234.0000
 ACCREC | City Limousines                 | INV-0024  |           102 |   703.6300
 ACCREC | Basket Case                     | INV-0026  |           102 |   914.5500
 ACCREC | Marine Systems                  | INV-0027  |           106 |   396.0000
 ACCREC | Ridgeway University             | INV-0025  |           112 |  6187.5000
 ACCREC | DIISR - Small Business Services | INV-0016  |           142 |   270.6300
 ACCREC | City Limousines                 | INV-0017  |           144 |    21.7000
 ACCREC | City Limousines                 | INV-0012  |           147 |   216.5000
 ACCREC | City Limousines                 | INV-0006  |           164 |   250.0000
(21 rows)
```

## Appendix B — the scratch scripts

Both are in `/tmp` for this session only; they are reproduced here so the two
comparisons can be rerun. `gx-aged-flatten.py` writes the side-by-side tables in
§2 and lists as-printed differences:

```python
"""Flatten a goXero Reports envelope and line it up against a Xero reference export.

Usage: gx-aged-flatten.py <payload.json> <reference.txt>

Both sides are reduced to tab-separated text lines. A JSON Section prints its
Title on its own line (Xero's export prints block headings the same way) and then
its rows. The reference file's own "-" cells are kept as they are: they are what
the side-by-side is measured against.
"""
import json, sys, re

def flatten(path):
    rep = json.load(open(path))["Reports"][0]
    out = []
    def walk(rows):
        for r in rows:
            if r["RowType"] == "Section":
                if r.get("Title"):
                    out.append(r["Title"])
                walk(r.get("Rows") or [])
            else:
                out.append("\t".join(c.get("Value", "") for c in r.get("Cells") or []))
    walk(rep["Rows"])
    return out

def reference(path):
    txt = open(path).read()
    body = txt.split("--- report output (tab-separated) ---", 1)[1]
    return [l for l in body.split("\n")]

pay = flatten(sys.argv[1])
ref = [l.rstrip("\n") for l in reference(sys.argv[2])]
# drop leading/trailing blank lines from the export
while ref and not ref[0].strip(): ref.pop(0)
while ref and not ref[-1].strip(): ref.pop()

def cells(line): return line.split("\t") if line else []

n = max(len(pay), len(ref))
diffs = []
print(f"{'goXero (flattened payload)':<64}| Xero (reference export)")
print("-" * 64 + "|" + "-" * 40)
for i in range(n):
    p = pay[i] if i < len(pay) else ""
    r = ref[i] if i < len(ref) else ""
    flag = "  " if p == r else "!!"
    print(f"{flag}{p:<62}| {r}")
    if p != r:
        pc, rc = cells(p), cells(r)
        for j in range(max(len(pc), len(rc))):
            a = pc[j] if j < len(pc) else ""
            b = rc[j] if j < len(rc) else ""
            if a != b:
                diffs.append((i + 1, j + 1, a, b))
print()
print(f"{len(diffs)} differing cell(s) of {n} row(s) compared")
for row, col, a, b in diffs:
    print(f"  row {row} col {col}: goXero {a!r} vs Xero {b!r}")
```

`gx-aged-compare.py` normalises the presentation (a `-` and a `0.00` are the same
nil, separators are not a difference) and then compares by value, which is what
isolates the claimant name as the only substantive difference:

```python
"""Compare a goXero Reports payload with Xero's captured export, by value.

Usage: gx-aged-compare.py <payload.json> <reference.txt>

The reference prints a nil as "-" and groups thousands with commas; the payload
prints a nil as "0.00" and does neither. Neither difference is a difference in
what the report says, so this script normalises both sides to the value each cell
carries and compares those. It reports, per row, whether the values match, and
then lists every cell that still differs once the presentation is normalised —
which is the list that has to be justified by hand.
"""
import json, re, sys

def payload_lines(path):
    rep = json.load(open(path))["Reports"][0]
    out = []
    def walk(rows):
        for r in rows:
            if r["RowType"] == "Section":
                if r.get("Title"):
                    out.append([r["Title"]])
                walk(r.get("Rows") or [])
            else:
                out.append([c.get("Value", "") for c in r.get("Cells") or []])
    walk(rep["Rows"])
    return out

def reference_lines(path):
    body = open(path).read().split("--- report output (tab-separated) ---", 1)[1]
    out = []
    for line in body.split("\n"):
        line = line.rstrip("\r")
        if not line.strip():
            continue
        out.append(line.split("\t"))
    return out

NUM = re.compile(r"^-?[\d,]+(\.\d+)?$")

def value(cell):
    """The value a cell carries: a number, a percentage, or None for a nil."""
    s = cell.strip()
    if s in ("", "-"):
        return None
    if s.endswith("%"):
        return ("pct", round(float(s[:-1]), 2))
    if NUM.match(s):
        return ("num", round(float(s.replace(",", "")), 2))
    return ("text", s)

def close(a, b):
    if a is None or b is None:
        return (a is None and b is None) or a == ("num", 0.0) or b == ("num", 0.0) \
            or a == ("pct", 0.0) or b == ("pct", 0.0)
    if a[0] != b[0]:
        return False
    if a[0] == "text":
        return a[1] == b[1]
    return abs(a[1] - b[1]) < 0.005

pay = payload_lines(sys.argv[1])
ref = reference_lines(sys.argv[2])
assert len(pay) == len(ref), f"row counts differ: payload {len(pay)} vs reference {len(ref)}"

presentational = 0     # cells equal in value, different in spelling
residual = []          # cells that differ in value
for i, (p, r) in enumerate(zip(pay, ref), start=1):
    label = p[0] if p else ""
    if len(p) != len(r):
        residual.append((i, label, "row width", len(p), len(r)))
        continue
    for j in range(len(p)):
        if p[j] == r[j]:
            continue
        if close(value(p[j]), value(r[j])):
            presentational += 1
        else:
            residual.append((i, label, f"col {j+1}", p[j], r[j]))
    print(f"row {i:2d} {label:<32} {'VALUES MATCH' if not any(x[0]==i for x in residual) else 'DIFFERS'}")

print()
print(f"rows compared: {len(pay)}")
print(f"cells identical as printed: {sum(1 for p, r in zip(pay, ref) for a, b in zip(p, r) if a == b)}")
print(f"cells different in spelling but equal in value: {presentational}")
print(f"cells different in value: {len(residual)}")
for row in residual:
    print("  ", row)
```
