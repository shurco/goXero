# Where the tax in this ledger actually lives, and what the Sales Tax Report should print

An exact account of the tax in organisation `6823b27b-c48f-4099-bb27-4202a4f496a2`, of the
measure `GET /api/v1/reports/sales-tax` (`/api/v1/reports/bas`) takes of it, of what the
report therefore prints today, and of what would have to change for it to print the figure
its own measure implies.

**Nothing was changed to produce this.** No product code was edited, no migration was run,
no row was written. Every figure below is either an output of a query in the body of this
document, or a verbatim capture of the running API, or a line of a file under
`docs/xero-reference/**` (frozen, and not edited). The database was read through
`docker exec bonefish-postgres-1 psql -U goxero -d goxero` and the API through
`http://localhost:8080`, both read-only; the backfill in section 6 exists only as a
read-only CTE that was never executed as a write.

---

## 0. The snapshot every number is pinned to, and why it needs pinning

The demo organisation is shared. A second session in this worktree writes to it, and it
wrote to it **while this document was being written**: four bank journals
(`journal_number` 1612–1615) were created between `19:30:45.665233+02` and
`19:30:53.904777+02` on 2026-09-11, two of them carrying a tax type on the coded line with
`tax_amount` 0. Section 8 reports that drift and its exact effect.

Everything in sections 1–5 is therefore pinned to one instant; sections 6 and 7 use later
readings, each labelled with the time it was read:

| Snapshot | Instant (Europe/Amsterdam, +02) | What was read |
| --- | --- | --- |
| **S0** | 2026-09-11 **19:27:32 – 19:29:26** | `E1`–`E12` (19:27:32–19:28:11), the 820 bridge (19:28:10), and the API render of the report (19:29:26, `DateTimeUTC 2026-09-11T17:29:26.489312Z`) |
| S1 | 2026-09-11 19:31:11 | the same queries re-run after the other session's four journals |

Every figure quoted in sections 1–7 was read at S0, and every one of them was then re-read
at 19:30:40 and again at 19:31:11 and came back **identical** except for the four new
journals of section 8. No number in this document was adjusted to make a comparison agree;
where a comparison does not agree, the difference is stated and decomposed.

---

## 1. What the Sales Tax report is measuring

### 1.1 The only place the report gets its numbers from

`internal/repository/report.go:807` `SalesTaxByRate` is the report's entire data source.
It reads exactly one table pair — `gl_journal_lines` joined to `gl_journals` — plus
`accounts` for each line's class and `tax_rates` for a display name. Verbatim
(`internal/repository/report.go:809–844`):

```sql
WITH agg AS (
    SELECT l.tax_type,
           COALESCE(SUM(CASE WHEN a.type IN ('REVENUE','SALES')
                             THEN -l.net_amount END),0) AS net_sales,
           COALESCE(SUM(CASE WHEN a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES')
                             THEN  l.net_amount END),0) AS net_purchases,
           COALESCE(SUM(CASE WHEN a.type IN ('REVENUE','SALES')
                              AND l.tax_amount <> 0 THEN l.tax_amount END),0) AS tax_collected,
           COALESCE(SUM(CASE WHEN a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES')
                              AND l.tax_amount <> 0 THEN l.tax_amount END),0) AS tax_paid,
           COUNT(*) FILTER (WHERE a.type IN ('REVENUE','SALES')) AS sales_lines,
           COUNT(*) FILTER (WHERE a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES')) AS purchase_lines,
           COUNT(*) FILTER (WHERE a.type IN ('REVENUE','SALES')
                              AND l.tax_amount <> 0) AS measured_sales,
           COUNT(*) FILTER (WHERE a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES')
                              AND l.tax_amount <> 0) AS measured_purchases
      FROM gl_journal_lines l
      JOIN gl_journals j ON j.journal_id = l.journal_id
      JOIN accounts a ON a.account_id = l.account_id
     WHERE j.organisation_id = $1
       AND j.journal_date BETWEEN $2 AND $3
       AND COALESCE(l.tax_type,'') <> ''
     GROUP BY l.tax_type
)
SELECT agg.tax_type, /* tax rate display name */, agg.net_sales, agg.net_purchases,
       agg.tax_collected, agg.tax_paid, agg.sales_lines, agg.purchase_lines,
       agg.measured_sales, agg.measured_purchases
  FROM agg ORDER BY agg.tax_type
```

(The two `IN (…)` lists are not literal in the source; they are `quotedList(incomeAccountTypes...)`
and `quotedList(costAccountTypes...)`, defined at `internal/repository/report.go:102–107` as
`{REVENUE, SALES}` and `{DIRECTCOSTS, EXPENSE, OVERHEADS, DEPRECIATN, WAGES}`. `FIXED` — the
class of accounts 710 and 720 — is in **neither** list, which section 5.2.3 turns out to matter.)

`internal/handlers/report_render.go:906` `renderSalesTax` only formats those rows: one row per
`tax_type`, a `Total` row that sums the rows, and a trailing row carrying the caveat sentence.
It reads no other data.

### 1.2 The measure, in one sentence

**The report measures the sum of the `tax_amount` column of every posted journal line in the
period that carries a tax type, split by the rate the line names and by whether the account
the line hit is an income or a cost account.**

### 1.3 Its basis

It is a **document-basis** figure. It reads no document table (`invoices`,
`invoice_line_items`, `credit_notes`, …) and no bank table; it reads no account *balance*
either — `accounts` is joined only to classify a line, never to sum it. It is a sum over
posted ledger lines in a date period, which is exactly what a document-basis tax workpaper is.

The three bases the task names, and where each of them is in this organisation:

| Basis | What it sums | This organisation's figure | Source |
| --- | --- | --- | --- |
| **document-basis** | the tax recorded on each posted line/document for the period | `820` nets to **422.59**; the documents carry 2,522.05 of tax on sales and 1,949.87 on purchases (section 3) | account 820 / `invoices.total_tax` |
| **cash-basis** | the tax that moved through the bank in the period | Tax Collected **1,844.56**, Tax Paid **(1,474.01)**, Net Tax Movements **370.55** | `docs/xero-reference/cash-summary.txt` |
| **account-balance** | the balance of the tax control account as at a date | Sales Tax **422.59** credit | `docs/xero-reference/trial-balance.txt`, reproduced by this ledger to the cent |

The report is meant to reproduce the **document-basis** figure — Xero's Sales Tax Report, the
workpaper behind the BAS. Three things in the code decide it, and none of them is accidental:

1. It filters `COALESCE(l.tax_type,'') <> ''` and groups by `tax_type`. A cash-basis report
   cannot be built that way: the tax that moved through the bank is on lines whose *other*
   side is the bank account, and its rate is an attribute of the coded line, not of the bank
   movement. The group-by-rate shape is the shape of a workpaper per rate.
2. It splits the columns into Net Sales and Net Purchases on the *account class* of the line.
   Cash Summary's three lines (Tax Collected / Tax Paid / Net Tax Movements) are not per-rate
   and are not split by class.
3. `internal/repository/report.go:803–806`, the comment over the function, says the report is
   the "sales-tax / BAS workpaper" and that any other account class "is not a sale or a
   purchase and contributes to neither column" — a statement about what is being classified,
   i.e. about documents.

So the report can **never** print 1,844.56 / 1,474.01 / 370.55: that is the Cash Summary's
measure of a different population (money that settled), and the Sales Tax report's code reads
neither the bank nor the settlement. Its number, when it prints one, must land on the
document-basis figure of section 3.9 — **422.59** — and on nothing else.

### 1.4 The guard

One rule stands between the query and the page. `internal/repository/report.go:860–870`:

```go
// A side is measured only when it has lines and every one of them
// records a tax amount. A rate with no sales at all has nothing to
// measure, so its collection stays empty rather than reading 0.00.
if measuredSales > 0 && measuredSales == salesLines {
        v := collected
        row.TaxCollected = &v
}
if measuredPurchases > 0 && measuredPurchases == purchaseLines {
        v := paid
        row.TaxPaid = &v
}
```

and, one level up, `internal/handlers/report_render.go:956–971`:

```go
allCollected := len(rows) > 0 && collectedRows == len(rows)
allPaid := len(rows) > 0 && paidRows == len(rows)
…
if allCollected { total.Cells[3] = money(taxCollected) }
if allPaid      { total.Cells[4] = money(taxPaid) }
if allCollected && allPaid { total.Cells[5] = money(taxCollected.Sub(taxPaid)) }
```

"Measured" is defined as `tax_amount <> 0`. That single definition is the root of the report's
side of the problem, and section 5.2 shows why with numbers.

---

## 2. What the report prints today

Request (the API is running; it was not restarted):

```sh
TOK=$(curl -s -X POST http://localhost:8080/api/auth/login -H 'Content-Type: application/json' \
      -d '{"email":"admin@demo.local","password":"admin123"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')
ORG=6823b27b-c48f-4099-bb27-4202a4f496a2
curl -s "http://localhost:8080/api/v1/reports/sales-tax?fromDate=2026-01-01&toDate=2026-12-31" \
     -H "Authorization: Bearer $TOK" -H "Xero-Tenant-Id: $ORG"
```

Captured at S0, `DateTimeUTC 2026-09-11T17:29:26.489312Z`. The response's `Rows` array, laid out
as a table — every cell's `Value` is here, and an empty cell is the empty string, which is what the
API returns for a tax cell the guard refuses to fill:

```
RowType: Header   Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax
Section:
  Row  Tax on Purchases (8.25%)   0.00       19198.85    (empty)  (empty)  (empty)
  Row  Tax Exempt (0%)            0.00        1823.60    (empty)  (empty)  (empty)
  Row  Tax on Consulting (8.25%)  29375.43        0.00    (empty)  (empty)  (empty)
  Row  Tax on Goods (8.75%)        163.75      500.00    (empty)  (empty)  (empty)
  SummaryRow  Total               29539.18    21522.45    (empty)  (empty)  (empty)
Row  "Tax Collected, Tax Paid and Net Tax are left empty: no tax rate carries a tax amount
      on every one of its posted lines, and this report will not total part of a rate. Some
      lines do record a tax amount; they are not summed here because the rest of their
      rate's lines do not."
```

The same array as the API sent it, unedited and unabridged (the envelope fields were
`"Id": "da5493c5-c560-485a-9af5-069eaf9b7a77"`, `"Status": "OK"`, `"ProviderName": "goxero"`,
`"ReportID": "SalesTaxReport"`, `"ReportName": "Sales Tax Report"`, `"ReportType":
"SalesTaxReport"`, `"ReportTitles": ["Sales Tax Report", "Demo Company (Global)", "From 1 January
2026 To 31 December 2026"]`, `"ReportDate": "31 December 2026"`, `"Fields": []`):

```json
"Rows": [
  {"RowType": "Header", "Cells": [{"Value": "Tax Rate"}, {"Value": "Net Sales"},
     {"Value": "Net Purchases"}, {"Value": "Tax Collected"}, {"Value": "Tax Paid"},
     {"Value": "Net Tax"}]},
  {"RowType": "Section", "Rows": [
    {"RowType": "Row", "Cells": [{"Value": "Tax on Purchases (8.25%)"}, {"Value": "0.00"},
       {"Value": "19198.85"}, {"Value": ""}, {"Value": ""}, {"Value": ""}]},
    {"RowType": "Row", "Cells": [{"Value": "Tax Exempt (0%)"}, {"Value": "0.00"},
       {"Value": "1823.60"}, {"Value": ""}, {"Value": ""}, {"Value": ""}]},
    {"RowType": "Row", "Cells": [{"Value": "Tax on Consulting (8.25%)"}, {"Value": "29375.43"},
       {"Value": "0.00"}, {"Value": ""}, {"Value": ""}, {"Value": ""}]},
    {"RowType": "Row", "Cells": [{"Value": "Tax on Goods (8.75%)"}, {"Value": "163.75"},
       {"Value": "500.00"}, {"Value": ""}, {"Value": ""}, {"Value": ""}]},
    {"RowType": "SummaryRow", "Cells": [{"Value": "Total"}, {"Value": "29539.18"},
       {"Value": "21522.45"}, {"Value": ""}, {"Value": ""}, {"Value": ""}]}
  ]},
  {"RowType": "Row", "Cells": [
    {"Value": "Tax Collected, Tax Paid and Net Tax are left empty: no tax rate carries a tax amount on every one of its posted lines, and this report will not total part of a rate. Some lines do record a tax amount; they are not summed here because the rest of their rate's lines do not."}
  ]}
]
```

The same request, re-issued at 19:35:10 while the other session's four journals of §8 were in the
ledger, returns the identical shape — the same four rates, the same columns empty, the same caveat
sentence — with only the report's own Net Sales total unchanged at `29539.18` and the two Net Purchases cells moved by
the drift (`19214.35`, `1838.60`, `Total 21552.95`, against S0's `19198.85`, `1823.60`,
`21522.45`; +15.50 and +15.00, exactly the two tax-typed lines of §8). The tax columns and the
caveat sentence are unchanged by it.

That caveat sentence is the `collectedRows == 0 && paidRows == 0` branch of
`taxColumnsCaveat` (`internal/handlers/report_render.go:1002–1018`). It is **true**: not one
of the four rate rows has a fully-measured side, so `collectedRows` and `paidRows` are both
zero and the second branch ("Tax Collected and Tax Paid are the tax amount recorded on each
posted journal line") is not reached. Its predecessor, which claimed no line carried a tax
amount at all, was false and was corrected earlier in this worktree
(`docs/reports-xero-parity-summary.md` §8, finding 2).

The two columns that *do* print are correct and are a real check on the import: Net Sales
29,539.18 is Xero's Sales figure to the cent, and Net Purchases 21,522.45 differs from Xero's
Cost of Sales 775.98 + Total Operating Expenses 20,496.47 = 21,272.45 by exactly the 250.00
of another session's bank coding on `400 Advertising` (section 8).

---

## 3. Where the tax actually lives

Every query below was run at S0 with `psql -P footer=off`, and every one of them was re-run at
19:30:40 and 19:31:11. Every amount came back identical at all three readings; the only thing that
moved was the line count of two bank rows, in the four journals section 8 records. The `now()` the
query prints is part of the output, not decoration.

### E1 — the entire input of the report, by rate and account class

```sql
SELECT now();
SELECT l.tax_type, a.type AS account_type, count(*) AS lines,
       sum(l.net_amount) AS net_amount, sum(l.tax_amount) AS tax_amount,
       count(*) FILTER (WHERE l.tax_amount <> 0) AS lines_with_tax
  FROM gl_journal_lines l
  JOIN gl_journals j ON j.journal_id = l.journal_id
  JOIN accounts a ON a.account_id = l.account_id
 WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
   AND j.journal_date BETWEEN DATE '2026-01-01' AND DATE '2026-12-31'
   AND COALESCE(l.tax_type,'') <> ''
 GROUP BY 1,2 ORDER BY 1,2;
```

```
              now
-------------------------------
 2026-09-11 19:27:32.778433+02

 tax_type | account_type | lines | net_amount  | tax_amount | lines_with_tax
----------+--------------+-------+-------------+------------+----------------
 INPUT    | DIRECTCOSTS  |     1 |    775.9800 |     0.0000 |              0
 INPUT    | EXPENSE      |    69 |  18422.8700 |    74.1600 |             33
 INPUT    | FIXED        |     3 |   4698.2800 |     0.0000 |              0
 NONE     | EXPENSE      |     8 |   1823.6000 |     0.0000 |              0
 OUTPUT   | REVENUE      |    35 | -29375.4300 |     0.0000 |              0
 OUTPUT2  | EXPENSE      |     1 |    500.0000 |     0.0000 |              0
 OUTPUT2  | REVENUE      |     5 |   -163.7500 |     0.0000 |              0
```

122 posted lines carry a tax type (124 at S1 — §8). **33** of the 122 record a tax amount — all 33 of them on
`INPUT`/`EXPENSE` lines, all 33 from the bank feed (E2). Nothing on a revenue line records
one; the three `FIXED` lines (the capital bills of E6) record none; the `NONE` lines record
none, correctly, because their rate is 0%.

### E2 — every unit of tax in `gl_journal_lines.tax_amount`

```sql
SELECT now();
SELECT l.tax_type, a.code, a.type, j.source_type, count(*) lines, sum(l.tax_amount) tax
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND l.tax_amount <> 0
 GROUP BY 1,2,3,4 ORDER BY 1,2;
```

```
              now
-------------------------------
 2026-09-11 19:27:32.782252+02

 tax_type | code |  type   |   source_type   | lines |   tax
----------+------+---------+-----------------+-------+---------
 INPUT    | 429  | EXPENSE | BANKTRANSACTION |     1 |  3.8100
 INPUT    | 449  | EXPENSE | BANKTRANSACTION |     2 | 22.6400
 INPUT    | 453  | EXPENSE | BANKTRANSACTION |    10 | 13.9300
 INPUT    | 461  | EXPENSE | BANKTRANSACTION |     2 |  5.5400
 INPUT    | 473  | EXPENSE | BANKTRANSACTION |     1 |  5.3000
 INPUT    | 489  | EXPENSE | BANKTRANSACTION |     1 |  8.3800
 INPUT    | 493  | EXPENSE | BANKTRANSACTION |    16 | 14.5600
```

33 lines, 74.16 in total, every one of them a bank-feed expense line. That is the *whole* of
what the report's measure can currently see, and it is why the tax columns are empty: 74.16 is
recorded on 33 of the 70 purchase lines of the `INPUT` row, so `measured_purchases` (33) is
not `purchase_lines` (70), and `Tax Paid` stays empty.

### E3 — account 820: every posting, by where it came from

```sql
SELECT now();
SELECT j.source_type, count(*) lines, sum(l.net_amount) AS net
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code='820'
 GROUP BY 1 ORDER BY 1;
SELECT sum(l.net_amount) AS account_820_balance, count(*) AS lines
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code='820';
```

```
              now
-------------------------------
 2026-09-11 19:27:32.782977+02

   source_type   | lines |    net
-----------------+-------+-----------
 BANKTRANSACTION |    33 |   74.1600
 CREDITNOTE      |     5 |   61.6800
 EXPENSECLAIM    |     4 |   13.7500
 INVOICE         |    65 | -572.1800

 account_820_balance | lines
---------------------+-------
           -422.5900 |   107
```

**The tax is in the ledger.** It is not in `gl_journal_lines.tax_amount` (that column holds
74.16) — it is on the 107 lines that *hit account 820*, posted alongside the document lines by
migration 00024. Every comparison in this document is ultimately a comparison against
`−422.59`, and section 3.9 shows that figure is not an accident: it is the exact sum of the
tax leg of every posted document and bank movement.

### E4 — the naive pairing: invoice and bill line items by status

```sql
SELECT now();
SELECT i.type, i.status, count(DISTINCT i.invoice_id) documents, count(*) lines, sum(li.tax_amount) tax
  FROM invoice_line_items li JOIN invoices i ON i.invoice_id=li.invoice_id
 WHERE i.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'
 GROUP BY 1,2 ORDER BY 1,2;
```

```
              now
-------------------------------
 2026-09-11 19:27:32.784373+02

  type  |   status   | documents | lines |    tax
--------+------------+-----------+-------+-----------
 ACCPAY | AUTHORISED |        12 |    12 |  707.7700
 ACCPAY | DELETED    |         4 |     4 |   23.7600
 ACCPAY | PAID       |        23 |    24 | 1242.1000
 ACCPAY | VOIDED     |         7 |     7 |  198.4600
 ACCREC | AUTHORISED |         9 |    12 |  743.3800
 ACCREC | DRAFT      |         2 |     2 |   83.8400
 ACCREC | PAID       |        21 |    26 | 1778.6700
```

Sums: ACCREC 2,605.89, ACCPAY 2,172.09. These are the two numbers of the coordinator's
pairing, and section 4.1 takes them apart.

### E5 — what the naive pairing leaves out

```sql
SELECT now();
SELECT 'ACCRECCREDIT' AS src, cn.status, count(*) lines, sum(li.tax_amount) tax
  FROM credit_note_line_items li JOIN credit_notes cn ON cn.credit_note_id=li.credit_note_id
 WHERE cn.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND cn.type='ACCRECCREDIT' GROUP BY 1,2
UNION ALL
SELECT 'ACCPAYCREDIT', cn.status, count(*), sum(li.tax_amount)
  FROM credit_note_line_items li JOIN credit_notes cn ON cn.credit_note_id=li.credit_note_id
 WHERE cn.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND cn.type='ACCPAYCREDIT' GROUP BY 1,2
UNION ALL
SELECT 'EXPENSECLAIM(820 legs)', NULL, count(*), sum(l.net_amount)
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.source_type='EXPENSECLAIM' AND a.code='820'
UNION ALL
SELECT 'BANK(direct-spend tax)', NULL, count(*), sum(l.net_amount)
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.source_type='BANKTRANSACTION' AND a.code='820';
```

```
              now
-------------------------------
 2026-09-11 19:27:32.785521+02

          src           | status | lines |   tax
------------------------+--------+-------+---------
 ACCRECCREDIT           | PAID   |     3 | 84.2500
 ACCPAYCREDIT           | PAID   |     2 | 22.5700
 EXPENSECLAIM(820 legs) |        |     4 | 13.7500
 BANK(direct-spend tax) |        |    33 |  74.1600
```

### E6 — the three capital lines whose input tax the report's class split cannot see

```sql
SELECT now();
SELECT i.date, i.invoice_number, c.name AS contact, ac.code, ac.name, li.description, li.tax_amount
  FROM invoices i JOIN invoice_line_items li ON li.invoice_id=i.invoice_id
  JOIN accounts ac ON ac.account_id=li.account_id LEFT JOIN contacts c ON c.contact_id=i.contact_id
 WHERE i.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND ac.type='FIXED'
 ORDER BY i.date;
```

```
             now
------------------------------
 2026-09-11 19:27:32.78748+02

    date    | invoice_number |    contact    | code |        name        |        description         | tax_amount
------------+----------------+---------------+------+--------------------+----------------------------+------------
 2026-07-21 |                | PC Complete   | 720  | Computer Equipment | Laptop (Oliver)            |   148.8700
 2026-08-21 | 710            | ABC Furniture | 710  | Office Equipment   | Coffee table for reception |    76.2100
 2026-09-05 |                | PC Complete   | 720  | Computer Equipment | Laptop (Tracy)             |   162.5200
```

76.21 + 148.87 + 162.52 = **387.60**, and 387.60 is the entire difference between the
document-basis net tax and account 820 (section 5.4). These three lines are on `FIXED`
accounts, which `costAccountTypes` (`internal/repository/report.go:103–106`) does not contain,
so the report counts neither their net nor their tax. The ledger does: 820 holds the 387.60.

### E7 — the rates the organisation defines

```sql
SELECT now();
SELECT name, tax_type, report_tax_type, display_tax_rate, effective_rate, status FROM tax_rates
 WHERE organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' ORDER BY tax_type;
```

```
             now
---------------------------
 2026-09-11 19:27:32.788587+02

           name            | tax_type | report_tax_type | display_tax_rate | effective_rate | status
---------------------------+----------+-----------------+------------------+----------------+--------
 Tax on Purchases (8.25%)  | INPUT    | INPUT           |           8.2500 |         8.2500 | ACTIVE
 Tax Exempt (0%)           | NONE     | NONE            |           0.0000 |         0.0000 | ACTIVE
 Tax on Consulting (8.25%) | OUTPUT   | OUTPUT          |           8.2500 |         8.2500 | ACTIVE
 Tax on Goods (8.75%)      | OUTPUT2  | OUTPUT2         |           8.7500 |         8.7500 | ACTIVE
```

Four active rates, one of them **0%**. `NONE` is in use on 9 posted lines (E1, S1) and its
correct tax amount on every one of them is **zero** — which is what destroys the Total row's
guard (section 5.2.1). The rates are also the organisation's own and are what the report's
`tax_type` groups by, so the report's four rows are the four rates that actually carry postings.

### The bridge, item by item

The reconciliation the task asks for, in the style of
`docs/xero-reference/reconciliation.md`: start from the number that looks like a tax
difference and walk, one named item at a time, to the number the ledger actually holds.
Run at 19:28:10:

```sql
SELECT now();
WITH doc AS (
  SELECT i.type, i.status, i.total_tax
    FROM invoices i WHERE i.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'),
cn AS (
  SELECT cn.type, sum(li.tax_amount) AS tax
    FROM credit_notes cn JOIN credit_note_line_items li ON li.credit_note_id=cn.credit_note_id
   WHERE cn.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' GROUP BY 1),
other AS (
  SELECT j.source_type, sum(l.net_amount) AS tax
    FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id
    JOIN accounts a ON a.account_id=l.account_id AND a.code='820'
   WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' GROUP BY 1)
SELECT * FROM (
  SELECT 1 ord, 'ACCREC invoice lines, every status' item,
         (SELECT sum(total_tax) FROM doc WHERE type='ACCREC') amt
  UNION ALL SELECT 2, 'less ACCPAY bill lines, every status',
         -(SELECT sum(total_tax) FROM doc WHERE type='ACCPAY')
  UNION ALL SELECT 3, '= the naive difference',
         (SELECT sum(total_tax) FROM doc WHERE type='ACCREC')-(SELECT sum(total_tax) FROM doc WHERE type='ACCPAY')
  UNION ALL SELECT 4, 'less ACCREC DRAFT/VOIDED (no journal)',
         -(SELECT coalesce(sum(total_tax),0) FROM doc WHERE type='ACCREC' AND status IN ('DRAFT','VOIDED','DELETED'))
  UNION ALL SELECT 5, 'plus ACCPAY VOIDED/DELETED (no journal)',
         (SELECT coalesce(sum(total_tax),0) FROM doc WHERE type='ACCPAY' AND status IN ('DRAFT','VOIDED','DELETED'))
  UNION ALL SELECT 6, '= posted sales invoices (30) less posted bills (35)',
         (SELECT sum(total_tax) FROM doc WHERE type='ACCREC' AND status NOT IN ('DRAFT','VOIDED','DELETED'))
       - (SELECT sum(total_tax) FROM doc WHERE type='ACCPAY' AND status NOT IN ('DRAFT','VOIDED','DELETED'))
  UNION ALL SELECT 7, 'less ACCRECCREDIT note lines',
         -(SELECT tax FROM cn WHERE type='ACCRECCREDIT')
  UNION ALL SELECT 8, 'plus ACCPAYCREDIT note lines',
         (SELECT tax FROM cn WHERE type='ACCPAYCREDIT')
  UNION ALL SELECT 9, 'less expense-claim input tax (820 legs)',
         -(SELECT tax FROM other WHERE source_type='EXPENSECLAIM')
  UNION ALL SELECT 10, 'less bank direct-spend input tax (820 legs)',
         -(SELECT tax FROM other WHERE source_type='BANKTRANSACTION')
  UNION ALL SELECT 11, '= account 820 Sales Tax',
         (SELECT sum(l.net_amount) FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id
            JOIN accounts a ON a.account_id=l.account_id
           WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code='820')
) b ORDER BY ord;
```

```
              now
-------------------------------
 2026-09-11 19:28:10.998235+02

 ord |                        item                         |    amt
-----+-----------------------------------------------------+------------
   1 | ACCREC invoice lines, every status                  |  2605.8900
   2 | less ACCPAY bill lines, every status                | -2172.0900
   3 | = the naive difference                              |   433.8000
   4 | less ACCREC DRAFT/VOIDED (no journal)               |   -83.8400
   5 | plus ACCPAY VOIDED/DELETED (no journal)             |   222.2200
   6 | = posted sales invoices (30) less posted bills (35) |   572.1800
   7 | less ACCRECCREDIT note lines                        |   -84.2500
   8 | plus ACCPAYCREDIT note lines                        |    22.5700
   9 | less expense-claim input tax (820 legs)             |   -13.7500
  10 | less bank direct-spend input tax (820 legs)         |   -74.1600
  11 | = account 820 Sales Tax                             |  -422.5900
```

**Remainder: none.** Eleven lines of arithmetic, every one of them a named population with a
count and a status behind it (E4, E5), landing on the cent on the figure account 820 holds.
Each step says what it is:

| # | Item | Amount | What it is | Can it move a figure the report prints? |
| --- | --- | --- | --- | --- |
| 1 | ACCREC line-item tax, every status | +2,605.89 | 40 lines, 32 invoices, `DRAFT`+`AUTHORISED`+`PAID` | the 2 DRAFT invoices post **no journal**, so they cannot move a printed figure |
| 2 | ACCPAY line-item tax, every status | −2,172.09 | 47 lines, 46 bills, includes `DELETED` and `VOIDED` | the 11 DELETED/VOIDED bills post **no journal** either |
| 3 | naive difference | 433.80 | a difference of two *mixed-status* populations | see §4.1 — it is not a tax figure at all |
| 4 | less ACCREC DRAFT | −83.84 | 2 invoices, no journal | no |
| 5 | plus ACCPAY VOIDED+DELETED | +222.22 | 11 bills (198.46 VOIDED + 23.76 DELETED), no journal | no |
| 6 | **posted** sales invoices less **posted** bills | **572.18** | 30 ACCREC, 35 ACCPAY — exactly the 65 `INVOICE` journals of E3 | **yes** — this is the document population that is actually posted |
| 7 | less ACCRECCREDIT note tax | −84.25 | 3 lines, CN-0014/0015/0023, `PAID`, `820` debited | **yes** |
| 8 | plus ACCPAYCREDIT note tax | +22.57 | 2 lines, `820` credited | **yes** |
| 9 | less expense-claim input tax | −13.75 | 4 `820` legs of the 3 claims / 4 journals | **yes** |
| 10 | less bank direct-spend input tax | −74.16 | 33 `820` legs — the same 33 lines and the same 74.16 as E2 | **yes** — this is the only tax the report can see |
| 11 | **account 820 Sales Tax** | **−422.59** | 107 lines | — |

Both endpoints cross-check against the frozen capture:

```sql
SELECT now();
SELECT coalesce(sum(CASE WHEN l.net_amount>0 THEN l.net_amount END),0) AS debits,
       coalesce(sum(CASE WHEN l.net_amount<0 THEN -l.net_amount END),0) AS credits,
       sum(l.net_amount) AS net, count(*) AS lines
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code='820';
```

```
              now
-------------------------------
 2026-09-11 19:27:59.030054+02

  debits   |  credits  |    net    | lines
-----------+-----------+-----------+-------
 2122.0300 | 2544.6200 | -422.5900 |   107
```

`docs/xero-reference/general-ledger-detail.txt` section `##Sales Tax` (line 389) closes at line
499 with `Total Sales Tax  2,122.03  2,544.62  (422.59)`. Debit for debit, credit for credit,
net for net, to the cent. (Xero's file shows 109 postings against this ledger's 107; the two
extra are Xero's own paired presentation of the receivable/payable credit-note allocations,
which `docs/reports-xero-parity-summary.md` §4 accounts for as a Dr/Cr wash inside one account
and which nets to the same figure.)

### 3.9 Does every tax-bearing document post its tax to 820? Yes — all 65 of them

This is the question the task asks before any difference can be called a difference.

```sql
SELECT now();
WITH leg AS (
  SELECT j.journal_id, j.source_type, j.source_id, sum(abs(l.net_amount)) AS leg, sum(l.net_amount) AS signed_leg
    FROM gl_journals j JOIN gl_journal_lines l ON l.journal_id=j.journal_id
    JOIN accounts a ON a.account_id=l.account_id AND a.code='820'
   WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' GROUP BY 1,2,3)
SELECT i.type, count(*) journals, count(*) FILTER (WHERE leg.leg=i.total_tax) AS leg_is_tax_total,
       count(*) FILTER (WHERE leg.signed_leg = CASE WHEN i.type='ACCREC' THEN -i.total_tax ELSE i.total_tax END) AS sign_correct,
       sum(i.total_tax) AS document_tax
  FROM leg JOIN invoices i ON i.invoice_id=leg.source_id WHERE leg.source_type='INVOICE'
 GROUP BY 1 ORDER BY 1;
```

```
              now
-------------------------------
 2026-09-11 19:27:59.035014+02

  type  | journals | leg_is_tax_total | sign_correct | document_tax
--------+----------+------------------+--------------+--------------
 ACCPAY |       35 |               35 |           35 |    1949.8700
 ACCREC |       30 |               30 |           30 |    2522.0500
```

65 journals, 65 correct legs, 0 mismatches, and the sign is right on every one: an ACCREC
invoice credits 820 (`−total_tax`), an ACCPAY bill debits it (`+total_tax`). The same holds
for the bank:

```sql
SELECT now();
WITH bj AS (
  SELECT j.journal_id, sum(l.tax_amount) FILTER (WHERE a.code<>'820') AS coded_tax,
         sum(l.net_amount) FILTER (WHERE a.code='820') AS leg
    FROM gl_journals j JOIN gl_journal_lines l ON l.journal_id=j.journal_id JOIN accounts a ON a.account_id=l.account_id
   WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.source_type='BANKTRANSACTION' GROUP BY 1)
SELECT count(*) journals, count(*) FILTER (WHERE coded_tax=leg) agree, count(*) FILTER (WHERE coded_tax<>leg) disagree,
       sum(leg) AS total_bank_tax FROM bj;
```

```
              now
-----------------------------
 2026-09-11 19:27:59.0372+02

 journals | agree | disagree | total_bank_tax
----------+-------+----------+----------------
       83 |    33 |        0 |        74.1600
```

83 bank journals, 33 of them carry tax, and on all 33 the coded line's `tax_amount` equals its
`820` sibling exactly. So the ledger is internally consistent: **whatever tax exists, is on 820,
and its sum to the cent is −422.59.**

The two document families whose tax lives nowhere near `gl_journal_lines.tax_amount` were
checked separately, because the credit-note line table has `account_code` but no `account_id`,
and expense claims have no line table at all in use:

```sql
SELECT now();
WITH dl AS (
  SELECT l.line_id, j.source_id, j.source_type, l.account_id, l.description, abs(l.net_amount) AS amt
    FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id
   WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.source_type IN ('INVOICE','CREDITNOTE'))
SELECT source_type, count(*) AS doc_lines,
       count(*) FILTER (WHERE n_il = 1) AS matched_exactly_one,
       count(*) FILTER (WHERE n_il = 0) AS matched_none,
       count(*) FILTER (WHERE n_il > 1) AS matched_more_than_one
  FROM (SELECT d.*, (SELECT count(*) FROM invoice_line_items il
                      WHERE il.invoice_id=d.source_id AND il.account_id=d.account_id
                        AND il.line_amount=d.amt AND COALESCE(il.description,'')=COALESCE(d.description,'')) AS n_il
          FROM dl d WHERE d.source_type='INVOICE') x
 GROUP BY source_type
UNION ALL
SELECT 'CREDITNOTE', count(*), count(*) FILTER (WHERE n=1), count(*) FILTER (WHERE n=0), count(*) FILTER (WHERE n>1)
  FROM (SELECT d.*, (SELECT count(*) FROM credit_note_line_items cl JOIN accounts a ON a.code=cl.account_code
                      WHERE cl.credit_note_id=d.source_id AND a.account_id=d.account_id
                        AND cl.line_amount=d.amt AND COALESCE(cl.description,'')=COALESCE(d.description,'')) AS n
          FROM dl d WHERE d.source_type='CREDITNOTE') y;
```

```
              now
-------------------------------
 2026-09-11 19:31:47.259675+02

 source_type | doc_lines | matched_exactly_one | matched_none | matched_more_than_one
-------------+-----------+---------------------+--------------+-----------------------
 INVOICE     |       204 |                  74 |          130 |                     0
 CREDITNOTE  |        15 |                   5 |          10 |                     0
```

The 204 and 15 include each journal's control line and its `820` line, which correctly match
nothing (130 = 65 control + 65 tax; 10 = 5 control + 5 tax). **Every one of the 74 document
lines of the 65 posted invoices and all 5 credit-note lines is matched by exactly one source
line item; none is unmatched and none is ambiguous.** That is what makes a backfill possible
at all, and it is the cardinality the backfill spec in section 6 rests on.

Expense claims have no line table with tax in it (`receipts`/`receipt_line_items` are empty:
0 rows, 0 rows). Their per-line tax survives only as the sibling `820` leg:

```sql
SELECT now();
SELECT j.journal_number, a.code, a.type, l.tax_type, l.tax_amount, l.net_amount, l.description
  FROM gl_journals j JOIN gl_journal_lines l ON l.journal_id=j.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.source_type='EXPENSECLAIM'
 ORDER BY j.journal_number, a.code;
```

```
              now
-------------------------------
 2026-09-11 19:31:47.268825+02

 journal_number | code |  type   | tax_type | tax_amount | net_amount |                      description
----------------+------+---------+----------+------------+------------+--------------------------------------------------------
           1570 | 493  | EXPENSE | INPUT    |     0.0000 |    15.6100 | Orlena Greenville - Breakfast before MRE conference
           1570 | 493  | EXPENSE | INPUT    |     0.0000 |    16.6300 | Orlena Greenville - Parking for MRE conference
           1570 | 801  | CURRLIAB |          |     0.0000 |   -34.9000 |
           1570 | 820  | CURRLIAB |          |     0.0000 |     1.2900 |
           1570 | 820  | CURRLIAB |          |     0.0000 |     1.3700 |
           1571 | 461  | EXPENSE | INPUT    |     0.0000 |    27.2500 | Orlena Greenville - Print/bind report
           1571 | 801  | CURRLIAB |          |     0.0000 |   -29.5000 |
           1571 | 820  | CURRLIAB |          |     0.0000 |     2.2500 |
           1572 | 453  | EXPENSE | INPUT    |     0.0000 |   107.1100 | Xero Demo - Battery pack & power cable for home office
           1572 | 801  | CURRLIAB |          |     0.0000 |  -115.9500 |
           1572 | 820  | CURRLIAB |          |     0.0000 |     8.8400 |
```

Journal 1570 has two claim lines and two `820` legs, so an expense claim is not a
one-tax-to-one-line document: see §6.3 for the pairing rule and its caveat. 1.29 + 1.37 + 2.25
+ 8.84 = **13.75**, which is item 9 of the bridge.

### 3.10 The reconciliation, concluded

* Every tax-bearing document **does** post its tax to 820, with the right sign, and 820 holds
  **422.59**.
* The sum of the tax leg of every journal touching 820 is **−422.59**, and the bridge from the
  mixed-status pairing to it has **no unexplained remainder**.
* Not one unit of that 422.59 is a document line's `tax_amount`. The report's measure and the
  ledger's tax account describe the same money in two different columns, and only the second
  one is populated.

---

## 4. The two conflicting pairings, resolved

### 4.1 `2,605.89 − 2,172.09 = 433.80` against 820's `422.59`

This pairing compares **two populations that are not the same measure**. Its two terms are the
sums of the captured source CSVs:

```sh
cd migrations/data/xero && python3 -c "
import csv
for f,h in [('invoice-lines.csv','invoice-lines'),('bill-lines.csv','bill-lines'),
            ('credit-note-lines.csv','credit-note-lines'),('expense-claims.csv','expense-claims')]:
    r=list(csv.DictReader(open(f)))
    print('%-20s %3d rows  tax_amount sum = %9.2f' % (h, len(r), sum(float(x['tax_amount'] or 0) for x in r)))
"
```

```
invoice-lines         40 rows  tax_amount sum =   2605.89
bill-lines            47 rows  tax_amount sum =   2172.09
credit-note-lines      5 rows  tax_amount sum =    106.82
expense-claims         4 rows  tax_amount sum =     13.75
```

2,605.89 is **every** ACCREC line item including the 2 `DRAFT` invoices that post no journal;
2,172.09 is **every** ACCPAY line item including 7 `VOIDED` and 4 `DELETED` bills that post no
journal either. Subtracting one from the other cancels the two sides' unmatched noise against
each other and produces a number that is not the tax on anything: it is a difference of two
mixed-status populations. The four items it leaves out — credit-note tax 106.82, expense-claim
tax 13.75, bank direct-spend tax 74.16, and the status mismatch itself — are exactly the items
the bridge in §3 walks through, and the bridge's **remaining remainder is zero**.

So: **the pairing is wrong in kind (mixed populations), and the number that is the right
comparison is `422.59` — the sum of the tax leg of every journal touching account 820**, reached
from 433.80 by the eleven-step bridge in §3, item by item, with nothing unexplained left over.

### 4.2 `897.35 of document net tax against the ledger's 422.59`

The earlier session's pairing (`docs/reports-xero-parity-summary.md` §5.3 and §6,
`docs/xero-import-00024.md` §11) is not reproducible at all, and therefore is not comparing a
different measure — it is comparing a figure that does not exist in any data this organisation
has:

* `grep -rn "897.35"` over the whole repository finds exactly two files, and both are the two
  documents that *state* the problem. It appears in no captured Xero file, in no CSV, and in no
  query output.
* It is not a column sum of any captured CSV: those are 2,605.89, 2,172.09, 106.82, 13.75.
* It is not any aggregate the database produces: the per-type, per-status and per-source sums
  are E4's and E5's, and 820 is 422.59. No window of that data nets to 897.35.
* `docs/xero-import-00024.md` §11 says so itself: the figure "does not appear in any captured
  file, in `docs/xero-reference/reconciliation.md`, or in the ledger, so it cannot be tied out
  here."

The two source files the earlier session read it from, `invoice-lines.csv` and `bill-lines.csv`,
were regenerated afterwards by the 00024 work. 897.35 is a stale figure from a superseded data
state. It should not be carried forward as an open item; §4.1's bridge replaces it.

### 4.3 The right comparison, in one line

`422.59` = account 820 = the sum of the tax leg of the 65 invoice, 5 credit-note, 3 expense-claim
and 33 bank journals that carry tax. And separately, because it is Xero's own capture and not
ours: `2,437.80 − 2,015.21 = 422.59` (§7).

---

## 5. Which of the three is the defect

### 5.1 (a) The import left `tax_amount` at 0 on every document line — **this is the defect**

Migration 00024 builds the ledger from a `xr_gl` temp table of literal `VALUES`
(`migrations/00024_xero_reference_source_documents.sql:643`) in which **every** document line is
written with `tax_amount` `0.00`. The file's own example, verbatim at line 650:

```sql
('invoice/INV-0001', DATE '2026-07-11', 'INV-0001', 'INVOICE', 'invoice/INV-0001', 'control', '610', 'INV-0001', NULL,       0.00,  541.25),
('invoice/INV-0001', DATE '2026-07-11', 'INV-0001', 'INVOICE', 'invoice/INV-0001', 'line/1',  '200', 'Desktop/network support …', 'OUTPUT', 0.00, -500.00),
('invoice/INV-0001', DATE '2026-07-11', 'INV-0001', 'INVOICE', 'invoice/INV-0001', 'tax',     '820', NULL,                          NULL, 0.00,  -41.25),
```

The tax is in the journal — on the `tax` line, against 820 — and `0.00` on the `line/N` line.
The bank rows of the same table compute the split and get it right, which is why the only 33
lines in the whole ledger that carry a tax amount are bank lines (E2). The source data was
there: `invoice_line_items.tax_amount` holds 4,777.98 over 85 lines, `credit_note_line_items`
holds 106.82 over 5, and the 820 legs hold the claim tax. The import did not carry it onto the
line the report reads.

**What the report prints if only (a) is fixed** (§6 gives the backfill; §6.5 gives what the report's own query returns over it):

| Row | Tax Collected | Tax Paid | Net Tax |
| --- | --- | --- | --- |
| Tax on Purchases (8.25%) | *(no sales)* | **1,583.86** | — |
| Tax Exempt (0%) | *(no sales)* | *(empty)* | — |
| Tax on Consulting (8.25%) | **2,423.47** | *(no purchases)* | — |
| Tax on Goods (8.75%) | **14.33** | **43.75** | **−29.42** |
| **Total** | *(empty)* | *(empty)* | *(empty)* |

and the caveat switches to the fourth branch: *"Tax Collected and Tax Paid are shown only where
every line under the rate records a tax amount: **2 of 4** rates support Tax Collected and
**2 of 4** support Tax Paid, so the rest of those two columns and Net Tax are left empty."*
That is the count at S0, where the backfill measures 70 of the 70 purchase lines of `INPUT`; at
S1 the other session's unsourceable line (§6.4) makes it 70 of 71, so Tax Paid falls to **1 of
4** and the `INPUT` row's Tax Paid cell empties again — §6.5's E14 output is the S1 reading.
The Total row prints no tax figure because the guard of §5.2.1 refuses — so (a) alone turns a
blank report into a half-filled one, not into a correct one.

### 5.2 (b) The report's guard and class split are each independently fatal — **also a defect**

Three separate things in `internal/repository/report.go` and
`internal/handlers/report_render.go` prevent a correct total for this organisation, and each
one is provable from the data above.

**5.2.1 The Total row requires a rate with no sales to have collected some.**
`allCollected := len(rows) > 0 && collectedRows == len(rows)`
(`internal/handlers/report_render.go:956`). `rows` is one per tax type the ledger uses — here 4,
including `NONE` and `INPUT`, which have **zero** income-class lines (E1). `TaxCollected` can
only be non-nil when `measuredSales > 0` (`report.go:863`), so for those two rows the cell is
nil *by definition*, and `collectedRows` can never reach `len(rows)`. The same holds on the
purchase side for `OUTPUT`, which has no cost lines. A total is therefore unreachable for any
organisation that uses more than one rate and does not post to every rate on both sides — which
is every organisation. The predicate the Total wants is "every rate that has lines on this side
measured them", not "every rate at all".

**5.2.2 "Measured" is defined as `tax_amount <> 0`, which a 0% rate can never satisfy.**
The `NONE` rate is `Tax Exempt (0%)` (E7) and its 9 posted lines have a correct tax amount of
**zero**. `measured_purchases` counts `tax_amount <> 0`, so it is 0 of 9, so
`measuredPurchases == purchaseLines` is false, so `allPaid` is false forever — and with it the
Total row's Tax Paid and Net Tax. A rate whose correct measurement is zero must be measured, not
absent; the test should ask whether the line's tax was *supplied by its document*, which is a
question about the source, not about the value.

**5.2.3 The class split drops the input tax the ledger holds on asset accounts.**
`costAccountTypes` (`report.go:103–106`) is
`{DIRECTCOSTS, EXPENSE, OVERHEADS, DEPRECIATN, WAGES}`; `FIXED` is not in it. The organisation's
three capital lines carry **387.60** of input tax, posted to 820 and visible in Xero's own GL
under Computer Equipment 311.39 and Office Equipment 76.21 — but on accounts of class `FIXED`,
which the report counts in neither column (E6). So a report whose purchases side is
scrupulously measured still reports **810.19** of net tax where the ledger holds **422.59**,
overstating the liability by 387.60. The purchase-side columns and the tax columns need not share
one class list: Net Purchases is a P&L column and may reasonably exclude capital, but the tax
columns are a tax workpaper and cannot.

**What the report prints if only (b) is fixed, with the data as it is at S0:** nothing changes.
`measured_sales` is 0 on every income row and `measured_purchases` is 33 of 70 on `INPUT` and 0
on every other purchase row, so no rate has a fully-measured side, `collectedRows` and `paidRows`
are both 0, and the report prints exactly what section 2 captured — empty tax columns and the
same caveat. (b) is necessary and not sufficient: it is the reason a *correct* total will never
appear even after (a) is fixed.

### 5.3 (c) "Tax is not in the ledger" — **false**

Account 820 holds **−422.59** on 107 lines, decomposing with no remainder into the documents and
bank movements that produced it (§3), reproduced by Xero's captured Trial Balance, Balance Sheet
and General Ledger Detail, and carried on a `820` leg of **all 65** posted invoice journals and
all 33 taxed bank journals with the right sign (E10, E11). The tax is in the ledger.

### 5.4 The numbers each option would print

| Option | Per-rate sales tax cells | Total row | Net Tax | Against Xero |
| --- | --- | --- | --- | --- |
| **Today** (S0) | all empty | all empty | empty | no comparison possible |
| **(a) alone** | INPUT paid 1,583.86; OUTPUT collected 2,423.47; OUTPUT2 collected 14.33 / paid 43.75 | empty — the guard of §5.2.1/§5.2.2 refuses | only the OUTPUT2 row carries one: 14.33 − 43.75 = −29.42 | 387.60 short (capital input tax unseen) |
| **(b) alone** | all empty — no side is fully measured even after the guard is repaired | empty | empty | identical to today |
| **(c)** | — | — | — | the premise is false |
| **(a) + (b)** | as (a), plus NONE paid **0.00** (a measured zero, not an empty cell) | collected **2,437.80**, paid **1,627.61** | **810.19** | still 387.60 short |
| **(a) + (b) + class split widened to the tax columns** | INPUT paid **1,971.46**; others as above | collected **2,437.80**, paid **2,015.21** | **422.59** | **agrees with Xero to the cent, rate by rate** (§7) |

The last row is the target. Its `422.59` is not read off account 820 — it is produced by the
report's own measure once the measure is complete, and it lands on 820 independently. That is
what the report is supposed to be.

The verdict, then:

* **(a) is the defect that empties the columns.**
* **(b) is a real and independent defect — three of them — and without it (a)'s repair still
  prints no total.**
* **(c) is false.**
* (a) is the one to fix first, and it is the one the task asked to be specified exactly.

---

## 6. The backfill, exactly

### 6.1 Which rows

`gl_journal_lines` rows of journals with `source_type` in `('INVOICE','CREDITNOTE','EXPENSECLAIM')`
— 65 invoice + 5 credit-note + **3** expense-claim = **73 journals** — restricted to the **document
lines**: the line whose `tax_type` is not null and whose account is not 820. The control legs (610 / 800 / 801) and the
`820` legs are excluded; the 820 legs already hold the right figure and are the very thing the
backfill is being reconciled to. That is **83 lines today**: 74 invoice lines, 5 credit-note
lines, 4 expense-claim lines. Restricting further to `tax_amount = 0` selects the same 83, since
all 83 are 0 today. The simulation counts them by source and class:

```sql
SELECT now();
WITH acct AS (
  SELECT account_id, code, type,
         CASE WHEN type IN ('REVENUE','SALES') THEN 'S'
              WHEN type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES') THEN 'P'
         END AS side
    FROM accounts WHERE organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'
),
claim_journals AS (
  SELECT journal_id FROM gl_journals
   WHERE organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND source_type='EXPENSECLAIM'
),
claim_pairs AS (
  SELECT d.line_id AS doc_line_id, t.amount AS tax
    FROM (SELECT l.line_id, l.journal_id, row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) rn
            FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
           WHERE l.journal_id IN (SELECT journal_id FROM claim_journals) AND a.code NOT IN ('801','820')) d
    JOIN (SELECT l.line_id, l.journal_id, l.net_amount AS amount,
                 row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) rn
            FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
           WHERE l.journal_id IN (SELECT journal_id FROM claim_journals) AND a.code='820') t
      ON t.journal_id=d.journal_id AND t.rn=d.rn
),
gl AS (
  SELECT l.line_id, j.source_type, l.tax_type, l.net_amount, l.tax_amount AS tax_now,
         ac.side,
         CASE WHEN ac.side='S' THEN sign(-l.net_amount) ELSE sign(l.net_amount) END AS dir,
         COALESCE(il.tax_amount, cnl.tax_amount, cp.tax) AS tax_src,
         (il.line_item_id IS NOT NULL OR cnl.line_item_id IS NOT NULL OR cp.doc_line_id IS NOT NULL) AS sourced
    FROM gl_journal_lines l
    JOIN gl_journals j ON j.journal_id=l.journal_id
    JOIN acct ac       ON ac.account_id=l.account_id
    LEFT JOIN invoice_line_items il
           ON j.source_type='INVOICE' AND il.invoice_id=j.source_id
          AND il.account_id=l.account_id AND il.line_amount=abs(l.net_amount)
          AND COALESCE(il.description,'')=COALESCE(l.description,'')
    LEFT JOIN credit_note_line_items cnl
           ON j.source_type='CREDITNOTE' AND cnl.credit_note_id=j.source_id
          AND cnl.account_code=ac.code AND cnl.line_amount=abs(l.net_amount)
          AND COALESCE(cnl.description,'')=COALESCE(l.description,'')
    LEFT JOIN claim_pairs cp ON cp.doc_line_id=l.line_id
   WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'
     AND COALESCE(l.tax_type,'')<>''
)
SELECT source_type, tax_type, side,
       count(*) lines,
       count(*) FILTER (WHERE tax_now<>0) AS n_now,
       sum(tax_now) AS tax_now,
       count(*) FILTER (WHERE back<>0) AS n_after,
       sum(back) AS tax_after,
       count(*) FILTER (WHERE NOT sourced) AS unsourced
  FROM (SELECT g.*, COALESCE(tax_src, tax_now, 0) * dir AS back FROM gl g) g
 GROUP BY 1,2,3 ORDER BY 1,2,3;
```

```
              now
-------------------------------
 2026-09-11 19:27:59.038142+02

   source_type   | tax_type | side | lines | n_now | tax_now | n_after | tax_after | unsourced
-----------------+----------+------+-------+-------+---------+---------+-----------+-----------
 BANKTRANSACTION | INPUT    | P    |    33 |    33 | 74.1600 |      33 |   74.1600 |        33
 BANKTRANSACTION | NONE     | P    |     6 |     0 |  0.0000 |       0 |    0.0000 |         6
 CREDITNOTE      | INPUT    | P    |     2 |     0 |  0.0000 |       2 |  -22.5700 |         0
 CREDITNOTE      | OUTPUT   | S    |     2 |     0 |  0.0000 |       2 |  -82.5000 |         0
 CREDITNOTE      | OUTPUT2  | S    |     1 |     0 |  0.0000 |       1 |   -1.7500 |         0
 EXPENSECLAIM    | INPUT    | P    |     4 |     0 |  0.0000 |       4 |   13.7500 |         0
 INVOICE         | INPUT    | P    |    31 |     0 |  0.0000 |      31 | 1518.5200 |         0
 INVOICE         | INPUT    |      |     3 |     0 |  0.0000 |       3 |  387.6000 |         0
 INVOICE         | NONE     | P    |     2 |     0 |  0.0000 |       0 |    0.0000 |         0
 INVOICE         | OUTPUT   | S    |    33 |     0 |  0.0000 |      33 | 2505.9700 |         0
 INVOICE         | OUTPUT2  | P    |     1 |     0 |  0.0000 |       1 |   43.7500 |         0
 INVOICE         | OUTPUT2  | S    |     4 |     0 |  0.0000 |       4 |   16.0800 |         0
```

That is the S0 reading; at S1 the two `BANKTRANSACTION` rows read 34 and 7 lines (the other
session's journals 1613 and 1614, §8), and no amount in the block moves.

Read the `unsourced` column as "lines this simulation has no document for". Every `INVOICE`,
`CREDITNOTE` and `EXPENSECLAIM` row reads **0** — those 83 lines all find their document. The
bank rows read 33 and 6 not because a document is missing but because the simulation does not
look for one: those lines already carry the figure the import gave them, so the backfill leaves
them alone (§6.3). The `INVOICE | INPUT | ` row with no `side` is the three capital lines of E6:
`side` is NULL because `FIXED` is in neither of the report's class lists, and they are the
387.60 of §6.5. The `tax_after` column sums to 74.16 + 1,518.52 + 387.60 + 2,505.97 + 43.75 + 16.08
− 22.57 − 82.50 − 1.75 + 13.75 = **4,453.01**, and `n_after` to 114 lines: the **81** document
lines that receive a value (83 minus the two `NONE` invoice lines, whose correct tax is zero)
plus the 33 bank lines that already carried one. 81 + 33 = 114.

Bank lines are **not** written. 33 of them already carry the right figure (E2/E11); the 6 `NONE`
lines (7 at S1) must stay 0 because their rate is 0%; and the two lines the other session added
at 19:30 have no document to source them from at all (§6.4).

### 6.2 Which source column, and the join key

| Journal `source_type` | Source column | Join to the document | Per-line key |
| --- | --- | --- | --- |
| `INVOICE` (both ACCREC and ACCPAY — one `invoices` table, `type` distinguishes them) | `invoice_line_items.tax_amount` | `gl_journals.source_id = invoice_line_items.invoice_id` | `invoice_line_items.account_id = gl_journal_lines.account_id` **and** `invoice_line_items.line_amount = abs(gl_journal_lines.net_amount)` **and** `coalesce(invoice_line_items.description,'') = coalesce(gl_journal_lines.description,'')` |
| `CREDITNOTE` | `credit_note_line_items.tax_amount` | `gl_journals.source_id = credit_note_line_items.credit_note_id` | `accounts.code = credit_note_line_items.account_code` (the table has `account_code` but **no** `account_id`) **and** `line_amount = abs(gl_journal_lines.net_amount)` **and** descriptions equal |
| `EXPENSECLAIM` | **no source table** — the sibling `820` line of the same journal | within the journal, by ordinal | `row_number() over (partition by journal_id order by line_id)` on the non-801/820 lines matched to the same ordinal on the 820 lines |

The invoice and credit-note keys are **verified 1:1**: of the 74 document lines of the 65 posted
invoices and the 5 credit-note lines, **74 and 5 match exactly one source line item, 0 match none
and 0 match more than one** (§3.9, E15). Nothing in the join depends on a guess.

Expense claims are the one soft spot and it must be stated rather than smoothed over. The claim
journals were written by 00024 with `source_key` NULL, so **`gl_journals.source_id` is NULL for
all three of them** — verified:

```sql
SELECT now();
SELECT j.journal_number, j.source_type, j.source_id IS NULL AS source_id_null, j.reference
  FROM gl_journals j
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.source_type='EXPENSECLAIM' ORDER BY j.journal_number;
SELECT count(*) AS receipts, (SELECT count(*) FROM receipt_line_items) AS receipt_lines FROM receipts;
```

```
              now
-------------------------------
 2026-09-11 19:33:06.263556+02

 journal_number | source_type  | source_id_null | reference
----------------+--------------+----------------+-----------
           1570 | EXPENSECLAIM | t              |
           1571 | EXPENSECLAIM | t              |
           1572 | EXPENSECLAIM | t              |

 receipts | receipt_lines
----------+---------------
        0 |             0
```

so a join by document id is impossible, and `receipts` / `receipt_line_items` — the only table
that could hold a claim's per-line tax — are empty. The sibling `820` leg is the only surviving
record (E16), and the migration's own key structure is `line/N` beside `tax/N`
(`migrations/00024_xero_reference_source_documents.sql:650`). Pairing by ordinal recovers the
journal total exactly. The caveat: the 820 legs have NULL descriptions and no ordinal is
persisted, so `line_id` — a UUIDv5 hash of the journal and line keys — is the only available
order, and it is not document order. Within journal 1570, whose two claim lines carry 15.61 and
16.63 and whose two 820 legs carry 1.29 and 1.37, the two values **may be swapped**. The journal
total (2.66) is exact, the report sums the journal, and no printed figure depends on which of
the two lines gets which; per-line presentation would. Stated, not hidden.

### 6.3 The sign rule

```sql
tax_amount = source_tax * CASE WHEN <income class> THEN sign(-net_amount) ELSE sign(net_amount) END
```

In words: **positive when the line moves in the document's own direction, negative when the
document reverses it** — a sale on an income account, a purchase on a cost account → `+`; a
customer credit note on an income account, a supplier credit on a cost account → `−`.

This is not a choice, and it is checkable three ways:

1. **The 33 bank lines the import did fill** all sit on cost lines with a positive `net_amount`
   and all carry a positive magnitude (E2) — the `+` case. The rule reproduces the only
   convention this ledger actually contains.
2. **The credit-note journals reverse the sign, and Xero's capture says so.** The ACCRECCREDIT
   revenue line of CN-0014 is `200 REVENUE net_amount +500.00` beside `820 +41.25`, i.e. the
   exact mirror of the invoice's `200 REVENUE −500.00` beside `820 −41.25`; a magnitude rule
   would *add* 84.25 to Tax Collected instead of subtracting it. Xero's own GL Detail files the
   credit-note tax on the opposite side of the same rate (`Tax on Consulting` shows 2,505.97
   credited and **82.50 debited**, §7), which is the same statement.
3. **It is the only rule under which the report's raw sums land on the ledger's own 820
   movements** — the identity `820 = −Collected + Paid` holds after the backfill up to exactly
   the capital residual of §6.5, and section 7 shows the resulting per-rate figures agree with
   Xero's captured GL to the cent.

Note that the product's own posting writers pass a line item's `TaxAmount` through as a
magnitude (`internal/repository/gl.go:207`, `:314`, `:490`) and carry the direction in
`net_amount` (`signedTaxFromNet`, `internal/repository/gl.go:132–137`). For an ordinary sale or
bill that is the same answer; for a credit note it is not, and the report would overstate
collected tax by the credit-note tax. That is an observation about the writers, offered as a
warning for whoever implements the backfill — **no code was changed to establish it.**

### 6.4 Lines whose document cannot be found

* **Document lines: none.** 0 of 74 invoice lines and 0 of 5 credit-note lines fail the join
  (E15). There is no orphan to invent a value for.
* **The 6 (S0) / 7 (S1) bank `NONE` lines** stay 0, correctly: a 0% rate's tax is zero, and they
  are not unmeasured, they are measured as zero. The report's guard cannot say so
  (§5.2.2) — that is the report's problem, not the data's.
* **The two lines the other session added at 19:30** are the only rows in the ledger the backfill
  cannot source: journal 1613's `404` line (`NONE`, 15.00) and journal 1614's `453` line
  (`INPUT`, 15.50) carry a tax type, carry `tax_amount` 0, have no source document, and have **no
  820 sibling** in their journal. They must keep 0 and be **counted as unmeasured**, not summed
  as if measured, and their rate's cell must stay empty until someone supplies the tax. The
  report's guard already behaves this way; what it lacks is the sentence that would name the
  count.
* **DRAFT/VOIDED/DELETED documents** post no journal, so no line exists to backfill and nothing
  is owed to them: their 83.84 (ACCREC draft) and 222.22 (ACCPAY voided/deleted) must not be
  added anywhere. Item 4 and item 5 of the bridge remove them for exactly that reason.

### 6.5 What the backfilled totals agree with, and the residual that must be stated

The backfill was run as a **read-only CTE that substitutes a `tax_amount` into a copy of the
report's own query** — nothing was written. The query is in §6.6. This is its E13 output, and the
E14 output of the Total cell the renderer derives from it, verbatim, both read at 19:35:17:

```
===== E13  what the report itself would print under option (a): the report query verbatim, over the ledger with the backfill applied in a read-only CTE, nothing written
              now
-------------------------------
 2026-09-11 19:35:17.469362+02

 tax_type |           rate            | net_sales  | net_purchases | sales_lines | purchase_lines | measured_sales | measured_purchases | cell_collected | cell_paid
----------+---------------------------+------------+---------------+-------------+----------------+----------------+--------------------+----------------+-----------
 INPUT    | Tax on Purchases (8.25%)  |          0 |    19214.3500 |           0 |             71 |              0 |                 70 |                |
 NONE     | Tax Exempt (0%)           |          0 |     1838.6000 |           0 |              9 |              0 |                  0 |                |
 OUTPUT   | Tax on Consulting (8.25%) | 29375.4300 |             0 |          35 |              0 |             35 |                  0 |      2423.4700 |
 OUTPUT2  | Tax on Goods (8.75%)      |   163.7500 |      500.0000 |           5 |              1 |              5 |                  1 |        14.3300 |   43.7500

===== E14  the Total row the report builds from those cells (report_render.go:956-971)
              now
-------------------------------
 2026-09-11 19:35:17.478301+02

 rates | rates_with_collected | rates_with_paid |       current_guard_collected       | current_total_collected | current_total_paid |                 corrected_guard                  | corrected_total_collected | corrected_total_paid | total_collected_cell | total_paid_cell
-------+----------------------+-----------------+-------------------------------------+-------------------------+--------------------+--------------------------------------------------+---------------------------+----------------------+----------------------+-----------------
     4 |                    2 |               1 | current guard: collectedRows=rates? | f                       | f                  | corrected guard: only rates that have such lines | t                         | f                    |            2437.8000 |         43.7500
```

Read the two together and §5.2 is arithmetic rather than argument: **2** of the 4 rates support
Tax Collected and **1** of the 4 supports Tax Paid, so the Total row's tax cells stay empty even
with perfect data; the total guard's text says `collectedRows = rates?` → `f`, and a guard that
counts only the rates having lines on the side being totalled (`corrected`) reaches `t` for
collected and *still* `f` for paid, because `Tax Exempt (0%)` has purchase lines whose correct tax
is zero and §5.2.2 cannot measure a zero.

`net_purchases` and `purchase_lines` in E13 are the S1 composition: `INPUT` reads 71 lines of which
70 are measured, because journal 1614's unsourced line of §8 arrived after S0. At S0 the same
query read `INPUT` 19,198.85 / **70 of 70 measured**, so `cell_paid` printed **1,583.86**; the extra
line now suppresses that cell, which is §6.4's behaviour working as it should. `Tax Exempt` reads 9
lines at S1 against 8 at S0 for the same reason. No tax amount in E13 moved: 2,423.47, 14.33 and
43.75 are identical at both readings.

Summed by the side the report defines:

| | Amount | Composed of |
| --- | --- | --- |
| Tax Collected | **2,437.80** | invoice OUTPUT 2,505.97 + invoice OUTPUT2 16.08 − ACCRECCREDIT 82.50 − ACCRECCREDIT OUTPUT2 1.75 |
| Tax Paid, report's class split | **1,627.61** | invoice INPUT 1,518.52 + bank 74.16 + claims 13.75 + invoice OUTPUT2 43.75 − ACCPAYCREDIT 22.57 |
| **Net Tax, report's class split** | **810.19** | 2,437.80 − 1,627.61 |
| account 820 | **−422.59** | §3 |
| **residual** | **387.60** | 810.19 − 422.59 = the three capital lines of E6: 76.21 + 148.87 + 162.52 |
| Tax Paid with the capital lines in | **2,015.21** | 1,627.61 + 387.60 |
| **Net Tax with the capital lines in** | **422.59** | **= account 820, exactly** |

So: **the backfilled totals do NOT agree with 820 — they leave 387.60 — and that difference must
be stated by the report, not hidden.** It is not a data error: the 387.60 is real, it is on 820,
Xero's own GL shows it under Computer Equipment 311.39 and Office Equipment 76.21, and the
backfill cannot make it appear because the report's class list has no `FIXED` in it. The honest
report states it; the fix that removes it is §5.2.3. The sentence the report should carry, once
the backfill is in, is of the shape:

> Tax Collected and Tax Paid are the tax recorded on the documents behind the posted journal
> lines. Tax Paid does not include **387.60** of input tax posted to asset accounts (classes
> outside this report's income and cost columns); account 820 Sales Tax reads **422.59** for the
> period, which is this report's Net Tax plus that input tax.

and after §5.2.3 it should read simply that Net Tax equals account 820.

### 6.6 The SQL — the read-only simulation, and the shape of the write

This is what was actually run. It writes nothing: the backfilled value lives in a CTE and the
report's query is run over it.

```sql
SELECT now();
WITH acct AS (
  SELECT account_id, code, type,
         CASE WHEN type IN ('REVENUE','SALES') THEN 'S'
              WHEN type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES') THEN 'P'
         END AS side
    FROM accounts WHERE organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'),
claim_journals AS (
  SELECT journal_id FROM gl_journals
   WHERE organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND source_type='EXPENSECLAIM'),
claim_pairs AS (
  SELECT d.line_id AS doc_line_id, t.amount AS tax
    FROM (SELECT l.line_id, l.journal_id, row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) rn
            FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
           WHERE l.journal_id IN (SELECT journal_id FROM claim_journals) AND a.code NOT IN ('801','820')) d
    JOIN (SELECT l.line_id, l.journal_id, l.net_amount AS amount,
                 row_number() OVER (PARTITION BY l.journal_id ORDER BY l.line_id) rn
            FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
           WHERE l.journal_id IN (SELECT journal_id FROM claim_journals) AND a.code='820') t
      ON t.journal_id=d.journal_id AND t.rn=d.rn),
lines AS (   -- gl_journal_lines with the backfill substituted for tax_amount
  SELECT l.line_id, l.journal_id, l.account_id, l.tax_type,
         CASE WHEN COALESCE(l.tax_amount,0) <> 0 THEN l.tax_amount
              ELSE COALESCE(src.tax_src,0) * CASE WHEN ac.side='S' THEN sign(-l.net_amount) ELSE sign(l.net_amount) END
         END AS tax_amount,
         l.net_amount
    FROM gl_journal_lines l
    JOIN gl_journals j ON j.journal_id=l.journal_id
    JOIN acct ac       ON ac.account_id=l.account_id
    LEFT JOIN invoice_line_items il
           ON j.source_type='INVOICE' AND il.invoice_id=j.source_id
          AND il.account_id=l.account_id AND il.line_amount=abs(l.net_amount)
          AND COALESCE(il.description,'')=COALESCE(l.description,'')
    LEFT JOIN credit_note_line_items cnl
           ON j.source_type='CREDITNOTE' AND cnl.credit_note_id=j.source_id
          AND cnl.account_code=ac.code AND cnl.line_amount=abs(l.net_amount)
          AND COALESCE(cnl.description,'')=COALESCE(l.description,'')
    LEFT JOIN claim_pairs cp ON cp.doc_line_id=l.line_id
    CROSS JOIN LATERAL (SELECT COALESCE(il.tax_amount, cnl.tax_amount, cp.tax) AS tax_src) src
   WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'),
agg AS (   -- verbatim from internal/repository/report.go:809-831
  SELECT l.tax_type,
         COALESCE(SUM(CASE WHEN a.type IN ('REVENUE','SALES') THEN -l.net_amount END),0) AS net_sales,
         COALESCE(SUM(CASE WHEN a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES') THEN l.net_amount END),0) AS net_purchases,
         COALESCE(SUM(CASE WHEN a.type IN ('REVENUE','SALES')             AND l.tax_amount <> 0 THEN l.tax_amount END),0) AS tax_collected,
         COALESCE(SUM(CASE WHEN a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES') AND l.tax_amount <> 0 THEN l.tax_amount END),0) AS tax_paid,
         COUNT(*) FILTER (WHERE a.type IN ('REVENUE','SALES')) AS sales_lines,
         COUNT(*) FILTER (WHERE a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES')) AS purchase_lines,
         COUNT(*) FILTER (WHERE a.type IN ('REVENUE','SALES')             AND l.tax_amount <> 0) AS measured_sales,
         COUNT(*) FILTER (WHERE a.type IN ('DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGES') AND l.tax_amount <> 0) AS measured_purchases
    FROM lines l
    JOIN gl_journals j ON j.journal_id = l.journal_id
    JOIN accounts a ON a.account_id = l.account_id
   WHERE j.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
     AND j.journal_date BETWEEN DATE '2026-01-01' AND DATE '2026-12-31'
     AND COALESCE(l.tax_type,'') <> ''
   GROUP BY l.tax_type)
SELECT a.tax_type,
       (SELECT tr.name FROM tax_rates tr WHERE tr.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND tr.tax_type=a.tax_type ORDER BY tr.name LIMIT 1) AS rate,
       a.net_sales, a.net_purchases, a.sales_lines, a.purchase_lines, a.measured_sales, a.measured_purchases,
       CASE WHEN a.measured_sales > 0 AND a.measured_sales = a.sales_lines THEN a.tax_collected END AS cell_collected,
       CASE WHEN a.measured_purchases > 0 AND a.measured_purchases = a.purchase_lines THEN a.tax_paid END AS cell_paid
  FROM agg a ORDER BY a.tax_type;
```

The write it implies — **stated for the implementer, not executed, and not part of this task** —
is that same expression applied as an `UPDATE` over the 83 document lines of the 73 document
journals, restricted to those whose document was found, with the unsourced ones left at 0 and
reported. It is a statement about `gl_journal_lines.tax_amount` only: no `net_amount`,
`gross_amount`, `account_id` or journal changes, so no account balance moves and account 820 is
untouched. Whatever implementer takes it on should note that (a) alone is not enough — §5.2's
three report defects must go with it, or the Total row stays empty — and that the sign rule of
§6.3 differs from what the product's own writers do for credit notes.

---

## 7. Which proposed figures Xero's capture can check

The Sales Tax Report itself was never captured (`docs/xero-reference/README.md` item 8), so
there is no captured *Sales Tax Report* to lay side by side. But Xero's captured **General Ledger
Detail** carries a **Tax column on every ledger line and a total for every account section**, and
its `Total Sales` row and its `Total Sales Tax` row give the two figures the report is trying to
print:

```
docs/xero-reference/general-ledger-detail.txt:387
Total Sales				1,019.95	30,559.13	(29,539.18)	(2,437.80)

docs/xero-reference/general-ledger-detail.txt:499
Total Sales Tax				2,122.03	2,544.62	(422.59)	-
```

Summing that tax column over the expense and asset sections, and grouping it by the file's own
rate-name column:

```sh
python3 /tmp/salestax/gl_tax.py      # reads docs/xero-reference/general-ledger-detail.txt
```

```
per account section, the tax column where it is not zero:
  Advertising                        796.70
  Cleaning                            91.58
  Computer Equipment                 311.39
  Consulting & Accounting              7.17
  Freight & Courier                    9.53
  General Expenses                    13.72
  Light, Power, Heating               27.71
  Motor Vehicle Expenses              53.99
  Office Equipment                    76.21
  Office Expenses                     73.66
  Printing & Stationery                7.79
  Purchases                           64.02
  Rent                               270.09
  Repairs and Maintenance            156.49
  Sales                            -2437.80
  Telephone & Internet                19.50
  Travel - National                   35.66
  SUM, expense/asset sections       2015.21

per tax rate name:
  Tax Exempt             collected       0.00   paid       0.00   lines   9
  Tax on Consulting      collected    2505.97   paid      82.50   lines  35
  Tax on Goods           collected      16.08   paid      45.50   lines   6
  Tax on Purchases       collected      22.57   paid    1994.03   lines  73

  collected total    2544.62
  paid total         2122.03
  net tax            -422.59
```

Every one of those figures is the backfill's own figure. Rate by rate:

| Rate | Xero GL Detail (frozen capture) | The backfill, from `invoice_line_items` / `credit_note_line_items` / the 820 legs |
| --- | --- | --- |
| Tax on Consulting (8.25%) | 2,505.97 credited, 82.50 debited, **35 lines** | invoice OUTPUT/S 2,505.97, credit-note OUTPUT/S 82.50, **35 lines** |
| Tax on Goods (8.75%) | 16.08 credited, 45.50 debited, **6 lines** | invoice OUTPUT2/S 16.08, credit-note OUTPUT2/S 1.75, invoice OUTPUT2/P 43.75, **6 lines** |
| Tax on Purchases (8.25%) | 22.57 credited, 1,994.03 debited, **73 lines** | credit-note INPUT/P 22.57; invoice INPUT/P 1,518.52 + capital 387.60 + bank 74.16 + claims 13.75 = 1,994.03; **73 lines** |
| Tax Exempt (0%) | 0.00, **9 lines** | the `NONE` lines, 0.00, 9 lines at S1 (8 at S0 — see below) |
| **Total** | **collected 2,437.80** (`Total Sales` tax column), **paid 2,015.21**, **net 422.59** | collected 2,437.80, paid 2,015.21, net 422.59 |

Two presentations differ and the nets do not. For `Tax on Goods` and `Tax on Purchases` Xero's GL
files a credit note's tax on the side opposite to the one the report's class split puts it on
(`Tax on Goods`: Xero 16.08 credited / 45.50 debited, the report 14.33 collected / 43.75 paid —
both −29.42; `Tax on Purchases`: Xero 22.57 credited / 1,994.03 debited, the report
1,971.46 paid against 22.57 less collected). Xero's GL groups by the *direction of the 820
movement within the rate*; the report groups by the *class of the account the line hit*. Per-rate
**net** tax is identical either way, and it is the net that reconciles to 820.

So the answer to "does a captured Xero figure exist to check this" is a qualified **yes**, and it
is the strongest check in this document: Xero's own tax column, per rate and in total, is
reproduced to the cent by a backfill derived entirely from this organisation's own documents.

For each figure on its own:

| Figure | Captured Xero figure? | Where | Agrees? |
| --- | --- | --- | --- |
| account 820 Sales Tax **422.59** | yes | `trial-balance.txt:33`, `balance-sheet.txt:26`, `general-ledger-detail.txt:499` and `:551` | **yes**, to the cent |
| 820 gross **2,122.03 Dr / 2,544.62 Cr** | yes | `general-ledger-detail.txt:499` | **yes**, to the cent |
| **Tax Collected 2,437.80** (after the backfill) | yes | `general-ledger-detail.txt:387`, the `Total Sales` row's tax column | **yes**, to the cent |
| **Tax Paid 2,015.21** (complete measure) | yes | the sum of the tax column over the expense/asset sections of `general-ledger-detail.txt`; the 387.60 of it is in the `Computer Equipment` and `Office Equipment` section totals | **yes**, to the cent |
| **Net Tax 422.59** (complete measure) | yes | as 820 | **yes**, to the cent |
| Tax Paid **1,627.61** (report's class split) | **no** | Xero's own purchases total is 2,015.21; the report's class list has no `FIXED` in it | **no — 387.60 short**, and the 387.60 is named in the capture |
| Net Tax **810.19** (report's class split) | **no** | — | **no — 387.60 short** |
| per-rate: Tax on Consulting 2,423.47; Tax on Goods −29.42; Tax on Purchases 1,971.46; Tax Exempt 0.00 | yes | the per-rate parse above | **yes**, each to the cent |
| **Net Sales 29,539.18** | yes | `trial-balance.txt:10` (`200 Sales … 29,539.18`), `profit-and-loss.txt:11` | **yes**, to the cent |
| **Net Purchases 21,522.45** (at S0; 21,552.95 at S1) | no — not for this report | Xero's P&L is Cost of Sales 775.98 + Total Operating Expenses 20,496.47 = 21,272.45 | differs by exactly the **250.00** of another session's bank coding (§8); the further **30.50** at S1 is that session's journals 1613/1614 |
| 2,605.89 / 2,172.09 / 106.82 / 13.75 (source CSV sums) | checkable against the capture, not printed by Xero | `migrations/data/xero/{invoice,bill,credit-note,expense-claim}*.csv` | **yes**, they are the CSVs' own column sums |
| **433.80** | **no** | it is 2,605.89 − 2,172.09, a difference of two mixed-status populations | meaningless as a tax figure |
| **897.35** | **no** | not in any captured file, CSV, or query result (§4.2) | not reproducible from anything |
| Cash-basis **1,844.56 / 1,474.01 / 370.55** | yes | `cash-summary.txt:35–37` | a different measure; the Sales Tax report can never print these (§1.3) |

**One difference in the capture that I am stating rather than explaining away.** Xero's GL Detail
tags **9** lines `Tax Exempt`; the ledger had 8 at S0 and has 9 at S1 (the ninth is the other
session's journal 1613, not an import fix). The two lines Xero tags and the ledger does not are
the Conversion Balance Journal pair — `Business Bank Account` 4,130.98 and `Historical
Adjustment` 4,130.98 — which this ledger carries on accounts 090 and 840 with `tax_type` NULL
(`gl_journals.journal_number` 1603, `reference` `Conversion Balance`). Both land on balance-sheet
accounts, their rate is 0%, and Xero's own tax column is `-` on both. So this is a difference in
line tagging, it cannot move any figure any report prints, and the only way it could ever be
made to matter is by §5.2.2's broken "measured means non-zero" test counting them.

---

## 8. The other session's writes

The worktree and the database are shared. Two events belong to the other session and are
neither imported nor chased:

**(1) The 250.00 on `091` / `400`.** Journal `journal_number` 52, dated 2026-03-05, reference
`savings demo`, created `2026-09-11 16:37:09.120703+02`, legs `091 −250.00 [tax 0.00] | 400 +250.00
[tax 0.00]`. Xero's reference has no `091` account at all. It carries `tax_type` `NONE` on the
`400` line, moves the report's `NONE` row's Net Purchases, and accounts for the whole of the
250.00 by which Net Purchases 21,522.45 exceeds Xero's 21,272.45. It posts **no 820 leg**, so
account 820 is 422.59 with or without it, and no tax figure in this document moves because of
it.

**(2) Four bank journals, created while this document was being written.** Captured verbatim:

```sql
SELECT now();
SELECT j.journal_number, j.journal_date, j.source_type, j.created_date_utc, j.reference,
       string_agg(a.code || ' ' || l.net_amount::text || ' [tax ' || l.tax_amount::text || ']', ' | ' ORDER BY a.code) AS legs
  FROM gl_journals j JOIN gl_journal_lines l ON l.journal_id=j.journal_id JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.created_date_utc > '2026-09-11 19:25:00+02'
 GROUP BY 1,2,3,4 ORDER BY j.created_date_utc;
```

```
              now
-------------------------------
 2026-09-11 19:30:58.834962+02

 journal_number | journal_date |   source_type   |       created_date_utc        | reference |                         legs
----------------+--------------+-----------------+-------------------------------+-----------+------------------------------------------------------
           1612 | 2026-09-10   | BANKTRANSACTION | 2026-09-11 19:30:45.665233+02 |           | 090 −12.00 [tax 0.00] | 200 +12.00 [tax 0.00]
           1613 | 2026-09-10   | BANKTRANSACTION | 2026-09-11 19:30:47.449074+02 | Acct fee  | 090 −15.00 [tax 0.00] | 404 +15.00 [tax 0.00]
           1614 | 2026-09-10   | BANKTRANSACTION | 2026-09-11 19:30:50.762977+02 |           | 090 −15.50 [tax 0.00] | 453 +15.50 [tax 0.00]
           1615 | 2026-09-09   | BANKTRANSACTION | 2026-09-11 19:30:53.904777+02 |           | 090 −12.00 [tax 0.00] | 200 +12.00 [tax 0.00]
```

Their exact effect on this document's figures, as re-read at S1:

| | S0 | S1 |
| --- | --- | --- |
| account 820 | −422.59 / 107 lines | **−422.59 / 107 lines** — unchanged, these journals post no 820 leg |
| lines with `tax_amount <> 0` | 33 lines / 74.16 | **33 lines / 74.16** — unchanged |
| tax-typed lines | 122 | 124 (1613 adds a `NONE`, 1614 an `INPUT`) |
| the bridge of §3 | ends at −422.59 | **ends at −422.59**, identical output |
| the backfill's totals | collected 2,437.80 / paid 1,627.61 | **identical**; only line counts move |
| report's `NONE` row Net Purchases | 1,823.60 | 1,838.60 |
| report's `INPUT` row Net Purchases | 19,198.85 | 19,214.35 |
| `INPUT` row's Tax Paid cell after a backfill | 1,583.86 (70 of 70 measured) | **empty** (70 of 71 — journal 1614 has no document) |

Nothing in the tax analysis moves; the only change of substance is that journal 1614's presence
suppresses the `INPUT` row's Tax Paid cell after a backfill, which is the correct behaviour for a
line with no source document (§6.4). Note also that journals 1613 and 1614 carry a `tax_type`
with `tax_amount` 0 and no 820 sibling — the same shape as the import defect of §5.1, arriving
from the application's own bank-coding path rather than from a migration. That is an observation
about a live write, not a finding of this task.

---

## 9. Reproducing everything

```sh
cd /Users/shurco/orca/workspaces/goXero/bonefish

# every SQL statement in this document, in order, with the now() it was read at
docker exec -i bonefish-postgres-1 psql -U goxero -d goxero -P footer=off -f - < /tmp/salestax/evidence.sql
docker exec -i bonefish-postgres-1 psql -U goxero -d goxero -P footer=off -f - < /tmp/salestax/evidence2.sql
docker exec -i bonefish-postgres-1 psql -U goxero -d goxero -P footer=off -f - < /tmp/salestax/bridge.sql
docker exec -i bonefish-postgres-1 psql -U goxero -d goxero -P footer=off -f - < /tmp/salestax/joincheck.sql
docker exec -i bonefish-postgres-1 psql -U goxero -d goxero -P footer=off -f - < /tmp/salestax/optionab.sql

# Xero's own tax column, per rate and per account section
python3 /tmp/salestax/gl_tax.py

# the report as the API renders it
TOK=$(curl -s -X POST http://localhost:8080/api/auth/login -H 'Content-Type: application/json' \
      -d '{"email":"admin@demo.local","password":"admin123"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')
curl -s "http://localhost:8080/api/v1/reports/sales-tax?fromDate=2026-01-01&toDate=2026-12-31" \
     -H "Authorization: Bearer $TOK" -H "Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2"
```

Those five `.sql` files and their `.out` companions are in `/tmp/salestax/` and are not part of
the repository. Every statement whose result this document quotes is reproduced above with its
SQL and its output verbatim. Two of the labelled statements are **not** quoted, and for stated
reasons rather than omission: `E8` lists every journal created after the reference import began,
which section 8 answers with a narrower query of its own, and `E17` restates the arithmetic of
the two paragraphs above it — `2,437.80 − (1,627.61 + 387.60) = 422.59`, against a live read of
account 820 — as a one-row query whose left-hand sides are literals from `E13`, so the
arithmetic is in the body of section 6.5 and the query adds nothing to it.

---

## 10. Summary

1. **The report's measure** is the per-line `tax_amount` of posted lines carrying a tax type,
   grouped by rate and split by the account class the line hit — a **document-basis** figure. It
   is meant to reproduce Xero's Sales Tax Report, not the Cash Summary's cash-basis
   1,844.56 / 1,474.01 / 370.55, which no code path in it can produce (`report.go:807–844`).
2. **Every unit of tax is traceable and reconciles.** Account 820 holds −422.59 on 107 lines;
   the bridge from 2,605.89 − 2,172.09 = 433.80 walks to it in eleven numbered steps and lands on it
   with **no remainder**. All 65 posted invoice journals post their document's tax to 820 with
   the right sign, all 33 taxed bank journals agree with their coded line, and all 74 invoice and
   5 credit-note document lines join to exactly one source line item.
3. **The defect is (a): the import left `tax_amount` at 0 on every document line.** (b) is a
   second, independent defect — the report's Total guard can never be satisfied, its
   "measured = non-zero" test makes a 0% rate unmeasurable, and its class split drops 387.60 of
   input tax that the ledger holds. (c) is false.
4. **The backfill** is `invoice_line_items.tax_amount` and `credit_note_line_items.tax_amount`
   joined by `source_id` plus account, amount and description, and the sibling 820 leg for the
   expense claims, signed by the document's direction. It fills 83 lines in 73 journals and
   leaves **no line unsourced**. It moves no account balance.
5. **It reconciles to 820 only when the capital lines are let in.** With the report's class split
   as written the backfilled totals leave **387.60**, which is exactly the input tax on the three
   `FIXED`-class lines and is visible in Xero's own capture. With them in, Tax Collected
   **2,437.80**, Tax Paid **2,015.21**, Net Tax **422.59** — the last of which is account 820, to
   the cent.
6. **Xero's capture can check this**, and does: `general-ledger-detail.txt` carries a tax column
   whose per-rate totals (2,423.47 / −29.42 / 1,971.46 / 0.00), whose `Total Sales` tax cell
   2,437.80 and whose expense-section sum 2,015.21 are all reproduced by the backfill to the
   cent, and whose net 422.59 is Xero's Sales Tax balance.
7. **The two conflicting pairings are both disposed of.** 433.80 compares two mixed-status
   populations and 422.59 is the right comparison; 897.35 is not a figure in any data this
   organisation has.
