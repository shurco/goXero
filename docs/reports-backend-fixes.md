# Report backend fixes — four endpoints that answered with the wrong report

Four report endpoints returned `200` with a payload that was not the report they
name. This is what each one did, what it does now, and the evidence for both.
The last section answers two follow-up questions about the same reports — the
`/reports` index and the aged reports' ageing columns.

Everything below was produced against the running API on the demo organisation
(`6823b27b-c48f-4099-bb27-4202a4f496a2`), whose ledger is the real one from
`migrations/`:

```sh
T=$(cat /tmp/gx.token)
H=(-H "Authorization: Bearer $T" -H "Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2")
```

| | before | after |
|---|---|---|
| Trial Balance | measured one period; dropped every account with no movement in it | balance sheet accounts at closing balance, profit and loss at year-to-date movement, every account with a balance rendered |
| Cash Summary | the Bank Summary body under a different `ReportName` | its own aggregation in Xero's layout: income, less expenses, other cash movements and tax movements, closed by Net Cash Movement (the sum of the rendered rows, i.e. the bank accounts' own movement) and the balances either side of the window |
| Aged … by Contact | the aged *summary* report, with the summary's `ReportID` and `ReportName` | one section per contact with that contact's invoices and subtotal, a grand total, and its own report identity |
| BAS / Sales Tax | a header row and nothing else, under a report name the route did not match | one row per tax rate the organisation's own coded journals carry, plus a Total row that adds up to them |

The rebuilt binary was run on **port 8099** (`/tmp/gx-server2`); port 8080 belongs
to the coordinator and was not touched. The "before" payloads come from a binary
built from `HEAD` in a scratch tree and run on **port 8098**:

```sh
git archive HEAD | tar -x -C /tmp/gx-before
(cd /tmp/gx-before && go build -o /tmp/gx-before-server ./cmd/server)
go build -o /tmp/gx-server2 ./cmd/server
SERVER_PORT=8098 /tmp/gx-before-server &
SERVER_PORT=8099 /tmp/gx-server2 &
```

---

## Defect 1 — Trial Balance measured the wrong thing and dropped a whole account type

**Before.** `/reports/trial-balance` rendered a row only for an account with
movement inside the reported period, and filled the YTD columns with that
account's year-to-date *line movement* — both sides of the account added up
separately. An account whose postings all fall outside the period disappeared
from the report and from its total; the demo organisation's entire Liabilities
section did.

```
$ curl -s "${H[@]}" http://localhost:8098/api/v1/reports/trial-balance | python3 /tmp/show.py
ReportID=TrialBalance  ReportName=Trial Balance
  Header | Account | Debit | Credit | YTD Debit | YTD Credit
  [Section] Revenue
    Row | Sales (200) | 0.00 | 7108.18 | 0.00 | 10355.68
    SummaryRow | Total Revenue | 0.00 | 7108.18 | 0.00 | 10355.68
  ...
  [Section] Assets
    Row | Business Bank Account (090) | 7108.18 | 609.00 | 10355.68 | 11579.47
    Row | Business Savings Account (091) | 0.00 | 0.00 | 0.00 | 250.00
    Row | Office Equipment (710) | 0.00 | 0.00 | 1000.00 | 0.00
    SummaryRow | Total Assets | 7108.18 | 609.00 | 11355.68 | 11829.47
  SummaryRow | Total | 7717.18 | 7717.18 | 22185.15 | 22185.15
```

Account `840 Historical Adjustment` (a current liability carrying 8,654.01) is
not in the payload at all, and `090 Business Bank Account` reads as two
movements (10,355.68 debit against 11,579.47 credit) rather than as the 7,430.22
the bank statement closes at.

**After.** Every account carrying a balance at the report date is rendered, and
each account is measured the way Xero's Trial Balance measures it: the balance
carried as at the report date for a balance sheet account, the year-to-date
movement for a profit and loss account. The account's own `type` picks the
measure; nothing is keyed off a code, a name or a figure.

```
$ curl -s "${H[@]}" http://localhost:8099/api/v1/reports/trial-balance | python3 /tmp/show.py
ReportID=TrialBalance  ReportName=Trial Balance
  Header | Account | Debit | Credit | YTD Debit | YTD Credit
  [Section] Revenue
    Row | Sales (200) | 0.00 | 7108.18 | 0.00 | 10355.68
    SummaryRow | Total Revenue | 0.00 | 7108.18 | 0.00 | 10355.68
  [Section] Less Operating Expenses
    Row | Advertising (400) | 0.00 | 0.00 | 6203.75 | 0.00
    ...
    SummaryRow | Total Less Operating Expenses | 609.00 | 0.00 | 10829.47 | 0.00
  [Section] Assets
    Row | Business Bank Account (090) | 7108.18 | 609.00 | 7430.22 | 0.00
    Row | Business Savings Account (091) | 0.00 | 0.00 | 0.00 | 250.00
    Row | Office Equipment (710) | 0.00 | 0.00 | 1000.00 | 0.00
    SummaryRow | Total Assets | 7108.18 | 609.00 | 8430.22 | 250.00
  [Section] Liabilities
    Row | Historical Adjustment (840) | 0.00 | 0.00 | 0.00 | 8654.01
    SummaryRow | Total Liabilities | 0.00 | 0.00 | 0.00 | 8654.01
  SummaryRow | Total | 7717.18 | 7717.18 | 19259.69 | 19259.69
```

`090` now reads its closing balance — **7,430.22** — and `840` is present at
**8,654.01**. The period columns are unchanged (7,717.18 each side: the
September movement).

The title block states the rule, so a reader is not left to infer it:

```
 - Trial Balance
 - Demo Company (Global)
 - From 1 September 2026 To 11 September 2026
 - Debit/Credit: movement in the period. YTD Debit/YTD Credit: year-to-date movement for profit and
   loss accounts, balance carried as at the report date for balance sheet accounts.
```

### The Total row is checkable against the ledger

Each column of the Total row is accumulated from the *rendered* cells, so it can
be checked against the rows above it rather than against a query of its own. The
same measure, computed independently in SQL, must reproduce it:

```sql
WITH measure AS (
  SELECT a.code, a.type,
         COALESCE(SUM(CASE WHEN j.journal_date <= DATE '2026-09-11' THEN l.net_amount END),0) AS closing,
         COALESCE(SUM(CASE WHEN j.journal_date BETWEEN DATE '2026-01-01' AND DATE '2026-09-11'
                           THEN l.net_amount END),0) AS ytd
    FROM accounts a
    LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id
    LEFT JOIN gl_journals j ON j.journal_id = l.journal_id AND j.organisation_id = a.organisation_id
   WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
   GROUP BY a.code, a.type
), m AS (
  SELECT CASE WHEN type IN ('REVENUE','SALES','DIRECTCOSTS','EXPENSE','OVERHEADS','DEPRECIATN','WAGESEXPENSE')
              THEN ytd ELSE closing END AS v
    FROM measure
)
SELECT SUM(CASE WHEN v > 0 THEN v ELSE 0 END) AS total_debit,
       SUM(CASE WHEN v < 0 THEN -v ELSE 0 END) AS total_credit
  FROM m;
```

```sh
$ docker exec -i bonefish-postgres-1 psql -U goxero -d goxero < /tmp/tb-measure.sql
 total_debit | total_credit
-------------+--------------
  19259.6900 |   19259.6900
```

19259.69 per side, exactly what the report prints — and the two accounts the
acceptance criteria name, straight from the ledger:

```sh
$ docker exec -i bonefish-postgres-1 psql -U goxero -d goxero -c \
  "SELECT a.code, a.name, a.type, COALESCE(SUM(l.net_amount),0) AS closing_balance
     FROM accounts a
     LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id
     LEFT JOIN gl_journals j ON j.journal_id = l.journal_id AND j.organisation_id = a.organisation_id
    WHERE a.organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code IN ('090','840')
    GROUP BY a.code, a.name, a.type"
 code |         name          |   type   | closing_balance
------+-----------------------+----------+-----------------
 090  | Business Bank Account | BANK     |       7430.2200
 840  | Historical Adjustment | CURRLIAB |      -8654.0100
```

**Why the totals differ from the old ones.** The old report added the debit and
credit *movements* of every account separately, so an account that received and
spent money inflated both sides (090 alone contributed 10,355.68 to debits and
11,579.47 to credits). The closing-balance measure nets an account's history
into a single figure and puts it on the side the balance falls, so the totals
are smaller and, unlike the old pair, meaningful. That is also how Xero's own
Trial Balance reads: its reference output totals 21,272.45 of profit and loss
movement plus 21,323.01 of balance sheet balances, 42,595.46 on each side.

**One property worth stating.** The two YTD sides balance when every posting
falls inside the financial year, or when a prior year's result has been closed
to equity. In this organisation the prior year has not been closed, so the mixed
measure does not balance the two sides against each other — a property of the
measure, not of the report. The report does not paper over it: the Total row is
the sum of what it renders, and what it renders is each account's own balance.
The integration test pins the balanced case with a single-year ledger and pins
the Total-is-the-sum-of-the-rows property in both.

---

## Defect 2 — Cash Summary was the Bank Summary under a different name

> **Correction from the coordinator (msg_35e3aab549d6).** My first repair of
> this defect rendered "cash received and spent by month, per bank account",
> which is not Xero's Cash Summary either — it was the Bank Summary's own
> per-account view with months added. The coordinator supplied the real
> reference (`docs/xero-reference/cash-summary.txt`), and this section documents
> the renderer built to it: one period, grouped Income / Less Expenses / Other
> Cash Movements / Tax Movements, closing on Net Cash Movement and the Cash
> Balance. The month machinery is gone from the repository as well as the
> renderer.

**Before.** `/reports/cash-summary` returned the Bank Summary payload, byte for
byte apart from its `ReportName`. A client asking where the cash *went* was told
where it *ended up*:

```
$ curl -s "${H[@]}" "http://localhost:8098/api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-12-31" | python3 /tmp/show.py
ReportID=CashSummary  ReportName=Cash Summary
  Header | Bank Account | Opening Balance | Cash Received | Cash Spent | Closing Balance
  [Section]
    Row | Business Bank Account (090) | 8654.01 | 10355.68 | 11579.47 | 7430.22
    Row | Business Savings Account (091) | 0.00 | 0.00 | 250.00 | -250.00
  SummaryRow | Total | 8654.01 | 10355.68 | 11829.47 | 7180.22

$ curl -s "${H[@]}" "http://localhost:8098/api/v1/reports/bank-summary?fromDate=2026-01-01&toDate=2026-12-31" | python3 /tmp/show.py
ReportID=BankSummary  ReportName=Bank Summary
  Header | Bank Account | Opening Balance | Cash Received | Cash Spent | Closing Balance
  [Section]
    Row | Business Bank Account (090) | 8654.01 | 10355.68 | 11579.47 | 7430.22
    Row | Business Savings Account (091) | 0.00 | 0.00 | 250.00 | -250.00
  SummaryRow | Total | 8654.01 | 10355.68 | 11829.47 | 7180.22
```

**After.** Cash Summary is its own aggregation and its own layout: the reference's
sections, the reference's grid — the period's own year and then the three
comparative columns — and a Net Cash Movement that is the sum of the rows above
it:

```
$ curl -s "${H[@]}" "http://localhost:8099/api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-12-31" | python3 /tmp/show.py
ReportID=CashSummary  ReportName=Cash Summary
  Header |  | 2026 | Yearly average (YTD) | Variance | Variance for Variance
  [Section] Income
    Row | Sales (200) | 10355.68 |  |  |
    SummaryRow | Total Income | 10355.68 |  |  |
  [Section] Less Expenses
    Row | Advertising (400) | 6203.75 |  |  |
    Row | Cleaning (408) | 1082.50 |  |  |
    Row | Entertainment (420) | 1522.00 |  |  |
    Row | Light, Power, Heating (445) | 1363.92 |  |  |
    Row | Motor Vehicle Expenses (449) | 148.50 |  |  |
    Row | Office Expenses (453) | 88.10 |  |  |
    Row | Printing & Stationery (461) | 49.20 |  |  |
    Row | Repairs and Maintenance (473) | 69.50 |  |  |
    Row | Telephone & Internet (489) | 110.00 |  |  |
    Row | Travel - National (493) | 192.00 |  |  |
    SummaryRow | Total Expenses | 10829.47 |  |  |
  Row | Surplus (Deficit) | -473.79 |  |  |
  [Section] Plus Other Cash Movements
    Row | Office Equipment (710) | -1000.00 |  |  |
    SummaryRow | Total Other Cash Movements | -1000.00 |  |  |
  [Section] Plus Tax Movements
    Row | Tax Collected |  |  |  |
    Row | Tax Paid |  |  |  |
    Row | Net Tax Movements |  |  |  |
  SummaryRow | Net Cash Movement | -1473.79 |  |  |
  [Section] Summary
    Row | Opening Balance | 8654.01 |  |  |
    Row | Plus Net Cash Movement | -1473.79 |  |  |
    SummaryRow | Cash Balance | 7180.22 |  |  |
```

The two sections are the coded lines themselves: over this window the sales-tax
workpaper (`/reports/sales-tax`) reads `Net Sales 10355.68` and
`Net Purchases 10829.47` — the same figures the Cash Summary groups under Income
and Less Expenses, because both reports are aggregating the same
`gl_journal_lines` rows and neither has a figure of its own.

### Net Cash Movement is an identity of the rows, not a second measurement

The renderer sums what it printed — `Total Income` plus the expenses' signed
contribution plus `Total Other Cash Movements`, plus the tax movement where it
can be measured — so the row cannot disagree with the rows above it. That sum is
also the bank accounts' own movement, because every journal that moves cash has
lines summing to zero and the sections partition the non-bank side of exactly
those journals. On the demo organisation, over the window above:

```
$ docker exec -i bonefish-postgres-1 psql -U goxero -d goxero \
    -v org=6823b27b-c48f-4099-bb27-4202a4f496a2 -v from=2026-01-01 -v to=2026-12-31 \
    -v win="2026-01-01..2026-12-31" < /tmp/cash-summary-check.sql
         window         | rendered_rows_sum | bank_movement
------------------------+-------------------+---------------
 2026-01-01..2026-12-31 |        -1473.7900 |    -1473.7900
```

The same query over the whole ledger — where the opening balance is nil —
returns `7180.2200` on both sides, and the report renders exactly that:

```
$ docker exec -i bonefish-postgres-1 psql -U goxero -d goxero \
    -v org=6823b27b-c48f-4099-bb27-4202a4f496a2 -v from=1900-01-01 -v to=2026-12-31 \
    -v win="1900-01-01..2026-12-31 (whole ledger, opening nil)" < /tmp/cash-summary-check.sql
                       window                       | rendered_rows_sum | bank_movement
----------------------------------------------------+-------------------+---------------
 1900-01-01..2026-12-31 (whole ledger, opening nil) |         7180.2200 |     7180.2200

$ curl -s "${H[@]}" "http://localhost:8099/api/v1/reports/cash-summary?fromDate=1900-01-01&toDate=2026-12-31" \
    | python3 /tmp/show.py | tail -6
  SummaryRow | Net Cash Movement | 7180.22 |  |  |
  [Section] Summary
    Row | Opening Balance | 0.00 |  |  |
    Row | Plus Net Cash Movement | 7180.22 |  |  |
    SummaryRow | Cash Balance | 7180.22 |  |  |
```

**On the coordinator's 7,430.22.** That figure is the closing balance of
`090 Business Bank Account` alone; the organisation has a second active bank
account, `091 Business Savings Account`, which closes at `-250.00` (a bank
transaction on 091 coded to `400 Advertising`, not a transfer), so the two
together close at `7180.22` and the report says `7180.22`. The report takes its
bank accounts from the ledger (`accounts.type = 'BANK'`), so it cannot be tuned
to one account without hardcoding. Over a window whose opening balance is nil the
report states a Net Cash Movement of `7180.22` — every posted bank line in the
books. If the intent is that the demo data should read 7,430.22 in this report,
that is a question about `091`'s posting, not about the renderer.

### What the schema cannot fill stays visible

`Plus Tax Movements` cannot be measured here: a line's tax amount is recorded
*beside* its net amount (`gl_journal_lines.tax_amount`) rather than posted to a
tax account, so no account's movement *is* the tax. The section keeps its
heading and its three lines with the amount cells empty — no `0.00`, which would
read as a measured zero — and the titles carry the reason. The three comparative
columns are empty for the same reason: a single-period report has nothing to
compare against and the organisation stores no budget to average.

```
 - Not represented: the Yearly average (YTD), Variance and Variance for Variance columns (this report
   is rendered for a single period and the organisation stores no budget to average or to compare
   against); Tax Movements (a line's tax amount is recorded beside its net amount rather than posted
   to a tax account, so no account's movement is the tax and this report will not assume which account
   holds it).
```

The window itself now defaults to the financial year, which is what makes the
period column — headed by the period's own year — honest, and what Xero's own
"Cash Summary … For the year ended" title means. An explicit `fromDate`/`toDate`
still wins, which is how every payload above was captured.

---

## Defect 3 — the two "by contact" endpoints were the summary report

**Before.** `/reports/aged-receivables-by-contact` and
`/reports/aged-payables-by-contact` were registered against the summary handlers,
so they answered with the summary report — including its identity. A client
could not tell the two apart even by looking at `ReportID`:

```
$ curl -s "${H[@]}" http://localhost:8098/api/v1/reports/aged-receivables-by-contact \
  | python3 -c "import json,sys; r=json.load(sys.stdin)['Reports'][0]; print(r['ReportID'],'|',r['ReportName'])"
AgedReceivables | Aged Receivables
```

**After.** They are their own report: one section per contact, holding that
contact's invoices, with the contact's own subtotal and a grand total, under a
report the route names.

```
$ curl -s "${H[@]}" http://localhost:8099/api/v1/reports/aged-receivables-by-contact \
  | python3 -c "import json,sys; r=json.load(sys.stdin)['Reports'][0]; print(r['ReportID'],'|',r['ReportName'])"
AgedReceivablesByContact | Aged Receivables by Contact

$ curl -s "${H[@]}" http://localhost:8099/api/v1/reports/aged-receivables-by-contact | python3 /tmp/show.py
ReportID=AgedReceivablesByContact  ReportName=Aged Receivables by Contact
  Header | Contact | Current | 1-30 | 31-60 | 61-90 | > 90 | Total
  SummaryRow | Total | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 | 0.00
```

The demo organisation has no invoices, so there is nothing to drill into here —
the shape above is the empty case. The populated shape (a section per contact,
one row per invoice, `Total <contact>`, and a grand total that equals the
summary report's) is asserted in
`internal/handlers/reports_backend_fixes_integration_test.go`, which seeds two
receivables for one customer and one payable for a supplier and checks the
drill-down against the summary report rather than against a literal.

---

## Defect 4 — BAS and Sales Tax were a header with no rows and no total

**Before.** `/reports/bas` and `/reports/sales-tax` returned the same payload —
a header row, no rate, no amount, no total — and both called themselves
`Sales Tax Report` while the route that returned them was named BAS:

```
$ curl -s "${H[@]}" http://localhost:8098/api/v1/reports/sales-tax | python3 /tmp/show.py
ReportID=BASReport  ReportName=Sales Tax Report
  Header | Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax

$ curl -s "${H[@]}" http://localhost:8098/api/v1/reports/bas | python3 /tmp/show.py
ReportID=BASReport  ReportName=Sales Tax Report
  Header | Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax
```

**After.** One row per tax rate the organisation's *own coded journals* carry —
the row set comes from `gl_journal_lines.tax_type`, joined to the organisation's
`tax_rates` only for the name, so no list of rates is assumed or hardcoded — plus
a Total row that adds up to the rows above it. The two routes keep their own
identities:

```
$ curl -s "${H[@]}" http://localhost:8099/api/v1/reports/sales-tax | python3 /tmp/show.py
ReportID=SalesTaxReport  ReportName=Sales Tax Report
  Header | Tax Rate | Net Sales | Net Purchases | Tax Collected | Tax Paid | Net Tax
  [Section]
    Row | Tax on Purchases (8.25%) | 0.00 | 609.00 |  |  | 
    Row | Tax on Consulting (8.25%) | 7003.78 | 0.00 |  |  | 
    Row | Tax on Goods (8.75%) | 104.40 | 0.00 |  |  | 
    SummaryRow | Total | 7108.18 | 609.00 |  |  | 
  Row | Tax Collected, Tax Paid and Net Tax are not represented: no posted journal line carries a
        tax amount, so there is nothing to report rather than a zero to print.

$ curl -s "${H[@]}" http://localhost:8099/api/v1/reports/bas | python3 -c \
  "import json,sys; r=json.load(sys.stdin)['Reports'][0]; print(r['ReportID'],'|',r['ReportName'])"
BASReport | BAS / Sales Tax Report
```

The rate names are this organisation's own (read from `tax_rates`), the net
sales and purchases are the coded lines under each rate, and the Total row is
the sum of the rows — 7,108.18 of sales and 609.00 of purchases, the same
figures the Trial Balance and Cash Summary show for the same period.

**The empty tax columns are deliberate.** Every line in this database carries
`tax_amount = 0`, so the columns are left empty and the report says why, in the
body: a 0.00 would read as a measured zero, and the ledger does not carry one.
The rule is per column and per rate: a tax column is filled only where *every*
line on that side of the rate records a tax amount, and it is totalled only when
every rate supports it — a total over the subset that happened to be recorded
would understate the return. The integration test seeds a rate with both a sale
and a purchase carrying tax amounts and checks the columns and the total against
the rows; it also checks that a window with no taxed lines reports no rate, no
invented zero and an explanatory note.

---

---

## Coordinator follow-ups

Two follow-up questions arrived while the four defects above were being fixed.
Both are answered here because both touch reports this task owns.

### `/reports/invoice-summary` was advertised as a report it does not serve

`ReportHandler.ReportsList` listed `{"ReportID":"InvoiceSummary", …,
"Path":"/reports/invoice-summary"}`, but the route is served by
`invoiceHandler.Summary`, which answers with the invoices screens' own KPI object
(`{totalInvoices, draft, authorised, paid, overdue, totalDue, totalPaid}`) and no
Reports envelope. The index promised a report shape the route does not serve.

**Decision: the bare object is the contract, so the entry goes.** That object is
what `web/src/lib/api.ts` declares for `invoiceApi.summary()` and what the
invoices and sales screens parse; wrapping it in a Reports envelope would break
those two pages, and the KPI totals are not a report in Xero's sense in any case
(no rows, no period). So `InvoiceSummary` was removed from `ReportsList` and the
route is unchanged:

```sh
$ curl -s "${H[@]}" "http://localhost:8099/api/v1/reports" | python3 -c 'import json,sys; print([r["ReportID"] for r in json.load(sys.stdin)["Reports"]])'
['TrialBalance', 'ProfitAndLoss', 'BalanceSheet', 'CashSummary', 'BankSummary', 'AgedReceivables', 'AgedPayables', 'ExecutiveSummary', 'BudgetSummary', 'BASReport', 'SalesTaxReport', 'JournalReport', 'AccountTransactions', 'GeneralLedgerDetail', 'AgedReceivablesByContact', 'AgedPayablesByContact', 'Form1120']
```

**The index and the routes can no longer disagree.** `TestHTTP_Reports_
EveryEndpointAnswersInTheXeroEnvelope` no longer hand-lists paths: it enumerates
every GET route under `/api/v1/reports/` from the Fiber app itself
(`registeredReportRoutes`, 20 routes today) and checks the index against that set
in both directions.

* Every path the index advertises must be a registered route, and must serve the
  `ReportID` its entry names.
* Every registered route must be classified, or the test fails and says so:
  advertised in the index, an entry in `aliasRoutes` (a second path to a report
  the index already advertises — `/reports/profit-loss` and
  `/reports/general-ledger` — which must serve the same `ReportID` as its
  canonical path), or an entry in `nonReportRoutes` with the reason it is not a
  report (`invoice-summary`), whose documented shape in `nonReportShape` the test
  also pins so the exemption cannot rot.

Both directions were checked by breaking them on purpose:

```
$ # an index entry with no route behind it
Messages: the index advertises /api/v1/reports/ghost but no route serves it
--- FAIL: TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope (0.15s)

$ # a route nobody classified
Messages: /api/v1/reports/zz-ghost is registered under /reports/ but classified nowhere: add it to the index, to aliasRoutes, or to nonReportRoutes with a reason
--- FAIL: TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope (0.21s)
```

### The aged reports: the labels, the percentage row, the payables sections

**Finding first: goXero's ageing *partition* is not Xero's, so matching Xero's
header text on its own would mislabel amounts.** The buckets in
`internal/repository/report.go` (`Aged`) are `Current` (not yet due) / `1-30` /
`31-60` / `61-90` / `> 90` days past due, headed `Current | 1-30 | 31-60 | 61-90 |
> 90` (`report_render.go`, the summary and the by-contact variant). Xero's
columns are `< 1 Month | 1 Month | 2 Months | 3 Months | Older`.

The two are not the same partition, and the reference reports' own column totals
prove it. Running both partitions over the reference dataset (the rows
`migrations/data/xero/{invoices,bills}.csv` carry, the same rows the importer
writes) as at 31 December 2026:

```sh
$ python3 - <<'PY'
import csv, datetime, collections
ASAT = datetime.date(2026, 12, 31)          # the reference reports' as-at date

def outstanding(path):
    for r in csv.DictReader(open(path)):
        if r['status'] == 'AUTHORISED' and float(r['amount_due']) > 0:
            due = datetime.date.fromisoformat(r['due_date'] or r['date'])
            yield r['contact'], due, float(r['amount_due'])

def goXero(d):   # internal/repository/report.go: Current / 1-30 / 31-60 / 61-90 / > 90
    d = (ASAT - d).days
    return 'Current' if d <= 0 else '1-30' if d <= 30 else '31-60' if d <= 60 else '61-90' if d <= 90 else '> 90'

def xero(d):     # < 1 Month / 1 Month / 2 Months / 3 Months / Older
    d = (ASAT - d).days
    return '< 1 Month' if d <= 30 else '1 Month' if d <= 60 else '2 Months' if d <= 90 else '3 Months' if d <= 120 else 'Older'

COLS = ['< 1 Month', '1 Month', '2 Months', '3 Months', 'Older']
for label, path in [('Aged Receivables', 'migrations/data/xero/invoices.csv'),
                    ('Aged Payables',    'migrations/data/xero/bills.csv')]:
    rows = list(outstanding(path))
    x = collections.Counter(); g = collections.Counter()
    for _, due, amt in rows:
        x[xero(due)] += amt; g[goXero(due)] += amt
    total = sum(amt for _, _, amt in rows)
    print(f'{label} (as at {ASAT}, status AUTHORISED, amount_due > 0; {len(rows)} rows)')
    print('  Xero   ' + '  '.join(f'{c} {x[c]:8.2f}' for c in COLS) + f'  | Total {total:8.2f}')
    print('  goXero ' + '  '.join(f'{c} {g[c]:8.2f}' for c in ['Current','1-30','31-60','61-90','> 90']) + f'  | Total {total:8.2f}')
    print()
PY
```

```sh
Aged Receivables (as at 2026-12-31, status AUTHORISED, amount_due > 0; 9 rows)
  Xero   < 1 Month     0.00  1 Month     0.00  2 Months     0.00  3 Months  8435.68  Older   758.83  | Total  9194.51
  goXero Current     0.00  1-30     0.00  31-60     0.00  61-90     0.00  > 90  9194.51  | Total  9194.51

Aged Payables (as at 2026-12-31, status AUTHORISED, amount_due > 0; 12 rows)
  Xero   < 1 Month     0.00  1 Month     0.00  2 Months  2132.51  3 Months  4031.15  Older  2223.10  | Total  8386.76
  goXero Current     0.00  1-30     0.00  31-60     0.00  61-90  2132.51  > 90  6254.25  | Total  8386.76
```

The `Xero` line is the reference reports' published column totals
(`docs/xero-reference/aged-receivables-summary.txt`, `aged-payables-summary.txt`);
the partition that produces them is `≤ 30 / 31-60 / 61-90 / 91-120 / > 120` days
past due — `< 1 Month / 1 Month / 2 Months / 3 Months / Older` on a days-past-due
reading, which a whole-months-overdue reading also fits, since the rows here are
dated 81 to 164 days back and the two readings only diverge at the boundary. It reproduces every published column total to the cent —
AR `3 Months` 8,435.68 and `Older` 758.83, AP `2 Months` 2,132.51, `3 Months`
4,031.15 and `Older` 2,223.10 — and the per-contact rows too (City Limousines'
488.20 in `Older` is its three July/August invoices 250.00 + 216.50 + 21.70, its
703.63 in `3 Months` the 20 September one). goXero's partition puts the whole
receivables balance in one column where Xero splits it, and puts 6,254.25 under
`> 90` where Xero prints 4,031.15 under `3 Months` and 2,223.10 under `Older`.

So the two disagreements are structural, not verbal: Xero has no separate
not-yet-due column (it folds it into `< 1 Month`) and it splits goXero's `> 90`
at 120 days. Relabelling goXero's five columns with Xero's five names would
publish the wrong figure under the right name — worse for a client than today's
honest mismatch.

**In scope / out of scope, per question:**

1. **Column labels — out of scope as posed; in scope only together with the
   partition.** The premise that the partitions match is false (above), and the
   coordinator's instruction was not to change the bucketing arithmetic, so
   neither changed. Nothing was relabelled: with the current buckets there is no
   column whose contents survive a rename. (One thing the reference dataset
   cannot settle: no unpaid invoice in it is *not yet due* at the as-at date, so
   whether `< 1 Month` really swallows `Current` is inferred from Xero's month
   naming, not measured.)
2. **The `Percentage of total` row — same condition.** The row itself is
   straightforward (each column ÷ the Total column, `-` where the bucket is nil,
   `100.00%` in Total, computed from the same rows the table prints), but under
   the current partition it would print `100.00%` in the `> 90` column where Xero
   prints 91.75% / 8.25% across `3 Months` / `Older`: a percentage that
   contradicts the reference. It belongs in the same change as the partition.
3. **The payables sections — representable from the schema, not implemented
   here.** Xero's `Total 8,502.71` is `Total Aged Payables 8,386.76` + `Total
   Expense Claims 115.95`, and the reference dataset carries exactly that claim
   (`migrations/data/xero/expense-claims.csv`: 115.95, unpaid, 10 September 2026,
   office expenses) — the same 115.95 Xero ages into `3 Months`. Xero files that
   claim under a person's name while the harvested row says `Xero Demo`; the
   amount and the ageing bucket are what the section would need. The schema
   stores what the section needs: `expense_claims` has `amount_due`,
   `payment_due_date`, `reporting_date`, `status` and `user_id`, and `users`
   holds the claimant's name that Xero prints. So the gap is not the schema, it
   is that `Aged` reads `invoices` only. Implementing it would add one query
   (submitted/authorised claims with `amount_due > 0`, aged from
   `COALESCE(payment_due_date, reporting_date)` by claimant) and would restructure
   the payables renderer into the three Xero blocks — a change to the report's
   totals and shape, and one that should use the corrected buckets above, so it
   was left for the coordinator to call rather than done unasked. It also could
   not be verified against live data today: the dev database currently holds no
   invoices and no expense claims at all (`select count(*) from invoices` → 0,
   `from expense_claims` → 0), so the reference dataset is the only thing to
   check against.

## Tests

Four regression tests, one per defect,
`internal/handlers/reports_backend_fixes_integration_test.go`:

```sh
$ go test ./internal/handlers/ -run 'TestHTTP_Reports_(TrialBalanceRendersEvery|CashSummaryIsItsOwn|AgedByContactIsThe|SalesTaxRowsFollow)' -v -count=1
=== RUN   TestHTTP_Reports_TrialBalanceRendersEveryAccountWithABalance
--- PASS: TestHTTP_Reports_TrialBalanceRendersEveryAccountWithABalance (0.13s)
=== RUN   TestHTTP_Reports_CashSummaryIsItsOwnReport
--- PASS: TestHTTP_Reports_CashSummaryIsItsOwnReport (0.12s)
=== RUN   TestHTTP_Reports_AgedByContactIsTheContactDrillDown
--- PASS: TestHTTP_Reports_AgedByContactIsTheContactDrillDown (0.09s)
=== RUN   TestHTTP_Reports_SalesTaxRowsFollowTheLedger
--- PASS: TestHTTP_Reports_SalesTaxRowsFollowTheLedger (0.09s)
PASS
ok  	github.com/shurco/goxero/internal/handlers	0.822s
```

They check against the ledger rather than against literals where they can: the
Trial Balance test reads every account with a non-zero closing balance out of
the database and requires a row for each, and reads the closing balance back for
the accounts it measures; the Cash Summary test reads the bank accounts out of
the chart of accounts and checks the report's Net Cash Movement against their
movement in the ledger, against the Bank Summary's own closing-less-opening for
the same window, and against the rows printed above it; the Sales Tax test reads
the rate names out of `tax_rates` and sums the rendered rows to check the total.

The pre-existing `internal/handlers/reports_parity_integration_test.go` was
updated for the organisation's current chart of accounts (migration 00023
replaced the demo chart) and now clears the seeded ledger before it posts its
own, so its figures are the fixture's alone. Its envelope test no longer
hand-lists report paths: it enumerates the 20 registered routes under
`/api/v1/reports/` from the router and cross-checks the `/reports` index against
them in both directions, so an advertised path without a route and a route
nobody classified both fail the build (see the follow-ups above).

## Build and test

```sh
$ go build ./...
$ echo $?
0

$ go test ./internal/... -count=1
ok  	github.com/shurco/goxero/internal/bankcoding	0.313s
ok  	github.com/shurco/goxero/internal/bankfeed	0.626s
ok  	github.com/shurco/goxero/internal/bankrules	1.527s
ok  	github.com/shurco/goxero/internal/bankstatement	0.903s
ok  	github.com/shurco/goxero/internal/config	1.269s
ok  	github.com/shurco/goxero/internal/database	2.031s
ok  	github.com/shurco/goxero/internal/handlers	8.710s
ok  	github.com/shurco/goxero/internal/logger	2.514s
ok  	github.com/shurco/goxero/internal/middleware	3.106s
ok  	github.com/shurco/goxero/internal/models	3.067s
ok  	github.com/shurco/goxero/internal/repository	4.162s
?   	github.com/shurco/goxero/internal/router	[no test files]
?   	github.com/shurco/goxero/internal/testutil	[no test files]
```

The whole suite is green, `internal/handlers` included: the Form 1120 failures
that were open when this task started (reported to the coordinator rather than
absorbed here, since that workpaper is not this task's) pass now that the task
that owns them has landed.

## Files changed

| file | change |
|---|---|
| `internal/repository/report.go` | `TrialBalanceRow.ClosingBalance`; income/cost class helpers; `SalesTaxByRate` rewritten as one query per tax rate over the coded lines, with per-column measurement; `AgedDetailRow` + `AgedByContact`; `CashSummaryReport` + `CashSummary` (the month-by-account query and its row type removed in the rework) |
| `internal/handlers/report_render.go` | `trialBalanceMeasure` and the rewritten Trial Balance renderer (every account with a balance, a section per unmatched type, totals summed from the rendered rows); `renderCashSummary` and its `cashSummarySection`/`cashSummaryAmountRow`/`cashSummaryCaveat` helpers; `renderSalesTax`; `renderAgedByContact` |
| `internal/handlers/report.go` | Cash Summary wired to its own aggregation, with the window defaulting to the financial year; BAS and Sales Tax split into two endpoints with their own identities; `AgedReceivablesByContact` / `AgedPayablesByContact`; the catalogue now lists only paths that answer with a Reports envelope (`InvoiceSummary` removed, its route unchanged) |
| `internal/router/router.go` | `/reports/sales-tax` and the two by-contact routes registered against their own handlers |
| `internal/handlers/reports_backend_fixes_integration_test.go` | new — one regression test per defect |
| `internal/handlers/reports_parity_integration_test.go` | updated to the current chart of accounts and to a self-contained ledger; the envelope test rewritten to enumerate the routes from the router and cross-check the `/reports` index in both directions |
| `docs/reports-backend-fixes.md` | this document |

No migration, no `web/**` file and no `docs/xero-reference/**` file was touched.
