# Reports vs Xero — coordinator's summary

Written by the coordinator of the orchestration runs `run_61947862f7c5` and
`run_a891fdc5abd5`, from its own measurements against the running application, the shared
dev database and the frozen capture under `docs/xero-reference/`. It is a summary of what was asked, what was
delivered, what was verified independently, and what is still open. The independent
verifier's own report is `docs/reports-verification.md`.

## 1. What was asked

Check that every report under `/app/reports` works, find how each is built and which data
it reads, compare them against the Xero organisation `!!6Sp3` (`reporting.xero.com`), fix
the ones that are broken, use no hardcoded data, and — as the reference dataset — migrate
Xero's accounts and bank transactions (and, by the follow-up decision, everything needed
to reproduce Xero's whole Trial Balance) into this application.

Agreed scope: working reports plus honestly-marked stubs, not all ~80 catalogue reports;
import into the existing demo organisation; extend the capture to the full Trial Balance.

## 2. The imported dataset

| Migration | What it puts in demo organisation `6823b27b-…` |
| --- | --- |
| `00023_xero_reference_chart_of_accounts.sql` | Chart of accounts, tax rates, contacts, bank accounts, statement lines, coded bank transactions, and one derived opening-balance journal |
| `00024_xero_reference_source_documents.sql` | All 82 coded bank transactions (superseding 00023's 48), the real opening balance, 78 invoices/bills with 87 line items, 5 credit notes, 44 payments, 3 expense claims with their claimants, and the document/payment journals that post them |

Source values are captured under `migrations/data/xero/*.csv`; the capture, its
conventions and its gaps are described in `docs/xero-reference/README.md`, and the
account-by-account reproduction in `docs/xero-reference/reconciliation.md`.

**00023 is destructive by design, for the demo organisation only.** Its Up deletes that
organisation's bank statement lines, bank transactions, bank rules, GL journals, chart of
accounts, tax rates and contacts before re-creating them from the capture, because the
seeded US chart gives different meanings to the same account codes. Its Down restores the
previous rows with their original ids. It must not be run against an organisation whose
data you want to keep.

## 3. Where the reports stand

The index (`GET /api/v1/reports`) advertises 17 reports; all 17 answer 200 and return the
Xero envelope under the ReportID they advertise. The two by-contact endpoints
(`aged-receivables-by-contact`, `aged-payables-by-contact`) were served but had no web
page when the verification began; the pages were added while it ran, and every
`/app/reports` route now renders (see section 8).

Every page under http://localhost:5173/app/reports renders real API data with no error
banner. The four defects found in the initial audit (Trial Balance dropping accounts, the
Cash Summary not being its own statement, the by-contact endpoints and the BAS/Sales Tax
rows not producing the reports they name, and the aged reports not ageing on Xero's
partition) were fixed and are written up in `docs/reports-backend-fixes.md` and
`docs/xero-aged-parity.md`.

Two rows of the web catalogue were corrected by the coordinator in
`web/src/lib/reports-catalog.ts`: **Account Summary** is now marked unavailable rather
than pointed at Bank Summary, which is a different report, and **Budget Summary**'s
description no longer promises budgets that no table can hold.

Re-swept in a browser at ~21:55 local: all 21 `/app/reports` routes (the 17 the index
advertises plus `bas`, the two by-contact pages and `uncoded-statement-lines`) render with
no error element and no "Page not found" text, and each shows figures — `budget-summary`
shows none, which is its honest stub. `/app/reports/bas` answers 308 to
`/app/reports/sales-tax`, as designed.

## 4. The dataset check, account by account

Xero's Trial Balance (`docs/xero-reference/trial-balance.txt`, 25 accounts) against the
demo organisation's ledger, as at 2026-12-31. Two measurements are recorded, because another
session keeps writing to the same organisation; the second is the current one.

**At 2026-09-11 ~16:20 local** (the state sections 3 and 5 describe):

* All 25 accounts are present and carry Xero's figure, with **one** exception: `400
  Advertising` reads 9,907.05 against Xero's 9,657.05, a difference of exactly **250.00**.
* That 250.00 is **not** imported data. Another session in this worktree coded a 250.00
  bank transaction on account `091 Business Savings Account` against `400 Advertising`
  while this work was in progress; Xero's reference has no `091` line at all, and `091`
  appears in our Balance Sheet at −250.00 for the same reason. Netting it out, the
  organisation's own figures agree with the reference.
* Everything downstream of it differs by the same 250.00 and nothing else: Profit and
  Loss Net Profit 8,016.73 against Xero's 8,266.73, Total Assets 21,073.01 against
  21,323.01, Net Assets and Equity 8,016.73 against 8,266.73, Cash Spent 19,743.23
  against 19,493.23.
* Balance-sheet liabilities agree exactly: Accounts Payable 8,386.76, Unpaid Expense
  Claims 115.95, Sales Tax 422.59, Historical Adjustment 4,130.98, total 13,056.28.
* Aged Receivables reproduces `aged-receivables-summary.txt` to the cent (total 9,194.51;
  8,435.68 in `3 Months`, 758.83 in `Older`), Aged Payables reproduces
  `aged-payables-summary.txt` including its Aged Payables / Expense Claims / Total blocks
  and the 8,502.71 overall total.
* Bank Summary reproduces Xero for account 090 exactly: received 26,923.45, spent
  19,493.23, closing 7,430.22.

**Re-measured at 2026-09-11 ~21:45 local**, after two further writes by the same session —
journal **52** (created 16:37 local, a 250.00 spend from `091` coded to `400 Advertising`,
"Office Chair") and journals **1612–1615** (created 19:30 local, four bank transactions
against `090`). Twenty of the 25 capture accounts still carry Xero's figure exactly. The
five that do not, and the extra account:

| account | Xero | goXero | Δ | cause |
| --- | --- | --- | --- | --- |
| 090 Business Bank Account | 7,430.22 Dr | 7,375.72 Dr | −54.50 | journals 1612–1615: their `090` lines are −12.00, −15.00, −15.50, −12.00 |
| 200 Sales | 29,539.18 Cr | 29,515.18 Cr | −24.00 | journals 1612 and 1615, 12.00 each, debited to `200` ("Central City Parking") |
| 400 Advertising | 9,657.05 Dr | 9,907.05 Dr | +250.00 | journal 52 ("Office Chair") |
| 404 Bank Fees | 30.00 Dr | 45.00 Dr | +15.00 | journal 1613 ("Ridgeway Bank") |
| 453 Office Expenses | 862.48 Dr | 877.98 Dr | +15.50 | journal 1614 ("7-Eleven") |
| 091 Business Savings Account | not in Xero's chart | 250.00 Cr | — | journal 52's counterpart |

Its Total row is **42,821.46 / 42,821.46**, which is Xero's 42,595.46 + 250.00 + 15.00 +
15.50 − 54.50 — every one of those four numbers the other session's, and the five differ-
ences above are the whole of it. The five reports named in the third bullet were not
re-measured after 19:30; they move with the same rows and nothing else.
Bank Summary at the same re-measurement prints account 090 as received 26,923.45 / spent
**19,547.73** / closing **7,375.72** — spent and closing are the same 54.50 the table above
accounts for, against Xero's 19,493.23 and 7,430.22 — and a second row for `091` closing
−250.00 from journal 52, so its own total closing is 7,125.72. Aged Receivables still
reproduces its capture to the cent (total 9,194.51; 8,435.68 in `3 Months`, 758.83 in
`Older`, the same partition and the same percentages), and Aged Payables still reproduces
its own (Total Aged Payables 8,386.76, Expense Claims 115.95, Total 8,502.71). Profit and
Loss and the Balance Sheet were not re-measured after 19:30; they move with the same rows
and nothing else, by the same 250.00 and 54.50.

### The ledger's raw total, and why it is not the Trial Balance total

Xero's General Ledger grand total is 111,263.08 (`general-ledger-detail.txt`,
`reconciliation.md` line 147) while its Trial Balance total is 42,595.46; the two measure
different things. The imported ledger's own raw total was **108,142.54** at the 16:20
measurement, so the gap to Xero's 111,263.08 was **3,120.54**, and the coordinator accounted
for all of it:

| Item | Debit | Credit | Where it lives |
| --- | --- | --- | --- |
| Receivable credit-note allocations (Boom FM 541.25, DIISR 21.70, Hamilton Smith 541.25) | 1,104.20 | 1,104.20 | Not posted as a Dr/Cr pair on 610 |
| Payable credit-note allocations (PC Complete 270.63, Swanston Security 25.44) | 296.07 | 296.07 | Not posted as a Dr/Cr pair on 800 |
| Inventory opening balance (Xero journal 651, 630 against 630) | 320.00 | 320.00 | Not posted |
| Tracking Transfers (877) | 1,400.27 | 1,400.27 | No source document captured |
| **Total** | **3,120.54** | **3,120.54** | |

Every item is **net zero**: nothing here can move any account's closing balance, and none
of it appears in Xero's Trial Balance either. Per account, the ledger's debit and credit
totals match `general-ledger-detail.txt` exactly for every account except the four groups
above and the other session's rows.

Re-measured at ~21:45 the raw total is **108,447.04 / 108,447.04** over **439** lines, so the
gap to Xero's 111,263.08 is **2,816.04** = 3,120.54 − 250.00 (journal 52) − 54.50 (journals
1612–1615). Nothing about the four net-zero groups changed.

## 5. Differences that are real, and are stated rather than hidden

1. **The Trial Balance used to print a different measure from Xero's — fixed.** It summed
   gross debits and gross credits into two independent columns, so its Total row read
   108,392.54 / 108,392.54 where Xero prints 42,595.46, and an account that moved both ways
   read as the *sum* of its movement (Sales read 31,579.08 where Xero prints 29,539.18).
   A balanced ledger gives equal totals either way, which is exactly what hid it. It now
   sums each window signed — one net figure per account, split into the Debit/Credit pair on
   the side it falls — and its Total row read **42,845.46 / 42,845.46** at the 16:20
   measurement: Xero's 42,595.46 plus the 250.00 contamination. At the 21:45 re-measurement
   it reads **42,821.46 / 42,821.46**, Xero's 42,595.46 plus that 250.00 and the other
   session's four 19:30 bank journals, net +226.00. 20 of the 25 capture accounts match Xero
   to the cent; the five that do not are, account for account and journal for journal, the
   other session's rows, and `091` is an account Xero's chart does not have. Section 4 has
   the table. The report's own title note now states the basis. See section 8.
2. **The Cash Summary measured a different thing from Xero's — fixed.** Its bottom line was
   right (Cash Balance 7,180.22 = 7,430.22 − 250.00) but it sectioned each journal by its
   counterpart account's own class, so its Income block was empty, the invoice and payment flows
   landed under Other Cash Movements instead of Income and Less Expenses, and its three Plus Tax
   Movements rows printed nothing. It now looks through the document control accounts to the
   documents the cash settled — pro-rated by the share of each document the cash explains — and
   measures the tax each attributed line records beside its net, leaving out the second posting on
   `820`. Live, `?fromDate=2026-01-01&toDate=2026-12-31`:

   ```
   Income · Sales (200)                        21,030.73      Xero 21,054.73   −24.00 (contamination)
   Less Expenses · Total Expenses              15,678.25      Xero 11,266.77   +4,411.48 (840 + contamination)
   Surplus (Deficit)                            5,352.48      Xero  9,787.96   −4,435.48
   Plus Other Cash Movements · 710 + 720       −2,728.29      Xero (2,728.29)  exact
   Plus Other Cash Movements · 840              4,130.98      Xero prints it in Less Expenses
   Total Other Cash Movements                   1,402.69      Xero (2,728.29)  +4,130.98
   Plus Tax Movements · Tax Collected           1,844.56      Xero  1,844.56   exact
   Plus Tax Movements · Tax Paid               −1,474.01      Xero (1,474.01)  exact
   Plus Tax Movements · Net Tax Movements         370.55      Xero    370.55   exact
   Net Cash Movement                            7,125.72      Xero  7,430.22   −304.50 (contamination)
   ```

   The two differences that are not contamination are both stated: `840 Historical Adjustment`
   prints in Plus Other Cash Movements because that is what its own account class is — Xero's
   Balance Sheet files it under Current Liabilities (`balance-sheet.txt` line 25) while its Cash
   Summary prints it inside Less Expenses (`cash-summary.txt` line 20) — and the report says so in
   its own fourth ReportTitles line, naming both citations. Both placements sit below the Surplus
   row, so the movement and the balance are 7,430.22 either way. `Fixed Assets (2,728.29)` is
   printed as the two accounts it is made of, `710` 923.79 + `720` 1,804.50, which sum to Xero's
   figure exactly. Every remaining difference is the shared database's other session, whose five
   journals are −304.50 to the cent (section 4). `docs/cash-summary-xero-rule.md` derives the rule;
   `docs/cash-summary-impl.md` records the implementation, the migration (00026) that repaired the
   payment-to-document links the classification reads, and the line-by-line reconciliation of all
   28 captured lines.
3. **The Sales Tax Report printed no tax figure at all — fixed, and now matches Xero.**
   It printed Net Sales 29,539.18 — Xero's Sales figure exactly — and Net Purchases 21,522.45,
   with Tax Collected, Tax Paid and Net Tax empty. Two causes, both repaired. The import
   (migration 00024) carried `tax_type` onto the income and expense lines but left `tax_amount`
   at 0 on them, so only 33 of the 122 tax-typed lines had one; `invoice_line_items` did hold
   tax per line and account 820 held Xero's 422.59, so the material was there. And the report
   itself had three defects that would have kept its Total row empty even with perfect data: a
   guard no multi-rate organisation could satisfy, a "measured" test that could not measure a
   0% rate, and a purchase class list that dropped the input tax on capital accounts.
   `docs/sales-tax-tax-amount.md` measures all four; migration 00025 backfills the 83 document
   lines and `docs/sales-tax-impl.md` records the report fixes. The report now prints, for
   `?fromDate=2026-01-01&toDate=2026-12-31`:
   **Tax Collected 2,437.80 / Tax Paid 2,015.21 / Net Tax 422.59** — Xero's own figures, from
   `docs/xero-reference/general-ledger-detail.txt` lines 387 and 499, and 422.59 is account
   820's own balance for the period. One further decision was amended after the worker stopped:
   a rate whose line records no tax used to lose its whole column, which hid 1,971.46 of
   recorded tax behind one other-session line; the report now prints the ledger's figure and
   states the ledger's gap in its own body (`Total (1 tax-typed line records no tax)`). The
   amendment, with its arithmetic, is at the head of `docs/sales-tax-impl.md`. The **897.35** an
   earlier note carried against the ledger's 422.59 is not a figure in any data this
   organisation has — it appears in no captured file, no CSV and no query result — and is
   withdrawn; see section 8.
4. **Honest stubs, not defects.** Budget Summary renders Xero's budget layout, prints no
   Total row over zero accounts, and states in its own ReportTitles that no budget is
   stored because the schema has no budget table. Reports the API does not serve are
   marked unavailable in the catalogue rather than linked to something else.

## 6. Open items

* **The Sales Tax Report: closed.** Import backfill (migration 00025, applied) plus the report's
  guard, "measured" test and class split, plus the recorded-tax amendment. Verified live against
  Xero's capture: 2,437.80 / 2,015.21 / 422.59, with 422.59 = account 820. See
  `docs/sales-tax-tax-amount.md`, `docs/sales-tax-impl.md` (and its amendment) and section 5.
* **The Trial Balance's column model: closed.** Section 8.
* **The Cash Summary's sectioning: closed** (2026-09-11 ~22:20 local). The rule is derived in
  `docs/cash-summary-xero-rule.md`, the implementation is recorded in
  `docs/cash-summary-impl.md`, and the acceptance figures hold: Tax Collected 1,844.56, Tax Paid
  (1,474.01) and Net Tax Movements 370.55 are the capture's own to the cent, `Fixed Assets`
  (2,728.29) is exact as `710` + `720`, and `Net Cash Movement` is Xero's 7,430.22 less the other
  session's 304.50. Two deviations are stated rather than hidden: `840`'s placement (below the
  Surplus row, so it cannot move the balance) and the shared database's five journals. Section 5
  item 2 has the row-by-row comparison.
* **The Cash Summary's control accounts were named by code in its SQL: closed** by the same
  change that closed the item above, one step further on. The classification looked through
  `'610'`, `'800'`, `'801'` and `'820'` written into the query. Those four now come from
  `ReportRepository.cashControls`, which reads the organisation's own `system_account` tags for
  all four (the same tags postings resolve through, `systemAccountID` in
  `internal/repository/gl.go`). As first written it kept Xero's code as a fallback where a chart
  declared nothing; that fallback is **gone** — a role nobody holds resolves to `""`, which no
  account code equals, so an undeclared role leaves that cash unclassified rather than attributed
  to whichever account held the code in this chart. This organisation's chart declares all four
  (migration 00027), so the output is byte-identical across both changes — verified by hashing the
  rendered rows (`f700ed0aa0ad`) — and `TestIntegration_CashControlsFollowTheOrganisationTag` pins
  both halves: a chart tagging `1100` is looked through at `1100`, and a chart declaring nothing
  resolves to nothing.
* **The 897.35 figure: withdrawn** — it was never in the data.
* **The 250.00 of contamination on `091`/`400` belongs to another session** working in the same
  worktree and the same database; it is left in place and is not counted against any report.
* **The tax control account was a hardcoded account code: closed** (2026-09-11 late). The write
  path resolved the account a document's tax posts to with `accountIDByCode(ctx, tx, orgID,
  "820")` — the code `820 Sales Tax` of this capture's chart — at the three places this item used
  to name. It is now `systemAccountIDOrNil(ctx, tx, orgID, models.SystemAccountGST)`
  (`internal/repository/gl.go`), which is the same lookup `debtorsAccountID`/`creditorsAccountID`
  already used, so the account is read from the role the chart declares rather than from the code
  this chart happens to carry. The behaviour that was documented as the risk is unchanged and now
  has a name: a chart that declares no `GST` role still folds the tax into the counterpart line
  instead of posting it to a control account — `systemAccountIDOrNil` is the "defined behaviour for
  an absent role" form, and `TestIntegration_SystemAccountResolutionIsByRole`
  (`internal/repository/cash_controls_test.go`) asserts both that the absent role reports itself as
  absent and that `systemAccountID` — the strict form — errors naming the role. No account code
  remains in the write path: the same grep as before the change now returns nothing over
  `internal/repository/`.

  ```sh
  grep -nE "['\"](090|091|200|300|400|610|710|720|800|801|820|840)['\"]" \
    internal/repository/report.go internal/handlers/report_render.go internal/handlers/report.go
  ```

  This was briefly false: the Cash Summary's first delivery named its four control accounts as SQL
  literals and the item above removes them again.
* **The account codes the frontend carried: closed** (2026-09-11 late). The same defect, one
  layer out. `web/src/lib/chart-of-accounts.ts` answered the Accounts screen's YTD column from a
  thirty-odd-entry `YTD_BY_CODE` table describing no organisation's books (its own comment called
  the values "illustrative"), and offered to "Import standard chart" from a twelve-account
  `STANDARD_CHART_IMPORT` array — 090 Checking, 120 Accounts Receivable, 200 Accounts Payable, 220
  Sales Tax, 400 Sales. Those are not this chart's rows: its Accounts Receivable is `610`, its
  Sales Tax `820`, it has no account at `120` or `220`, and its `200` is Sales, a revenue account.
  Importing that array here would have created a second Sales at `400` beside the real one at
  `200`. Both are gone:
  * the YTD column is now read from the Trial Balance (`ytdByCode(report)`), which is where this
    application measures YTD and the one report whose per-account figures are checked against the
    capture. Xero prints that column unsigned — all 27 non-zero YTDs in the capture are positive,
    including the twelve credit balances — so the magnitude of YTD Debit − YTD Credit is what is
    shown, and an account the Trial Balance does not list stays blank rather than reading zero.
  * "Import standard chart" now reads `GET /api/v1/accounts/standard-chart`, served from
    migration 00029's reference table, whose 58 rows are the capture's own code, name, tax rate and
    description verbatim and in its order (`migrations/data/xero/accounts.csv`). The capture's
    `ytd` column is deliberately not carried: a chart template that shipped a balance would be
    inventing an opening figure for whichever organisation imported it. `TaxType` is resolved per
    tenant against that organisation's own `tax_rates` — the reference table carries the name Xero
    prints, the API speaks the type — and `Class` is derived from `Type` rather than stored twice.
    `TestHTTP_StandardChart` pins all of it against the capture.
  * `AccountCode: '200'` was the default account on a new invoice line, and the conversion-balances
    screen seeded itself from `['090','120','200']` with a fabricated opening debit of 4,130.98.
    A new line now starts with no account and the picker says so, and conversion balances seeds
    from the organisation's own chart — the bank accounts by type, the two document control
    accounts by declared role — with no balance filled in, an opening balance being the one figure
    in the books that can come from nowhere but the person entering it.
* **The bank-coding entry path's rate with no tax amount: closed** (fixed 2026-09-11 ~22:00 local).
  `internal/handlers/bank_statement.go` took `TaxAmount` from the request and wrote it through
  (`zeroIfNil`), and `internal/repository/gl.go` copied it onto the journal line, so a caller who
  named a tax rate and omitted the amount left `gl_journal_lines.tax_amount` at 0.00 with a
  non-zero rate — a line whose tax the ledger does not record. The reconcile screen cannot do
  anything else: it sends the rate and has no field for an amount (`web/src/lib/api.ts`, the
  `statementLines.create` payload). So this was never a choice between computing and refusing —
  computing is the only thing the caller's own interface allows, and it is what Xero does. Every
  bank transaction is now written through `resolveLineTaxes`/`lineTax` in
  `internal/repository/bank_transaction.go`, which fills the amount in from
  `tax_rates.effective_rate` — out of the line when `LineAmountTypes` is `Inclusive` (so net + tax
  still comes to the figure the statement states), on top of it when `Exclusive` — and leaves a
  stated amount, an untaxed line (`NONE`/empty) and an unknown rate exactly as the caller gave
  them. `postBankTransactionJournal` was corrected with it: it had been treating a line's own
  figure as the net unconditionally, which was invisible only because inclusive lines always
  carried a tax of 0.00. `recalculateBankTx` now splits an inclusive total (`SubTotal = Total −
  Tax`) so the header figures and the lines agree. Verified end to end through the API, not only in
  the repository: `TestHTTP_BankStatementCreateDerivesTheTaxFromTheRate` codes the capture's own
  −42.50 statement line under `INPUT` and gets the line's tax 3.24, `SubTotal` 39.26, `TotalTax`
  3.24 and `Total` 42.50 — the total the statement states, with the tax taken out of it rather than
  added to it. Journal 1614 itself is another session's row and was not touched; the Sales Tax
  report still prints its 0.00 and still names it in the caveat. See
  `docs/sales-tax-impl.md` section 6 and its amendment.
## 7. Reproducing the checks

```bash
TOK=$(curl -s -X POST http://localhost:8080/api/auth/login -H 'Content-Type: application/json' \
      -d '{"email":"admin@demo.local","password":"admin123"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')
ORG=6823b27b-c48f-4099-bb27-4202a4f496a2
Q="fromDate=2026-01-01&toDate=2026-12-31&date=2026-12-31"
curl -s "http://localhost:8080/api/v1/reports/trial-balance?$Q" -H "Authorization: Bearer $TOK" -H "Xero-Tenant-Id: $ORG"
```

Note that the Trial Balance endpoint takes `date`, not `toDate`; `toDate` is ignored
there, so a request carrying only a range silently reports as at today. The other
as-at reports take `date` as well.

Ledger totals:

```bash
docker exec bonefish-postgres-1 psql -U goxero -d goxero -c \
"SELECT SUM(net_amount) FILTER (WHERE net_amount>0) pos, SUM(net_amount) FILTER (WHERE net_amount<0) neg, COUNT(*) lines
   FROM gl_journal_lines l JOIN gl_journals j USING (journal_id)
  WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2';"
```

The dev database on :5432 is shared with a second session in this worktree that is writing
to the same demo organisation, so bank-account figures can move between two reads. Take
the timestamp with any figure you compare.

## 8. The independent verification, and what changed after it

`docs/reports-verification.md` is the work of a worker that built none of this and was told
to find out whether the claims were true rather than to make them true. Its verdict, over
the 18 report paths the router registers under `/api/v1/reports` (20 routes — `profit-loss` and
`general-ledger` are aliases of two of them) plus the four front-end stub pages the table lists at
the end, which are pages rather than endpoints:

| Outcome | Reports |
| --- | --- |
| Reproduces Xero to the cent | **2** — Aged Receivables, Aged Payables |
| Reproduces Xero except one difference of exactly 250.00, traced to one named journal | **4** — Trial Balance, Profit and Loss, Balance Sheet, Bank Summary |
| Differs from Xero beyond the 250.00 — in presentation, not in any total | **3** — General Ledger Detail, Account Transactions, Journal Report |
| Differs fundamentally — a different measure, not a layout | **1** — Cash Summary |
| No Xero capture to compare; internally consistent, every derivable figure agrees | **6** — Sales Tax / BAS, Executive Summary, Budget Summary, Form 1120, Aged Receivables by Contact, Aged Payables by Contact |
| Deliberate non-report | **1** — `invoice-summary` |
| Front-end stubs that say honestly they are not wired to the API | **4** — business-snapshot, health, visualisations, short-term-cash-flow |

The verifier independently derived the same 3,120.54 decomposition as section 4 above, item
by item, and confirmed every one of them is a Dr/Cr wash inside a single account. It
re-derived debits = credits = 108,392.54 over 431 lines in 157 journals, the Trial Balance
Total row against both its own rows and the ledger aggregate, assets = liabilities +
equity, and each bank account's closing balance, from the database.

### Three findings, and what was done with each

1. **A report printed different figures on two calls of the same URL.** General Ledger
   Detail's running `Balance` column is accumulated in the order the query returns, and
   `journalFeed`'s ordering key — `(account code, journal date, journal id)` — is not
   total: two lines of one journal posting to one account on one date tie on all three, so
   the heap decided their order. The verifier observed the same row printing −28,966.43 in
   one capture and −28,555.76 in another. **Fixed**: `l.line_id` is now the last key
   (`internal/repository/report.go`), which makes the order total. Eight back-to-back
   fetches of the report body now hash identically, and a regression test
   (`TestHTTP_Reports_GlDetailLineOrderIsStable`) posts three journals of three tied lines
   each and asserts the rendered order against `ORDER BY … l.line_id`: it fails three times
   out of three with the tiebreaker removed and passes with it in place. The same query
   backs Account Transactions and Journal Report, which print no running balance and so
   only had their row order permuted.
2. **The Sales Tax caveat sentence was false.** It claimed no posted journal line carries a
   tax amount; the ledger has 33 such lines totalling 74.16 and 122 lines with a `tax_type`,
   and `invoice_line_items` has 85 lines with a tax amount totalling 4,777.98. **Fixed**: the
   sentence now states what the report actually tests — that no tax rate carries a tax
   amount on *every* one of its posted lines — and says plainly that some lines do record
   one. The verifier's report quotes the old sentence, because it verified the binary that
   was running before this change; the tree was already fixed when its report landed.
3. **A path the API advertises had no browser route.** `GET /api/v1/reports/bas` serves the
   report and the index advertises `/reports/bas`, but `/app/reports/bas` was a SvelteKit
   404. **Fixed**: the route now answers 308 to `/app/reports/sales-tax`, the single page
   that renders this report, rather than rendering one report twice under two names.

The server on :8080 was rebuilt and restarted from the tree that carries all three fixes,
and no `.go` file in the tree is newer than the running binary.

### What the verifier found that was deliberately not changed

* **Three endpoints ignored part of their query string — since fixed** (see below). Trial
  Balance and Executive Summary read `date` and ignored `toDate`; Budget Summary ignored
  `fromDate`/`toDate`. It was left alone at the time because widening an endpoint's accepted
  parameters changes its contract; it was then fixed anyway, because the contract it changed to
  is the one the reports are named after.
* **`docs/xero-reference/reconciliation.md` §4, §5 and §6 contain four claims the captured
  files do not support.** The reference is frozen and was not edited; the four claims, each
  with its measurement and its repro command, are recorded in section 9 below and in
  `docs/reports-verification.md` §1.10 and §1.16.
* **The three parity decisions of section 5 remain open product decisions**, not defects:
  `840`'s placement in the Cash Summary, the caveat that states the Sales Tax ledger's own gap
  rather than hiding it, and the reports that say they are stubs instead of printing a zero.
  They are decisions because both answers are defensible and the capture is the only evidence
  for either; they are stated in the reports themselves, not only here, which is what makes
  them decisions rather than defects.

### Since that verification (2026-09-11 ~21:45 local)

* The Trial Balance's column model and the Sales Tax report were both closed after it, so the
  Sales Tax row of the table above ("No Xero capture to compare") no longer describes the
  report. The capture's own tax column does fix its figures, and they now match it exactly:
  Tax Collected 2,437.80 / Tax Paid 2,015.21 / Net Tax 422.59. Sections 5 and 6.
* All 18 report paths registered under `/api/v1/reports` were re-fetched from the restarted
  :8080 and answer 200. `invoice-summary` is the one that returns a bare summary object rather
  than a report envelope; that is its contract, not a failure.
* Trial Balance re-compared account by account: 20 of the 25 capture accounts exact, the five
  differing accounts and the extra `091` tabulated in section 4, and every one of those six
  differences traced to the other session's journal 52 (16:37 local) and journals 1612–1615
  (19:30 local). No figure on either side was adjusted to make a line agree.
* The changes since are confined to two functions' blast radius, checked by grep rather than
  assumed: `SplitSigned` is called only from `trialBalanceMeasure`, and `SalesTaxByRate` only
  from the Sales Tax handler — no other report reaches either. The other sixteen reports were
  not edited and their endpoints were re-fetched clean.
* The Cash Summary was closed after it (sections 5 and 6): the classification now looks through
  the document control accounts to the documents the cash settled, the three Plus Tax Movements
  rows carry the tax the ledger records beside each attributed line, and every account of a
  cash-moving journal lands in exactly one section. Its own verification: all 18 report paths
  re-fetched from the restarted :8080, 200 each; the Cash Summary's row payload hashed before and
  after the control-account change and identical; all 26 `/app/reports` browser routes re-swept
  with no error element and no 404; `gofmt -l .` empty, `go build ./...`, `go vet ./...`,
  `go test ./internal/... -count=1` and `bun run check && bun run build` all clean.
* The bank-coding entry path was fixed after it (section 6): a line coded from the reconcile
  screen now reaches the ledger with the tax its rate implies rather than with a rate and a
  tax of 0.00. It touches no report function — `resolveLineTaxes`/`lineTax` in
  `internal/repository/bank_transaction.go`, the inclusive branch of
  `postBankTransactionJournal` and `recalculateBankTx` — and the reports' figures are
  unchanged by it, because it changes only what future entries write. Existing rows, the
  imported dataset among them, are untouched.

* The hardcoded account codes the frontend carried were removed in the same pass (section 6):
  the Accounts screen's YTD column is now the Trial Balance's own figures rather than a written-in
  table, and "Import standard chart" reads migration 00029's reference rows — the capture's own
  58 accounts — through `GET /api/v1/accounts/standard-chart` instead of a twelve-account array
  in the source. `TestHTTP_StandardChart` checks the endpoint against the capture row by row.
* The Cash Summary's control-account fallback was removed, and the tax control account in the
  write path was resolved by role instead of by code (both in section 6). A chart that declares no
  role now leaves that money unclassified and refuses to post, rather than following a code that
  belongs to another chart.
* The three query-string parameters are read: `asAtParam` accepts `date` or `toDate` for Trial
  Balance, Executive Summary and Budget Summary, and Budget Summary reads `fromDate` through
  `optionalDateParam`. `TestHTTP_Reports_ToDateSetsThePeriod` pins that `toDate` moves the period
  and that naming neither leaves each report's documented default.
* Gates for this pass: `gofmt -l .` empty; `go build ./...`, `go vet ./...` and
  `go test ./internal/... -count=1` clean; `bun run check` 0 errors and `bun run build` clean;
  the 18 report endpoints re-fetched 200; the Cash Summary's rendered rows still
  `f700ed0aa0ad`.

## 9. Errata to `docs/xero-reference/reconciliation.md` — recorded, not applied

`reconciliation.md` is the poster's own account-by-account reproduction of Xero's Trial
Balance, and it is the claim the verification tested. Four of its statements are
contradicted by the captured files it cites. **The reference was deliberately left
unedited**: it is the evidence the verification rests on, and rewriting a claim to match a
later measurement would destroy the record of what was claimed. They are recorded here so a
reader is not misled by them.

| # | Where | The claim | What the captured file actually holds |
| --- | --- | --- | --- |
| 1 | §4 table, "Checks" | "Sum of reproduced debits **42,595.46**" / "Sum of reproduced credits **42,595.46**" | That is the **net-per-account** figure. The reproduction's own ledger totals **108,392.54** debit and 108,392.54 credit gross (108,142.54 excluding the 250.00 contamination of section 5). The same document's §5 gives Xero's *gross* GL total as 111,263.08, so §4 and §5 of one document measure two different things under one word, "sum". |
| 2 | §5, opening prose | "It holds **543 ledger lines** in 27 account sections" | The file holds **461** date-prefixed ledger lines across 27 sections — and **§5's own table sums to 461** (85+40+1+3+2+6+3+4+2+2+3+3+17+3+3+3+4+19+59+6+1+2+65+5+109+1+10). The prose contradicts its own table by 82 lines; there are no hidden 82 lines anywhere in the file (548 non-blank lines = 461 ledger lines + 5 preamble + 27 `##` headers + 27 `Total <account>` rows + 27 `Net movement` rows + 1 grand Total). |
| 3 | §5, closing sentence | 630 Inventory and 877 Tracking Transfers "are the reason the GL grand total is 111,263.08 while the Trial Balance total is 42,595.46" | Those two accounts contribute **1,720.27** of that **68,667.62** difference — 320.00 + 1,400.27, the Dr/Cr turnover each of them carries in excess of its nil net — i.e. **2.5%** of it. The other 66,947.35 is ordinary turnover inside the 25 balanced accounts, which is what "gross versus net" means; attributing the whole gap to two nil accounts is wrong in kind, not just in degree. |
| 4 | §6, opening sentence | "`docs/xero-reference/journal-report.txt` — **1,280 journal lines**. Posted per account code the lines net to the same 25 balances (**sum of debits = sum of credits = 42,595.46**)" | The file holds **751** journal lines (751 rows begin with a date) and its own grand Total row reads **194,720.04 / 194,720.04**. 1,280 appears nowhere in it. The 42,595.46 is, again, the net-per-account figure and not this file's total. The database holds 431 of those lines, in 157 journals. |

Everything else in `reconciliation.md` that was checked held: all **27** of §5's per-account
line counts match the file section by section, each section's net equals the Trial Balance
figure, §2's document counts match the import, and the 82 bank rows, 30 invoices, 35 bills, 5
credit notes, 4 expense-claim rows and 2 opening-balance rows of §4 are all present in the
database.

### Repro

```sh
cd docs/xero-reference
grep -cE '^[0-9]{1,2} (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) [0-9]{4}\t' general-ledger-detail.txt   # -> 461
grep -c '^##' general-ledger-detail.txt                                                                        # -> 27
grep -cE '^Total\t' general-ledger-detail.txt                                                                  # -> 1  (grand total, 111,263.08)
grep -cE '^Total ' general-ledger-detail.txt                                                                   # -> 27 (per account)
grep -cE '^[0-9]{1,2} (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) [0-9]{4}' journal-report.txt          # -> 751
grep -E '^Total\t' journal-report.txt                                                                          # -> 194,720.04 / 194,720.04

docker exec bonefish-postgres-1 psql -U goxero -d goxero -tA -c "
SELECT to_char(SUM(CASE WHEN net_amount>0 THEN net_amount ELSE 0 END),'FM999999990.00') AS gross_debit,
       to_char(SUM(CASE WHEN net_amount<0 THEN -net_amount ELSE 0 END),'FM999999990.00') AS gross_credit,
       count(*) AS lines FROM gl_journal_lines;"
# -> 108392.54 | 108392.54 | 431
```

Claim 3's arithmetic: 111,263.08 − 42,595.46 = 68,667.62; of that, 630 contributes 320.00
and 877 contributes 1,400.27 on each side, 1,720.27 in all.
