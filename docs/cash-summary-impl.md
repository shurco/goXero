# Cash Summary — what was implemented, and how it was checked

## AMENDMENT (coordinator, 2026-09-11 ~22:40 local) — read this before §1.2 and §10

The worker that wrote this record stopped after its edits landed; I verified the delivery against
the capture and then changed one thing in it. Nothing it measured is withdrawn: every figure in
§2, §3 and §4 reproduces exactly, including the three tax rows and the 304.50 of contamination.

**What changed.** `CashSummary`'s query named the accounts it looks through as SQL literals —
`'610'`, `'800'`, `'801'` in the `bank` CTE's control lookup, `'800'`/`'610'` in `doc_attr` and
`cn_attr`, `'801'` in `claim_attr`, and `'820'` in `claim_attr` and `direct_attr`. They are now the
query parameters `$5`–`$8`, resolved once by `ReportRepository.cashControls`
(`internal/repository/report.go`, immediately above `CashSummary`): the two document control
accounts come from the organisation's own `system_account` tags. Four roles are resolved, the
ones the report classifies through — `DEBTORS`, `CREDITORS`, `UNPAIDEXPCLM`, `GST`, declared for
this chart by migration 00027 — each read from the account that holds the role, and from nothing
else. As first written, this amendment still kept Xero's own code as the fallback for a role a
chart does not declare; the second amendment below removed that fallback, so there is one source
and no second answer. The codes are still resolved in one place, with their reasoning in
`cashControls`, rather than spelled out at each of the seven places the query used to carry one.

**Why.** This chart tags `610` and `800`, so the report's output does not change here at all — I
hashed the rendered rows before and after and they are identical (`f700ed0aa0ad` both times). The
point is the organisation whose Accounts Receivable is not 610: the tag is exactly the mechanism
the schema provides for saying which account it is, the report was ignoring it, and an untagged
code that happens to be right for one chart is the kind of hardcoding the whole parity exercise is
against. `TestIntegration_CashControlsFollowTheOrganisationTag`
(`internal/repository/cash_controls_test.go`, new) pins both halves: a chart tagging `1100` as
DEBTORS is looked through at `1100`, and a chart that declares nothing resolves to nothing — the
second half of that sentence read "gets the documented defaults" until the fallback was removed,
below.

**Where the amendment is.** `internal/repository/report.go`: `cashControlCodes`/`cashControls` new;
`CashSummary` takes four parameters and its five literals are gone
(`grep -nE "'(610|800|801|820)'" internal/repository/report.go` returns nothing).
`internal/repository/cash_controls_test.go` is new. Nothing else in this record's change set was
touched: `cashSectionFor`, `CashCategoryRow`, `CashSummaryReport`, the CTEs' logic, the renderer
and the migration are the worker's, exactly as §1 and §10 describe them, and the tax convention of
§1.2 is unchanged.

## SECOND AMENDMENT (coordinator, 2026-09-11 late) — the fallback is gone

**What changed.** `cashControls` no longer answers a role the chart has not declared with Xero's own
code for it. It reads the chart's own `system_account` rows and returns what it finds; a role
nobody holds comes back as `""`, which no account code equals, and the query then classifies none
of that money instead of attributing it to the account that held the code in the chart this was
written against. `TestIntegration_CashControlsFollowTheOrganisationTag` already asserted exactly
that (`assert.Equal(t, "", then.receivable, "an undeclared role resolves to no account, not to a
code")`) — the assertion was right and the code was not yet; it passes now.

**Why.** A code carried as a fallback is a fact about one chart in a second place, and it is the
worse of the two places to keep it: the tag is visible in the chart and the fallback is not, so an
organisation that had not declared its sales tax account would have had 820 classified as its tax
account silently. Which roles are resolved, and why each resolves to nothing when undeclared, is
stated once in `cashControls`' doc comment and nowhere else.

**Not withdrawn.** Nothing this record measured changes: the demo chart declares all four roles
(migration 00027), so the rendered rows are still `f700ed0aa0ad`. `TestHTTP_Reports_CashSummary*` and the
18-path sweep are unchanged. This pass also removed the account-code literals from the
frontend and made "Import standard chart" read a chart instead of a literal — recorded in
`docs/reports-xero-parity-summary.md`, not here.

**Gates after the amendment**, in a tree that also carries the coordinator's entry-path tax fix:
`gofmt -l .` empty; `go build ./...`, `go vet ./...`, `go test ./internal/... -count=1` all clean;
`bun run check` 0 errors, `bun run build` clean; all 18 report endpoints 200; the Cash Summary
re-fetched from the restarted :8080 and byte-identical.

Scope: `docs/cash-summary-xero-rule.md` §1 (the classification rule) implemented in the
repository and the renderer, the three tax rows the renderer used to emit empty, the caveat
sentence that had become false, and the migration that gives the report the document links it
reads. Tests, gates and the reasoning behind the one deliberate divergence from the capture are
below.

Everything here was measured against the running stack, not reasoned about: the demo database
(`bonefish-postgres-1`, db `goxero`), organisation `6823b27b-c48f-4099-bb27-4202a4f496a2`
(Xero short code `!!6Sp3`), through `GET /api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-12-31`.

---

## 1. What changed, and where

### 1.1 `migrations/00026_repair_cash_summary_document_links.sql` (new, 359 lines)

Goose version 26, applied. Up at lines 1–300, Down at 302–359.

The classification rule joins `payments.invoice_id` and guesses nothing at run time, so the links
have to be right in the data. Migration 00024 wrote them by matching the captured document number
on the payment row against the contact's documents, and deliberately wrote NULL when the contact
had more than one document with that number, or none. Twelve of the 24 `ACCPAYPAYMENT` rows came
out NULL.

What Up does:

- Links those twelve payments to the bill each one cleared, under fixed `uuid_generate_v5` keys,
  and only where `p.invoice_id IS NULL`. Each of the twelve is named with the evidence that picks
  it out (migration lines 134–151); the evidence is captured data only — the payment's amount and
  date, the bill's number, date, total and `amount_paid`, and the bank transaction's own reference.
  Three keys are used, because the number alone is not enough:
  - (a) contact + document number + amount — 4 rows (`Net Connect` and `PowerDirect` each have
    three bills numbered `Rpt`, and within one contact and one number the amount picks one out).
  - (b) contact + document number + amount + earliest still unsettled — 5 rows (`Xero`,
    `Truxton Property Management`).
  - (c) contact + the bill's own settled total — 3 rows (`PC Complete`, `Swanston Security`,
    `Gateway Motors`).
- Writes the five credit-note allocations the capture implies, with `ON CONFLICT DO NOTHING`
  (line 161 onwards), and sets the `invoices.amount_credited` those allocations imply, only where
  the column is 0.
- Runs six checks (line 194) that fail the migration rather than leaving a half-repaired dataset.

It writes **no** `gl_journals` or `gl_journal_lines` row, moves no balance and changes no amount.
A payment row carries no ledger entry of its own; the money side is the bank transaction the
capture already holds. What it touches is `payments.invoice_id`, `credit_note_allocations` and
`invoices.amount_credited`, and nothing else.

Down (line 302) reverses exactly what Up did, because every predicate it uses is one Up required:
it clears `invoice_id` only on the twelve named payments and only where the column holds the id Up
set it to; it deletes only the five allocation ids Up could have created; it zeroes
`amount_credited` only where the column holds the note total Up wrote. A link or an allocation
another session adds is named nowhere and is touched in neither direction.

Verified in the demo database at 2026-09-11 22:15 (`goose_db_version` 26 applied):

| check | result |
| --- | --- |
| `ACCPAYPAYMENT` rows / with `invoice_id` | 24 / 24 |
| `ACCRECPAYMENT` rows / with `invoice_id` (00024's work, untouched) | 20 / 20 |
| `credit_note_allocations` in the organisation | 5 |
| invoices with `amount_credited <> 0` | 5 |

Up and Down were also round-tripped inside a rolled-back transaction when the migration was
written; no `migrate-down` was run against the shared database.

**What it could not evidence** (stated in the migration's own section 4, lines 105–120, and
repeated here because it bounds the report):

- No credit note is allocated anywhere the three keys do not pick out one document uniquely, and
  no allocation is inferred from "the contact's oldest open invoice". Where the Cash Summary needs
  an allocation it is because a payment cleared something; the report says so where it cannot.
- No payment is split across several bills. `bills.csv` shows one bill per payment here and
  `payments.invoice_id` is a single column — a payment that covered two bills could not be
  recorded in it at all. Nothing in this capture is in that state: every amount above matches one
  bill's `amount_paid` exactly.

### 1.2 `internal/repository/report.go`

| what | lines |
| --- | --- |
| `CashSection` constants | 703–707 |
| `cashSectionFor` (account class → section) | 709–727 |
| `CashCategoryRow` — adds `Section`, `Tax`, `Unattributed` | 730–755 |
| `CashSummaryReport` — adds `CreditNoteTax`, `TaxCollected`, `TaxPaid`, `OtherMovement` | 757–786 |
| `CashSummary` — the aggregate query | 788–987 |
| row loop: section + tax tallies | 957–964 |
| credit-note tax query and the add/subtract | 977–986 |

The whole classification is one query (a chain of CTEs) plus the two opening/closing balance
reads that were already there. The CTEs, in order — the account codes quoted in the first and
last of them are the ones the query carried when this section was written; the amendments above
replaced them with the codes the organisation's declared roles resolve to, which are this chart's
`610`/`800`/`801`/`820` and were `'610'`/`'800'`/`'801'`/`'820'` written in:

- `bank` — every period journal that has a line on a `BANK` account, carrying the control account
  the journal itself names (`a2.code IN ($5,$6,$7)`, the codes the organisation's declared roles
  resolved to; as first written `a2.code IN ('610','800','801')` — see the amendments above). The
  control is read off the journal's own lines, never off the counterpart's type.
- `journal_cash` — the cash that moved through each control account: the sum of the journals'
  `BANK` lines, grouped by control.
- `doc_pay` — the period's payments joined to their invoice, with
  `frac = LEAST(1, (payment.amount + allocated) / invoice.total)`.
- `doc_attr` — the invoice's own line coding, pro-rated by `frac`, signed by `dp.doc_type`
  (`ACCPAY` → `−1`, otherwise `+1`).
- `cn_attr` — the credit notes the period's cash settled with, pro-rated by the allocation, signed
  the opposite way, so a credit note subtracts from the accounts it names; its sign is governed by
  the document it credits (`dp.doc_type = 'ACCPAY'`), not by the note's own type.
- `claim_attr` — expense claims (`801`): the `EXPENSECLAIM` journal matched by the opposite net
  amount on the `801` line, read for its coded lines rather than the control.
- `direct_attr` — every other line of a cash-moving journal: not on a bank account, not on `820`
  Sales Tax, and not on the control account the journal itself names. It is taken in the cash
  direction `−l.net_amount` (`a.code IS DISTINCT FROM b.control` is also what makes a journal with
  no control account at all fall here).
- `residual` — `journal_cash(control) − attributed(control)`, the part of a control account's own
  cash this ledger links to no document at all.

Each line's amount and tax are rounded to the cent **per line**
(`ROUND(li.line_amount * frac, 2)`, `ROUND(li.tax_amount * frac, 2)`), not once at document level.
That is load-bearing, see §7 item 2.

The tax convention, and why it is what it is:

```
Tax Collected = Σ Tax(income rows)  + CreditNoteTax
Tax Paid      = Σ Tax(every other row) − CreditNoteTax
Net           = Σ all row Tax       = 370.55
```

`CreditNoteTax` is the tax on **all** of the organisation's credit-note allocations (106.82), not
only those whose document took cash inside the window (24.32). Both readings are defensible — the
credit note's own lines are already subtracted from the accounts they name, tax included, so its
tax has to come back somewhere — but only the unrestricted reading reproduces the capture's
1,844.56 / (1,474.01). The amount is added to one line and subtracted from the other, so it cancels
in Net Tax Movements and the bottom line is the same either way; the tests pin the figures, not the
convention. This is also why the AR cash exceeds Sales + Tax Collected by exactly 106.82, which is
what the rule document's §4 arithmetic already observed.

### 1.3 `internal/handlers/report_render.go`

| what | lines |
| --- | --- |
| `renderCashSummary` (doc comment, then the function) | 748–859 |
| partition `cs.Categories` by `row.Section` | 788–798 |
| `cashSummarySection` — marks unattributed rows | 872–908 |
| `cashSummaryUnattributedMark` | 911–914 |
| `cashSummaryCaveat` — rewritten | 916–934 |
| `cashSummaryCoverage` — new | 936–955 |
| tax section populated, `Net Tax Movements` = the sum of the two rows | 817–837 |

Three things changed here:

1. **The sections are filled by `row.Section`, not by a policy the renderer holds.** The renderer
   previously decided which accounts went where; now the repository says and the renderer only
   partitions. Every account of a cash-moving journal lands in exactly one section, so the sections
   partition the movement instead of approximating it.
2. **The three tax rows carry figures.** They were `nil` before — printed as empty cells. They are
   now `cs.TaxCollected`, `cs.TaxPaid` and their sum, and `Net Cash Movement` is
   `surplus + otherMovement + netTax`, so the movement is still an identity of the printed rows.
3. **`cashSummarySection` marks an unattributed row** by appending
   ` (not attributed to a document)` to its label, and `cashSummaryCoverage` states the total.

The caveat. The sentence that had become false —

> Tax Movements (a line's tax amount is recorded beside its net amount rather than posted to a tax
> account, so no account's movement is the tax and **this report will not assume which account
> holds it**).

— no longer describes the report: the tax is now measured from each attributed line rather than
assumed. It is replaced with what is still true, and the variance clause and the year-average
clause are kept (that half was never false):

> Not represented: the Yearly average (YTD), Variance and Variance for Variance columns (…). Tax is
> measured rather than assumed: each attributed line contributes the tax this ledger records beside
> its net to Plus Tax Movements, and the tax a journal posts a second time as a line on 820 Sales
> Tax is left out rather than counted twice. Coverage: …

### 1.4 Tests

- New: `internal/handlers/cash_summary_classification_integration_test.go` (package
  `handlers_test`), five tests, window `2026-02-01 … 2026-02-28`.
- Changed: `internal/handlers/reports_backend_fixes_integration_test.go:208`
  `TestHTTP_Reports_CashSummaryIsItsOwnReport`, exactly the rewrite §7.4 predicted. Three
  assertions moved: the three tax cells now assert `0.00` ("must be a measured figure") instead of
  emptiness; the titles assertion now checks `Tax Movements` and `Coverage:`; and the footing
  assertion is now `Surplus + Total Other Cash Movements + Net Tax Movements`. The `rowLabels`
  expectation was left as it is — it still holds for the fixture, which has no `610/800/801/820`
  row and no unattributed amount.

---

## 2. The capture, line by line

`docs/xero-reference/cash-summary.txt` (read only, never edited) against the report fetched at
**2026-09-11 22:15:26 +02:00**. "Cause" is one of: **match**, **(i)** the deliberate 840
placement, **(ii)** contamination — rows the shared database's other session has added since the
capture was taken.

| # | capture line | capture | report | Δ | cause |
| --- | --- | --- | --- | --- | --- |
| 1 | Income · Sales | 21,054.73 | 21,030.73 | −24.00 | (ii) |
| 2 | Income · Total Income | 21,054.73 | 21,030.73 | −24.00 | (ii) |
| 3 | Less Expenses · Advertising | 5,500.00 | 5,750.00 | +250.00 | (ii) |
| 4 | Less Expenses · Bank Fees | 30.00 | 45.00 | +15.00 | (ii) |
| 5 | Less Expenses · Cleaning | 1,110.00 | 1,110.00 | 0.00 | match |
| 6 | Less Expenses · Consulting & Accounting | 58.00 | 58.00 | 0.00 | match |
| 7 | Less Expenses · Entertainment | 1,553.60 | 1,553.60 | 0.00 | match |
| 8 | Less Expenses · General Expenses | 46.19 | 46.19 | 0.00 | match |
| 9 | Less Expenses · Historical Adjustment | (4,130.98) | *(in Plus Other Cash Movements)* | — | **(i)** |
| 10 | Less Expenses · Light, Power, Heating | 235.50 | 235.50 | 0.00 | match |
| 11 | Less Expenses · Motor Vehicle Expenses | 654.36 | 654.36 | 0.00 | match |
| 12 | Less Expenses · Office Expenses | 700.37 | 715.87 | +15.50 | (ii) |
| 13 | Less Expenses · Printing & Stationery | 94.41 | 94.41 | 0.00 | match |
| 14 | Less Expenses · Rent | 3,273.66 | 3,273.66 | 0.00 | match |
| 15 | Less Expenses · Repairs and Maintenance | 1,745.61 | 1,745.61 | 0.00 | match |
| 16 | Less Expenses · Telephone & Internet | 186.37 | 186.37 | 0.00 | match |
| 17 | Less Expenses · Travel - National | 209.68 | 209.68 | 0.00 | match |
| 18 | Less Expenses · Total Expenses | 11,266.77 | 15,678.25 | +4,411.48 | (i) +4,130.98, (ii) +280.50 |
| 19 | Surplus (Deficit) | 9,787.96 | 5,352.48 | −4,435.48 | (i) −4,130.98, (ii) −304.50 |
| 20 | Plus Other Cash Movements · Fixed Assets | (2,728.29) | 710 Office Equipment (923.79) + 720 Computer Equipment (1,804.50) | 0.00 | match (layout, see below) |
| 21 | Plus Other Cash Movements · Historical Adjustment | *(Xero prints this line under Less Expenses — row 9)* | 4,130.98 | +4,130.98 | **(i)** |
| 22 | Plus Other Cash Movements · Total Other Cash Movements | (2,728.29) | 1,402.69 | +4,130.98 | (i) |
| 23 | Plus Tax Movements · Tax Collected | 1,844.56 | 1,844.56 | 0.00 | **match** |
| 24 | Plus Tax Movements · Tax Paid | (1,474.01) | (1,474.01) | 0.00 | **match** |
| 25 | Plus Tax Movements · Net Tax Movements | 370.55 | 370.55 | 0.00 | **match** |
| 26 | Net Cash Movement | 7,430.22 | 7,125.72 | −304.50 | (ii) |
| 27 | Summary · Opening Balance | *(blank)* | 0.00 | — | match (Xero prints no opening figure; no bank journal is dated before 1 Jan 2026) |
| 28 | Summary · Plus Net Cash Movement | 7,430.22 | 7,125.72 | −304.50 | (ii) |
| — | Summary · Cash Balance | 7,430.22 | 7,125.72 | −304.50 | (ii) |

The 28 numbered rows above are the capture's own numbered lines; `Cash Balance` is the 29th and
last row of the capture and is listed for completeness.

### Every difference, attributed

**(ii) Contamination — the shared database moved under the capture.** Four journals were written
into organisation `6823b27b-…` by another session on 2026-09-11, all after the capture. Times are
`gl_journals.created_date_utc` in the container's `+02` timezone:

| created | journal date | account | amount | effect on the report |
| --- | --- | --- | --- | --- |
| 16:37:09 | 2026-03-05 | 400 Advertising | 250.00 | Advertising +250.00, Total Expenses +250.00 |
| 19:30:45 | 2026-09-10 | 200 Sales | 12.00 | Sales −12.00 |
| 19:30:47 | 2026-09-10 | 404 Bank Fees | 15.00 | Bank Fees +15.00 |
| 19:30:50 | 2026-09-10 | 453 Office Expenses | 15.50 | Office Expenses +15.50 |
| 19:30:53 | 2026-09-09 | 200 Sales | 12.00 | Sales −12.00 |

Their sum is exactly the movement difference: −250.00 −15.00 −15.50 −24.00 = **−304.50**, which is
`Net Cash Movement` and `Cash Balance` to the cent. None of them carries tax (`tax_amount = 0.00`),
which is why the three tax lines match the capture exactly. No figure on either side was adjusted
to make a line agree.

The 453 Office Expenses row is journal **1614** — the row the coordinator flagged by name, and it
is confirmed here: `BANKTRANSACTION`, 2026-09-10, `453 Office Expenses` +15.50 and `090` −15.50,
`tax_type = INPUT` with `tax_amount = 0.00`. It is the other session's and was not touched. It
records no tax, so it moves the Office Expenses line by 15.50 and the tax lines by nothing; the
coordinator's caveat and this report's figures are the same statement.

**(i) Historical Adjustment — one deliberate divergence, stated in the report's own body.** See §3.

**Layout, not figures.** Two rows are shaped differently without any amount differing:

- The capture prints one aggregated `Fixed Assets` row of (2,728.29). This report prints the
  accounts it is made of — `Office Equipment (710)` 923.79 + `Computer Equipment (720)` 1,804.50 =
  (2,728.29). This is a pre-existing trait of the renderer (it names the account a row belongs to,
  the way the rest of this report and Xero's own Trial Balance do) and not something this change
  introduced. The Xero capture itself carries no code column, so the report cannot print Xero's
  label without inventing a grouping the ledger does not have.
- The capture leaves `Opening Balance` blank; this report prints `0.00`. The organisation's bank
  accounts open the window at zero because no bank journal is dated before 1 January 2026.

---

## 3. The 840 decision

`docs/cash-summary-xero-rule.md` §5 recommends rule A4: a named-account list, containing 840 and
nothing else, that pulls that account's rows out of Less Expenses and into Plus Other Cash
Movements. **A4 was deliberately not implemented.** The report sections every account by its own
role and hard-codes no account:

```go
func cashSectionFor(accountType string) string {
	if IsIncomeAccountType(accountType) {
		return CashSectionIncome
	}
	if containsType(costAccountTypes, accountType) {
		return CashSectionExpense
	}
	return CashSectionOther
}
```

The two Xero readings, both cited in the report's own title text:

- `docs/xero-reference/cash-summary.txt` line 20 — Xero's Cash Summary prints `Historical
  Adjustment (4,130.98)` **inside Less Expenses**.
- `docs/xero-reference/balance-sheet.txt` line 25 — Xero's own Balance Sheet files 840 under
  **Current Liabilities**.

This report follows the account's own class. 840 is typed `CURRLIAB` in this chart, Xero's Balance
Sheet files it there, and Xero's Trial Balance prints it as a Current Liability, so this report
prints it in Plus Other Cash Movements. The report says this in its own body, where a reader of the
figure will see it, and names both citations.

**The bottom line does not change.** Moving a line between two sections that both sit below the
Surplus row cannot move the movement or the balance. Shown both ways, on the capture's own figures
(contamination excluded):

*Layout A — Xero's, 840 inside Less Expenses*

```
Sales                                       21,054.73
Less Expenses: 15,397.75 − 4,130.98       = −11,266.77
Surplus (Deficit)                         =   9,787.96
Plus Other Cash Movements                     −2,728.29
Plus Tax Movements                               370.55
Net Cash Movement                         =   7,430.22
```

*Layout B — this report's, 840 in Plus Other Cash Movements*

```
Sales                                       21,054.73
Less Expenses: 15,397.75                  = −15,397.75
Surplus (Deficit)                         =   5,656.98
Plus Other Cash Movements: −2,728.29 + 4,130.98 = +1,402.69
Plus Tax Movements                               370.55
Net Cash Movement                         =   7,430.22
```

Identical: 7,430.22. (At the contaminated actual figures the same two layouts give 5,352.48 +
1,402.69 + 370.55 = 7,125.72, which is what the report prints.)

**The A4 alternative, and what it would cost.** Adding 840 to a section-exception list is a
one-line change in `cashSectionFor`. It would buy exact section-level capture parity on this
organisation's data and nothing else, and it would cost three things:

1. Every other balance-sheet account would still be sectioned by role, so the report would hold two
   rules for the same question — its own class, except for one account — with no principle that
   says why 840 and not, say, 820 Sales Tax — which this report already keeps out of the sections
   for a different reason (it is the second posting of tax the report has already measured), but
   which the capture does not print in Less Expenses either.
2. The exception would not generalise. Another organisation whose Historical Adjustment account has
   a different code, or which files it under a different class, would silently not get it.
3. The report's title text would have to stop saying "sectioned by its own account class", which is
   the sentence that currently makes the divergence checkable by a reader.

The judgement is that a stated, principled divergence is worth more than an unstated one-line
exception. If exact section-level parity is wanted later, it is `cashSectionFor` plus that one
account code, and the arithmetic in §3 shows the bottom line is unaffected either way.

---

## 4. The rendered report, before and after

Both blocks below are the report's own content as returned by
`GET /api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-12-31` — the report titles, then
every row with its 2026 cell and its three empty comparative cells.

The "before" is the previous classification (no look-through, no coverage row, empty tax rows)
rendered against the same database two seconds earlier, so the pair differs in the change and
nothing else. The before block carries the *new* caveat text, because the caveat lives in the
renderer and only the classification and the tax rows were reverted for this capture; the caveat's
own before and after are quoted in §1.3.

### Before — fetched 2026-09-11 22:15:24 +02:00

```
Cash Summary
Demo Company (Global)
From 1 January 2026 To 31 December 2026
Not represented: the Yearly average (YTD), Variance and Variance for Variance columns (this report is rendered for a single period and the organisation stores no budget to average or to compare against). Historical Adjustment is sectioned by its own account class - it is a current liability, which is where Xero's Balance Sheet files it (docs/xero-reference/balance-sheet.txt line 25) - so it prints in Plus Other Cash Movements where Xero's own capture prints it inside Less Expenses (docs/xero-reference/cash-summary.txt line 20); both are below the Surplus row, so the movement and the balance are the same under either reading. Tax is measured rather than assumed: each attributed line contributes the tax this ledger records beside its net to Plus Tax Movements, and the tax a journal posts a second time as a line on 820 Sales Tax is left out rather than counted twice. Coverage: the whole of the period's cash movement is attributed to the documents the ledger links it to; no part of it is carried on a control account for want of a link.

ReportID: CashSummary   ReportName: Cash Summary   ReportDate: 31 December 2026

  	2026	Yearly average (YTD)	Variance	Variance for Variance
Income
  Sales (200)	-24.00			
  Total Income	-24.00			
Less Expenses
  Advertising (400)	250.00			
  Bank Fees (404)	45.00			
  Entertainment (420)	53.60			
  General Expenses (429)	46.19			
  Motor Vehicle Expenses (449)	274.36			
  Office Expenses (453)	184.37			
  Printing & Stationery (461)	67.16			
  Repairs and Maintenance (473)	64.20			
  Telephone & Internet (489)	101.62			
  Travel - National (493)	177.44			
  Total Expenses	1263.94			
  Surplus (Deficit)	-1287.94			
Plus Other Cash Movements
  Accounts Receivable (610)	22792.47			
  Accounts Payable (800)	-18371.23			
  Unpaid Expense Claims (801)	-64.40			
  Sales Tax (820)	-74.16			
  Historical Adjustment (840)	4130.98			
  Total Other Cash Movements	8413.66			
Plus Tax Movements
  Tax Collected				
  Tax Paid				
  Net Tax Movements				
  Net Cash Movement	7125.72			
Summary
  Opening Balance	0.00			
  Plus Net Cash Movement	7125.72			
  Cash Balance	7125.72			
```

The before report reads the control accounts themselves: the movement of 610, 800, 801 and 820 is
reported *as* the movement, the income and expense sections hold only the few lines that happened to
be coded directly, and the tax section is three empty cells. It foots to 7,125.72 — the same figure
as the after report. That is exactly the point of the change: the old report was not wrong about how
much cash moved, it was wrong about where it went.

### After — fetched 2026-09-11 22:15:26 +02:00

```
Cash Summary
Demo Company (Global)
From 1 January 2026 To 31 December 2026
Not represented: the Yearly average (YTD), Variance and Variance for Variance columns (this report is rendered for a single period and the organisation stores no budget to average or to compare against). Historical Adjustment is sectioned by its own account class - it is a current liability, which is where Xero's Balance Sheet files it (docs/xero-reference/balance-sheet.txt line 25) - so it prints in Plus Other Cash Movements where Xero's own capture prints it inside Less Expenses (docs/xero-reference/cash-summary.txt line 20); both are below the Surplus row, so the movement and the balance are the same under either reading. Tax is measured rather than assumed: each attributed line contributes the tax this ledger records beside its net to Plus Tax Movements, and the tax a journal posts a second time as a line on 820 Sales Tax is left out rather than counted twice. Coverage: the whole of the period's cash movement is attributed to the documents the ledger links it to; no part of it is carried on a control account for want of a link.

ReportID: CashSummary   ReportName: Cash Summary   ReportDate: 31 December 2026

  	2026	Yearly average (YTD)	Variance	Variance for Variance
Income
  Sales (200)	21030.73			
  Total Income	21030.73			
Less Expenses
  Advertising (400)	5750.00			
  Bank Fees (404)	45.00			
  Cleaning (408)	1110.00			
  Consulting & Accounting (412)	58.00			
  Entertainment (420)	1553.60			
  General Expenses (429)	46.19			
  Light, Power, Heating (445)	235.50			
  Motor Vehicle Expenses (449)	654.36			
  Office Expenses (453)	715.87			
  Printing & Stationery (461)	94.41			
  Rent (469)	3273.66			
  Repairs and Maintenance (473)	1745.61			
  Telephone & Internet (489)	186.37			
  Travel - National (493)	209.68			
  Total Expenses	15678.25			
  Surplus (Deficit)	5352.48			
Plus Other Cash Movements
  Office Equipment (710)	-923.79			
  Computer Equipment (720)	-1804.50			
  Historical Adjustment (840)	4130.98			
  Total Other Cash Movements	1402.69			
Plus Tax Movements
  Tax Collected	1844.56			
  Tax Paid	-1474.01			
  Net Tax Movements	370.55			
  Net Cash Movement	7125.72			
Summary
  Opening Balance	0.00			
  Plus Net Cash Movement	7125.72			
  Cash Balance	7125.72			
```

The after report reads the documents the cash settled. Nothing is left on a control account,
because every cash movement in this window has a document link and the migration repaired the ones
that did not. The two figures that were empty are now measured, and neither the movement nor the
balance moved: 7,125.72 both before and after.

---

## 5. The coverage statement

The report states, in its own titles, how much of the period's cash movement it could not attribute
to a document. Two cases:

- **Nothing unattributed** (this organisation, this window):

  > Coverage: the whole of the period's cash movement is attributed to the documents the ledger
  > links it to; no part of it is carried on a control account for want of a link.

  The unattributed amount here is **0.00**. The residual query returned no row for any control in
  this window, so no row carries the `(not attributed to a document)` mark.

- **Something unattributed** (the case a caller must be able to see, exercised by
  `TestHTTP_Reports_CashSummaryStatesWhatItCannotAttribute` with 120.00 on 610):

  > Coverage: 120.00 of the period's cash movement is not attributed to a document - the ledger
  > links none to it - and is printed on the control account the journal itself named, in a row
  > marked (not attributed to a document).

The mechanism is exact by construction rather than by estimate: `residual(control) =
journal_cash(control) − attributed(control)`, where `journal_cash` sums the `BANK` lines of the
journals that use that control and `attributed` sums `amount + tax` of the rows tagged with it. The
two are computed from the same rows, so the residual is whatever is left and nothing is guessed.
The row is printed on the control account the journal named, never spread over the accounts a
document might have coded it to.

---

## 6. Tests, and the proof they fail without the fix

### The six tests

New file `internal/handlers/cash_summary_classification_integration_test.go`, window
`2026-02-01 … 2026-02-28`:

1. `TestHTTP_Reports_CashSummaryLooksThroughReceivablesToTheInvoice` — an ACCREC invoice with lines
   on 200 (100.00) and 260 (50.00), paid 150.00 through 090. Asserts Income is
   `["Sales (200)", "Other Revenue (260)"]`, that no `Accounts Receivable (610)` row appears
   anywhere, that Plus Other Cash Movements is empty, and that Total Income is 150.00 and the
   movement is the bank accounts' own movement.
2. `TestHTTP_Reports_CashSummaryLooksThroughPayablesToTheBill` — an ACCPAY bill coded to 400, paid
   80.00. Asserts Advertising (400) 80.00 in Less Expenses, no `Accounts Payable (800)` row, Total
   Expenses 80.00, movement −80.00.
3. `TestHTTP_Reports_CashSummaryStatesWhatItCannotAttribute` — a manual journal
   `090 +120.00 / 610 −120.00`. Asserts the row
   `Accounts Receivable (610) (not attributed to a document)` carrying 120.00 in Plus Other Cash
   Movements, that the titles contain `Coverage:` and `120.00`, that the report foots, and that
   Cash Balance = Opening + Net.
4. `TestHTTP_Reports_CashSummaryMeasuresTaxWithoutCountingItTwice` — a journal
   `453 50.00 (INPUT, tax 5.00) / 820 5.00 / 090 −55.00`. Asserts Office Expenses 50.00 in Less
   Expenses, Plus Other Cash Movements empty (the 820 line is not a movement of its own), Tax
   Collected 0.00, Tax Paid −5.00, and the movement −55.00 rather than −50.00 — the tax counted
   once, from the line that carries it, not twice from the 820 line.
5. `TestHTTP_Reports_CashSummaryFootsAgainstTheBankAccounts` — a receipt of 100.00, a supplier
   payment of 40.00, a taxed direct spend (453 50.00 + tax 5.00 + 820 5.00 + 090 −55.00) and an
   unlinked 610 receipt of 30.00. Asserts the movement equals the printed rows *and* the bank
   accounts' own movement (35.00), Cash Balance = Opening + Net, Income 100.00, Expenses 90.00,
   Other 30.00, Net Tax −5.00, and titles containing `Coverage: 30.00`.

Changed: `internal/handlers/reports_backend_fixes_integration_test.go:208`
`TestHTTP_Reports_CashSummaryIsItsOwnReport` — §7.4 of the rule document predicted exactly this
test, and the prediction held. Three assertions moved (the tax cells now assert `0.00` rather than
emptiness; titles now check `Tax Movements` and `Coverage:`; the footing is now
`Surplus + Total Other Cash Movements + Net Tax Movements`). The `rowLabels` expectation did not
need to move.

### The proof

Each test was run against the code with its specific fix reverted, then against the fixed code. All
three reverts are the real thing, not a stub: `lookthrough` removes `doc_attr`/`cn_attr`/
`claim_attr` and the residual from the query (the old classification, keeping direct lines only);
`residual` removes only the residual/coverage CTE and the `Unattributed` flag; `tax` reverts only
the renderer's tax rows to `nil`.

| revert | failing tests | first assertion to fail |
| --- | --- | --- |
| look-through removed | 1, 2, 3, 4, 5 (5 of 5 new) | `no report row for account 200`; `no report row for account 400`; `a movement the ledger links no document to must say so in its own row`; `the coverage statement must carry the unattributed amount`; `the tax account is where the tax was posted, not where it was spent`; `the movement is the bank accounts' own movement: got -60.00, want -55.00`; `net cash movement == the bank accounts' own movement: got 30.00, want 35.00`; income `0.00`, expenses `50.00`, other `85.00` |
| residual/coverage removed | 3, 5 | `trial balance has no row for account 610`; `the coverage statement must carry the unattributed amount`; `net cash movement == the bank accounts' own movement: got 5.00, want 35.00`; other cash movements |
| tax rows reverted to empty | 4, 5, and the existing test | `tax paid is the tax once, not twice`; `net tax movements`; `the movement is the bank accounts' own movement: got -50.00, want -55.00`; `Net Tax Movements must be a measured figure` |

Read as a whole: the look-through revert fails all five new tests, so none of them passes for a
reason unrelated to the classification; the residual revert fails exactly the two tests that assert
the coverage statement, so those two test the coverage mechanism and nothing else; the tax revert
fails exactly the tax tests plus the existing report test, which is why §7.4 said that test had to
move.

After restoring the fixed files the six tests pass:

```
ok  	github.com/shurco/goxero/internal/handlers	1.032s
```

---

## 7. Things found that the spec did not anticipate

1. **`invoice_line_items.account_id` is not always populated.** The rule document's §7.2 joins a
   line to its account by `account_id`. `internal/repository/invoice.go:212` inserts only
   `account_code` when an invoice is created through the API, so `account_id` is NULL for every
   line this application itself writes; the demo organisation's imported rows do have it. The join
   is therefore by code —
   `COALESCE(li.account_id, ac.account_id)` with `LEFT JOIN accounts ac ON ac.organisation_id = $1
   AND ac.code = li.account_code` — because a line's coding *is* its account code and the id beside
   it is a denormalisation. Without this, four of the five new tests failed with
   `no report row for account 200`.
2. **The 0.08 the rule document could not place is a rounding-order artefact, and per-line rounding
   removes it.** §8 item 2 left Tax Collected 8 cents out at 1,844.64. Rounding each line's tax to
   the cent *before* summing — `ROUND(li.tax_amount * frac, 2)` per line rather than summing at
   document level and rounding once — gives 1,737.74 instead of 1,737.82, and 1,737.74 + 106.82 =
   **1,844.56**, Xero's own figure. The same applies to the amounts, which is why every other line
   in §2 matches to the cent rather than to a cent or two. This is a measurement, not a
   reconciliation: nothing was adjusted to make a line agree.
3. **The expense-claim path needed an explicit rule.** `Travel - National (493)` only reaches
   209.68 if the `EXPENSECLAIM` journal is read for its coded lines. It is matched by the `801`
   line carrying the opposite net amount to the one the bank journal posted, which is the only link
   the ledger holds — an expense claim has no `payments` row to join through.
4. **The credit-note tax convention had to be chosen, and only one choice reproduces the capture.**
   Restricting `CreditNoteTax` to the notes whose document took cash inside the window gives
   Tax Collected 1,762.06 / Tax Paid (1,391.51) — 82.50 and 82.50 away from the capture. Taking all
   of the organisation's credit-note tax (106.82) reproduces 1,844.56 / (1,474.01) exactly. The
   amount cancels in Net Tax Movements either way, so the bottom line never depended on it; the
   report's two tax lines did.
5. **Two layout traits differ from the capture without any figure differing** — `Fixed Assets` (one
   aggregated Xero row, two named account rows here) and `Opening Balance` (blank in the capture,
   `0.00` here, because the bank accounts really do open the window at zero). Both pre-date this
   change; neither is a figure.
6. **`Net Cash Movement` prints at top level, after the tax section**, as it does in the capture
   (line 38) — it is not a row inside Plus Tax Movements.
7. **A bank line can name a tax type and record no tax, and one of the contaminated rows does.**
   Journal 1614 (2026-09-10, the other session's) codes 453 with `tax_type = INPUT` and
   `tax_amount = 0.00`. The tax convention here reads the tax the ledger records, so such a line
   contributes nothing to Plus Tax Movements on either side — which is exactly why the tax lines in
   §2 match the capture to the cent while the amounts do not. The row is left alone; it is the
   other session's, and it is stated here rather than corrected.
8. **The test database had leaked itself full.** `bonefish-pgtestdb-1` keeps its data directory on
   a 1.9 GB `tmpfs` (`compose.dev.yml`), and abandoned `pgtestdb` instances — created by test runs
   that were killed before their cleanup ran — had accumulated to 118 databases, filling it to
   99–100% (16.6 MB free). Every test then failed at `CREATE DATABASE` with
   `SQLSTATE 53100 could not extend file … No space left on device`. They were dropped with
   `DROP DATABASE … WITH (FORCE)` (plain `DROP DATABASE` failed silently because the instances kept
   idle connections), taking the directory from 1,507 MB / 139 databases to 141 MB / 14, and the
   tmpfs from 99% to 31% full. This touched no other session's *rows* — only abandoned test
   instances — and the `monitor`/template databases were left alone. Nothing was dropped in
   `bonefish-postgres-1` (the demo database).

---

## 8. What could not be determined

Stated plainly, so it is not mistaken for something that was verified:

1. **Whether Xero's own rule for `Historical Adjustment` is a named-account exception or something
   else.** §3 of the rule document recommends A4 and states it does not explain the capture, only
   reproduces it. This implementation takes the account's own class instead, and states that. Both
   readings foot identically; which one Xero uses internally could not be established from the
   capture alone.
2. **Whether the credit-note tax convention generalises.** The ledger's data reproduces the capture
   under the unrestricted reading and not under the restricted one, but a single organisation
   cannot show whether that is Xero's rule or a property of this import. §8 item 3 of the rule
   document raised the same doubt from the other side.
3. **Whether a payment covering two bills would be handled.** `payments.invoice_id` is one column
   and nothing in this capture is in that state, so the question is untested rather than answered.
4. **The other session's intent for the rows it is writing.** The four contaminated journals are
   reported in §2 with their creation times and effects and were not touched. Their purpose is not
   known. The coordinator has since identified one of them (journal 1614) as a row whose tax the
   ledger does not record; that matches what is measured here, and no attempt was made to
   reconstruct what it was for.

---

## 9. Gates

Run twice: once at 2026-09-11 22:14 +02:00, and again at 22:18 on the finished tree after the
revert-and-restore cycle (the reverted files were restored byte-identical, verified with `cmp`).
Both runs are green; the second is pasted here.

```
$ gofmt -l .
gofmt exit=0        (no files listed)

$ go build ./...
build exit=0

$ go vet ./...
vet exit=0

$ go test ./internal/... -count=1
ok  	github.com/shurco/goxero/internal/bankcoding	0.411s
ok  	github.com/shurco/goxero/internal/bankfeed	0.700s
ok  	github.com/shurco/goxero/internal/bankrules	1.044s
ok  	github.com/shurco/goxero/internal/bankstatement	1.815s
ok  	github.com/shurco/goxero/internal/config	1.332s
ok  	github.com/shurco/goxero/internal/database	2.150s
ok  	github.com/shurco/goxero/internal/handlers	10.162s
ok  	github.com/shurco/goxero/internal/logger	3.181s
ok  	github.com/shurco/goxero/internal/middleware	3.950s
ok  	github.com/shurco/goxero/internal/models	2.500s
ok  	github.com/shurco/goxero/internal/repository	3.619s
?   	github.com/shurco/goxero/internal/router	[no test files]
?   	github.com/shurco/goxero/internal/testutil	[no test files]
test exit=0

$ cd web && bun run check && bun run build
1789157897236 COMPLETED 479 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS
✓ built in 2.04s
```

---

## 10. Files changed

| file | change |
| --- | --- |
| `migrations/00026_repair_cash_summary_document_links.sql` | new |
| `internal/repository/report.go` | `CashSummary` classification rewritten; `CashCategoryRow`/`CashSummaryReport` extended |
| `internal/handlers/report_render.go` | `renderCashSummary` partitions by section and fills the tax rows; `cashSummarySection` marks unattributed rows; `cashSummaryCaveat` rewritten; `cashSummaryCoverage` added |
| `internal/handlers/cash_summary_classification_integration_test.go` | new, five tests |
| `internal/handlers/reports_backend_fixes_integration_test.go` | `TestHTTP_Reports_CashSummaryIsItsOwnReport` updated |

Untouched, as required: `docs/xero-reference/**` (read only); migration 00025; the Sales Tax work
(`SalesTaxByRate`, `SalesTaxRow`, `renderSalesTax`, `taxLedgerGap`, `taxSideGap`, `totalLabel`,
`taxGapClause`, `taxColumnsCaveat`, `TrialBalance`, `SplitSigned`, `trialBalanceMeasure`,
`trialBalanceTitleNote`, the journal feed ordering), including the coordinator's own amendment of
`renderSalesTax`/`totalLabel`/`taxGapClause`/`taxColumnsCaveat` and `SalesTaxRow`'s two tax fields
from `*decimal.Decimal` to `decimal.Decimal` — the coordinator flagged that amendment in message
`msg_8ee65ff7f2ac` (2026-09-11 21:43 +02:00), which arrived while this work was in progress; the
cash-summary ranges cited in §1.3 do not overlap those functions, and the Sales Tax report was
re-checked afterwards and still prints Collected 2,437.80 / Total, matching the coordinator's
figure. Not touched either: every row another session owns, including journal 1614.

Two coordinator notes were taken rather than acted on beyond what the spec already said. The first
is the Sales Tax amendment above. The second is that migration 00025 wrote
`gl_journal_lines.tax_amount` on `INVOICE`/`CREDITNOTE`/`EXPENSECLAIM` document lines while
bank-transaction journals already carried theirs from `bank_transaction_line_items.tax_amount`;
this report reads `tax_amount` off whichever line it is attributing, so it makes no distinction
between the two paths and needed no change for it.
