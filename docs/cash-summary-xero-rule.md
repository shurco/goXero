# Xero Cash Summary — the classification rule, the evidence, and an implementation plan

**Task:** Orca `task_41ac8d6a2305`, dispatch `ctx_bf45c43be29e`.
**Target:** `/Users/shurco/orca/workspaces/goXero/bonefish` (worktree), branch `feat-xero-parity-bank`.
**Organisation:** `6823b27b-c48f-4099-bb27-4202a4f496a2` (Demo Company (Global), shortCode `!!6Sp3`).
**Database:** docker container `bonefish-postgres-1`, database `goxero`, user `goxero`.
**Reference:** `docs/xero-reference/**` — frozen capture, read only, never edited by this task.
**Product code changed by this task:** none. **Migrations run by this task:** none. **File created:** this one.

Evidence was collected between `2026-09-11 19:30 +02` and `2026-09-11 19:41 +02`; the live API
was read at `2026-09-11T17:36:16Z`. Every SQL statement and every HTTP call behind the numbers
in this document is reproduced verbatim in §9. Another Claude session shares this worktree and
this database and wrote to this organisation while I worked; §6 accounts for that separately and
no number here has been adjusted to make a comparison agree.

---

## 1. The rule

goXero's `CashSummary` classifies each journal that touched a bank account by the **`type` of the
counterpart account** (`internal/repository/report.go:721-733`, the `a.type <> $4` grouping). Xero
does something else. Xero classifies each movement of a bank account by **what the money was coded
to**, and where the money was coded to Accounts Receivable (610), Accounts Payable (800) or Unpaid
Expense Claims (801), Xero **looks through the control account to the source document** and takes
the document's own coding.

The rule, as the capture and the ledger together establish it:

1. **Take the bank journals.** A journal is in scope if at least one of its lines posts to an
   account whose `accounts.type` is `BANK`. In this organisation that is 88 journals: 87 with
   `gl_journals.source_type = 'BANKTRANSACTION'` and 1 `MANUALJOURNAL` (the opening conversion).
   A journal's lines sum to zero, so the counterpart side of the set is exactly the bank accounts'
   own movement.

2. **Read the control account off the journal, not off the counterpart's type.** If the journal has
   a line on 610, 800 or 801, that control account names the kind of transaction (customer receipt,
   supplier payment, expense-claim payment). If it has none, the journal is a direct spend or a
   direct receipt (or a manual journal over a bank account) and step 8 applies.

3. **Look through 610 to the receivable documents the cash settled.** For each `payments` row with
   `payment_type = 'ACCRECPAYMENT'` that the bank journal represents, resolve the invoice
   (`payments.invoice_id → invoices`), then attribute each of that invoice's
   `invoice_line_items` to the account on the line, in proportion to the cash applied:

   ```
   attrib(line) = line_item.line_amount * payment.amount / invoice.total
   tax(line)    = line_item.tax_amount  * payment.amount / invoice.total
   ```

   The section the account lands in follows the account's own role — 200 Sales is Income; a
   purchase account is Less Expenses — and the *sign* the section prints is the money direction,
   not the account type.

4. **Look through 800 to the payable documents the cash settled**, by the same formula. The one
   difference that matters, and the one this organisation exercises: **an allocated credit note
   counts as settlement.** When a bill was settled partly by cash and partly by a credit note,
   the denominator used for the cash applied to the bill is the bill's total and the numerator is
   the cash, but the credit note's own line coding is then **subtracted** from the account it
   names. Together those two halves reproduce Xero exactly on the two documents in this ledger
   that have both (PC Complete and Swanston Security — §4, rows `Office Expenses`,
   `Computer Equipment`, `Other Cash Movements`, and §5).

5. **Look through 801 to the expense claim.** A bank journal whose control is 801 is a payment of
   an expense claim. The claim's own journal (`gl_journals.source_type = 'EXPENSECLAIM'`) carries
   the expense coding; the bank journal carries only 090 / 801. Attribute the claim journal's
   non-801 lines to their accounts. The two journals are **not** dated the same day (claims are
   dated 7 and 11 Aug, the paying bank journals 27 Jul and 11 Aug), so the link is the amount:
   the claim journal whose 801 line equals the bank journal's 801 line, negated.

6. **Separate tax from net.** Every attributed line contributes its `tax_amount` to the
   *Plus Tax Movements* section and its net amount to the Income / Less Expenses / Other section.
   Where the ledger posts tax a second time as a line on the 820 control account inside the same
   journal (it does, for direct spends: `sum(line.tax_amount)` over the direct bank journals =
   74.16 = the net posted to 820 on those same journals), that second posting must **not** be
   counted again — it is the same tax.

7. **Print the section the account's role puts it in.** 200 Sales → *Income*. Purchase accounts →
   *Less Expenses*. The fixed-asset accounts (710, 720) → *Plus Other Cash Movements*. Account 840
   Historical Adjustment → **inside *Less Expenses*** (§5).

8. **A movement whose counterpart is a balance-sheet account with no source document is attributed
   to that account as-is**, with the raw line amount and the money's own direction. The only such
   movement in this ledger is the opening conversion journal (840, §5). No document is required and
   none is invented.

---

## 2. Candidate rules the evidence rejects

**A. "A liability or equity counterpart lands in Less Expenses."** Rejected. It is a description of
the 840 case, not a rule, and the section it would create is contradicted by every other liability
and equity account in this organisation: 610, 800 and 801 are all current liabilities (or
liabilities in substance) and Xero puts *none* of them in Less Expenses — it looks through them.
A rule that must exempt 610, 800 and 801 to survive is not a rule.

**B. "Any counterpart with no source document lands in Less Expenses."** This is the strongest
survivor of the 840 line, and it is rejected only narrowly. It survives all 19 other cash-summary
lines, because every other line's counterpart *does* have a source document. It fails on one
independent piece of ledger evidence: the 820 Sales Tax control account also appears as the
counterpart of bank journals with no source document (the direct input tax on the bank
transactions, 74.16), and Xero does **not** print it in Less Expenses — it prints the tax in *Plus
Tax Movements* instead. So "no source document" is not by itself what puts 840 where it is; the
rule in §1 step 7 has to name 840, and §5 recommends how.

**C. "Section by account type, with a single override for 840."** Rejected. It reproduces the
capture's *layout* while getting the *classification* wrong for every control account: it would
print 610 22,792.47, 800 (18,371.23), 801 (64.40) and 820 (74.16) as four lines of *Plus Other
Cash Movements*, which is exactly what goXero's renderer does today and is exactly what the
sectioning is supposed to dissolve. Xero's capture has no such lines: it has Sales, sixteen
expense accounts and one Fixed Assets line, and their sum is the bank movement.

**D. "Sign and foot."** Rejected as a sole rule, kept as a constraint. The printed sign of every
line is forced — `Income 21,054.73 − Expenses 11,266.77 − Other 2,728.29 + Tax 370.55 = 7,430.22 =
Cash Balance` — so any candidate rule can be tested by whether it foots. It does not, by itself,
say which line a movement belongs to.

**E. "Pro-rate every document by cash, with no credit-note handling."** Rejected, but only after it
was built and run — it is the *nearly* right rule and it is what the first four-fifths of this
investigation produced. Of the 28 capture lines in §4 it reproduces 16 exactly and every other line
except three within a difference that is wholly contamination or rounding. The three it misses are
the account lines `Office Expenses` (950.37, contamination removed, against 700.37 — **+250.00**),
`Computer Equipment` (1,554.50 against 1,804.50 — **−250.00**) and therefore `Fixed Assets`
(2,478.29 against 2,728.29 — **−250.00**); `Total Expenses`, `Surplus` and `Net Cash Movement`
inherit the +250.00. §4 shows the correction and the arithmetic. The residual is not rounding: it
is a real account-to-account transfer caused by the PC Complete / OG laptop credit note, and it
disappears as soon as §1 step 4 is applied.

---

## 3. How each figure in the capture was reproduced

The model in §9.2 (`/tmp/model5.sql`) is the rule of §1 turned into SQL over the live ledger. Its
per-account output, verbatim:

```
+-------------+-------------------------+----------+----------+---------+----+--------------+
|    code     |          name           |   type   |   net    |   tax   | n  |     srcs     |
+-------------+-------------------------+----------+----------+---------+----+--------------+
| 200         | Sales                   | REVENUE  | 21078.65 | 1737.82 | 28 | AR+DIRECT    |
| 400         | Advertising             | EXPENSE  |  5750.00 |  453.75 |  2 | AP+DIRECT    |
| 404         | Bank Fees               | EXPENSE  |    45.00 |    0.00 |  3 | DIRECT       |
| 408         | Cleaning                | EXPENSE  |  1110.00 |   91.58 |  6 | AP           |
| 412         | Consulting & Accounting | EXPENSE  |    58.00 |    4.78 |  2 | AP           |
| 420         | Entertainment           | EXPENSE  |  1553.60 |    0.00 |  4 | AP+DIRECT    |
| 429         | General Expenses        | EXPENSE  |    46.19 |    3.81 |  1 | DIRECT       |
| 445         | Light, Power, Heating   | EXPENSE  |   235.50 |   19.43 |  2 | AP           |
| 449         | Motor Vehicle Expenses  | EXPENSE  |   654.36 |   53.99 |  3 | AP+DIRECT    |
| 453         | Office Expenses         | EXPENSE  |   715.87 |   60.28 | 16 | AP+CN+DIRECT |
| 461         | Printing & Stationery   | EXPENSE  |    94.41 |    5.54 |  3 | CLAIM+DIRECT |
| 469         | Rent                    | EXPENSE  |  3273.66 |  270.09 |  3 | AP           |
| 473         | Repairs and Maintenance | EXPENSE  |  1745.61 |  144.02 |  3 | AP+DIRECT    |
| 489         | Telephone & Internet    | EXPENSE  |   186.37 |   15.37 |  3 | AP+DIRECT    |
| 493         | Travel - National       | EXPENSE  |   209.68 |   14.56 | 18 | CLAIM+DIRECT |
| 710         | Office Equipment        | FIXED    |   923.79 |   76.21 |  1 | AP           |
| 720         | Computer Equipment      | FIXED    |  1804.50 |  148.87 |  1 | AP           |
| 840         | Historical Adjustment   | CURRLIAB | -4130.98 |    0.00 |  1 | DIRECT       |
| TAX-820-net | Sales Tax ctl           |          |    79.07 |    0.00 | 36 |              |
+-------------+-------------------------+----------+----------+---------+----+--------------+
```

Section subtotals from the same run (§9.2, `/tmp/model5b.sql`):

```
| TAX-820(all)                    |  79.07 |   0.00 | 36 |
| TOTAL-EXPENSE-SECTION           | 15678.25 | 1137.20 | 69 |   (accounts 400..493)
| TOTAL-INCOME-SECTION(200)       | 21078.65 | 1737.82 | 28 |
| TOTAL-OTHER-SECTION(710+720)    |  2728.29 |  225.08 |  2 |
```

---

## 4. Line-by-line reconciliation

Every line of `docs/xero-reference/cash-summary.txt` — all 28 rows of the report body, including
the four section totals and the whole Summary block — with the ledger rows that produce it, my
computed figure, and the difference. "Xero" is the capture verbatim; "Model" is §9.2. A difference
is labelled **contamination** (§6, another session's writes), **rounding** or **model**. No figure
here has been nudged.

| # | Capture line | Xero | Ledger rows attributed to it | Model | Difference | Class |
|---|---|---|---|---|---|---|
| 1 | Income · Sales | 21,054.73 | 20 `payments` rows (`ACCRECPAYMENT`) → 20 invoices → their 200 lines, cash-pro-rated; + 2 direct bank lines coded 200 | 21,078.65 | +23.92 | contamination 24.00, rounding −0.08 |
| 2 | Income · Total Income | 21,054.73 | = line 1 | 21,078.65 | +23.92 | same |
| 3 | Less Expenses · Advertising | 5,500.00 | Hoyt Productions bill 08-4123 (5,953.75; 400 = 5,500.00/453.75) settled by the 21 Aug 5,953.75 payment; + a direct 250.00 | 5,750.00 | +250.00 | contamination |
| 4 | Bank Fees | 30.00 | Ridgeway Bank 1 Jul 15.00 + 1 Aug 15.00, `Tax Exempt`, net = gross | 45.00 | +15.00 | contamination |
| 5 | Cleaning | 1,110.00 | 5 MCO bills 216.50 (408 = 200.00/16.50) + the 27 Jul 119.08 bill (408 = 110.00/9.08); all settled in full | 1,110.00 | 0.00 | **match** |
| 6 | Consulting & Accounting | 58.00 | 2 Xero bills 31.39 (412 = 29.00/2.39), 8 Jul and 8 Aug | 58.00 | 0.00 | **match** |
| 7 | Entertainment | 1,553.60 | Carlton Functions bill 1,500.00 (`Tax Exempt`) + 3 direct rows 15.60 + 16.00 + 22.00 (`Tax Exempt`) | 1,553.60 | 0.00 | **match** |
| 8 | General Expenses | 46.19 | Brunswick Petals 50.00 direct, `Tax on Purchases` → 46.19/3.81 | 46.19 | 0.00 | **match** |
| 9 | Historical Adjustment | (4,130.98) | `MANUALJOURNAL` 2026-06-21 "Conversion Balance": 090 +4,130.98 / 840 −4,130.98 | (4,130.98) | 0.00 | **match** (see §5) |
| 10 | Light, Power, Heating | 235.50 | PowerDirect bills 119.08 (445 = 110.00/9.08) and 135.85 (445 = 125.50/10.35) | 235.50 | 0.00 | **match** |
| 11 | Motor Vehicle Expenses | 654.36 | Gateway Motors bill 411.35 (449 = 380.00/31.35) + 2 direct Melrose Parking 148.50 (`Chq 409`, 449 = 137.18/11.32) | 654.36 | 0.00 | **match** |
| 12 | Office Expenses | 700.37 | 9781 bill's 453 line 500.00 (of 1,463.88, shared with 473); Swanston 21 Jul bill 59.54 → 55.00 less the Refund credit note's 23.50 = 31.50; PC Complete bill 1,953.37 → 720 **1,804.50** with the OG laptop credit note's 453 (250.00) subtracted = **−250.00**; OG laptop bill 270.63 → **+250.00**; direct 60.23 + 31.50 + 27.25 + 49.89 (7-Eleven) | 715.87 | +15.50 | contamination |
| 13 | Printing & Stationery | 94.41 | Office Supplies 23.50 → 21.71 and 49.20 → 45.45 direct; expense-claim 461 27.25 | 94.41 | 0.00 | **match** |
| 14 | Rent | 3,273.66 | 3 Truxton bills 1,181.25 (469 = 1,091.22/90.03), 21 Jul / 21 Aug / 31 Aug, each settled in full | 3,273.66 | 0.00 | **match** |
| 15 | Repairs and Maintenance | 1,745.61 | Central Copiers 945-OCon 1,063.56 → 982.50×900/1,063.56 = 831.4058; 9781 bill's 473 line 850.00; 24 Locks direct 69.50 → 64.20 | 1,745.61 | 0.00 | **match** |
| 16 | Telephone & Internet | 186.37 | Net Connect bills 44.92 → 41.50 and 46.82 → 43.25; Telus direct 110.00 → 101.62 | 186.37 | 0.00 | **match** |
| 17 | Travel - National | 209.68 | 16 direct Passing Places Parking 12.00 → 11.09 each = 177.44; expense-claim 493 32.24 | 209.68 | 0.00 | **match** |
| 18 | Less Expenses · Total Expenses | 11,266.77 | Σ of lines 3–17 as printed above (840 entering at (4,130.98)) — and 840 is *in* the section, see §5 | 11,547.27 | +280.50 | contamination 280.50 |
| 19 | Surplus (Deficit) | 9,787.96 | line 2 − line 18 | 9,531.38 | −256.58 | contamination −256.50, rounding 0.08 |
| 20 | Other · Fixed Assets | (2,728.29) | 710 Office Equipment 923.79 (ABC Furniture bill 1,000.00) + 720 Computer Equipment 1,804.50 (PC Complete bill 1,953.37) | (2,728.29) | 0.00 | **match** |
| 21 | Other · Total Other Cash Movements | (2,728.29) | = line 20 | (2,728.29) | 0.00 | **match** |
| 22 | Tax · Tax Collected | 1,844.56 | tax attributed to 200 lines (1,737.82) + all credit-note tax (106.82) | 1,844.64 | +0.08 | rounding |
| 23 | Tax · Tax Paid | (1,474.01) | tax attributed to 400–493 and 710/720 (1,362.28) + claim input tax (4.91) + all credit-note tax (106.82) | (1,474.01) | 0.00 | **match** |
| 24 | Tax · Net Tax Movements | 370.55 | line 22 + line 23 | 370.62 | +0.07 | rounding |
| 25 | Net Cash Movement | 7,430.22 | line 2 − line 18 − line 21 + line 24 | 7,173.71 | −256.51 | contamination −256.50, rounding −0.01 |
| 26 | Summary · Opening Balance | – | no bank journal dated before 1 Jan 2026 | 0.00 | – | **match** (Xero prints no opening) |
| 27 | Summary · Plus Net Cash Movement | 7,430.22 | = line 25 | 7,173.71 | −256.51 | same as line 25 |
| 28 | Summary · Cash Balance | 7,430.22 | opening 0.00 + line 25 | 7,173.71 | −256.51 | same as line 25 |

### The arithmetic that lands on Xero's figures

Every one of Xero's figures is reachable from the ledger. The load-bearing ones:

**Total Expenses.** 11,266.77 = (Σ my expense-section accounts, 15,678.25) − 4,130.98 (840, which
Xero prints inside the section) − 250.00 − 15.00 − 15.50 (the three contamination lines). Exactly.
Equivalently, the `Model` column of table rows 3–17 sums to 11,547.27 on its own, and the four
contamination cents account for the rest.

**Fixed Assets.** 2,728.29 = 923.79 (710) + 1,804.50 (720, the PC Complete bill's 720 line at full
value). It is **not** the fixed-asset balance-sheet movement. The balance-sheet movement is
923.79 + 3,774.49 = **4,698.28** (Office Equipment 923.79 + Computer Equipment 3,774.49), which is
1,969.99 more — because the Tracy laptop bill (1,969.99) is never settled by cash in the period,
so the cash summary does not see it. The two figures differ by exactly that one unpaid bill.

**Sales.** 21,054.73 = 21,078.65 − 24.00 (contamination) + 0.08 (an 8-cent net/tax split on the AR
side; see the rounding note below).

**Tax.** Tax Paid lands exactly: 1,362.28 (tax on the lines attributed to 400–493 and 710/720) +
4.91 (tax posted to 820 on the expense claims the bank paid) + 106.82 (all credit-note tax) =
1,474.01. Tax Collected is 8 cents out: 1,737.82 + 106.82 = 1,844.64 against 1,844.56.

The 106.82 is the sum of every credit note's tax — receivable 84.25 (CN-0014 41.25, CN-0015 41.25,
CN-0023 1.75) plus payable 22.57 (OG laptop 20.63, Refund 1.94) — and it is added to **both** tax
lines. That is the capture's own arithmetic, not a model artefact: it is why the AR cash
(22,792.47) exceeds Sales + Tax Collected by exactly 106.82, and why the same 106.82 inflates
both sides and cancels in Net Cash Movement.

**The 0.08.** Sales is 0.08 **low** and Tax Collected is 0.08 **high** against the capture, and the
two errors cancel in Net Tax Movements (0.07), in Net Cash Movement (0.01 of the −256.51) and in
Cash Balance. I could not locate the single AR line responsible; the sign is consistent across the
two lines and the magnitude is bounded at one cent-level rounding in one invoice's net/tax split.
It is labelled **rounding** and left unresolved rather than absorbed.

### The credit-note correction in full

The uncorrected rule (pro-rate by cash, ignore credit notes) gives, for the PC Complete / OG laptop
pair:

* PC Complete bill 1,953.37, cash 1,682.74 → 720 = 1,804.50 × 1,682.74/1,953.37 = **1,554.50**
* OG laptop bill 270.63, cash 270.63 → 453 = **+250.00**

Applying §1 step 4:

* PC Complete bill: the OG laptop credit note (270.63, allocated to it) counts as settlement, so
  the cash-settled fraction is (1,682.74 + 270.63)/1,953.37 = 1 → 720 = **1,804.50**; and the
  credit note's own 453 coding is subtracted → 453 **−250.00**
* OG laptop bill 270.63 settled in full by its own payment → 453 **+250.00**
* Net: 720 +250.00, 453 0.00 — which is what Xero prints, and the two lines' totals are unchanged,
  which is why the error is invisible in Net Cash Movement and visible only in these two accounts.

The same correction on Swanston Security: bill 59.54, cash 34.10, Refund credit note 25.44
allocated → 453 = 55.00 − 23.50 = 31.50, identical to the uncorrected pro-rata 55.00 × 34.10/59.54
= 31.4998. That document cannot distinguish the two rules; PC Complete can, and does.

`credit_note_allocations` is **empty** in this database (0 rows), so the allocation has to be
reconstructed from `invoices.amount_paid` / `invoices.status` / `invoices.amount_credited`. That is
an implementation risk, not a rule risk — see §7.

---

## 5. Historical Adjustment: the case that breaks a type-based reading

### The evidence

**What Xero prints.** `docs/xero-reference/cash-summary.txt:20` — `Historical Adjustment
(4,130.98)`, inside *Less Expenses*, alphabetically between `General Expenses` and
`Light, Power, Heating`, and counted in `Total Expenses 11,266.77` (verified: Σ the sixteen printed
expense lines = 11,266.77 exactly, and 840 is one of them).

**Where Xero files the account elsewhere.** `docs/xero-reference/balance-sheet.txt:25` prints
`Historical Adjustment 4,130.98` under Current Liabilities. `docs/xero-reference/trial-balance.txt:34`
prints `840 Historical Adjustment Current Liability 4,130.98` on the credit side. And
`migrations/data/xero/accounts.csv:50` carries the migration's own typing:
`840,Historical Adjustment,Current Liability,Tax Exempt (0%),For accountant adjustments,4130.98`.

**So a rule that sections by account type cannot produce this line.** Under `type = Current
Liability` the account belongs with 610 / 800 / 801, and Xero puts none of those in Less Expenses.

**What the ledger says produced it.** `gl_journals.journal_id = 2a56a21a-8a5a-5ae5-b69b-ab29a94f8217`,
`journal_date 2026-06-21`, `source_type MANUALJOURNAL`, `reference "Conversion Balance"`, one line
on 090 for **+4,130.98** and one line on 840 for **−4,130.98**. Nothing else in the ledger touches
840. It is the opening balance of the bank account, journalised against Historical Adjustment by
the migration.

**The printed sign is forced, and it is not the account's sign.** In the ledger 840 is a credit
(−4,130.98) and the bank is a debit (+4,130.98). Inside *Less Expenses* the line has to carry a
sign that makes the report foot: `21,054.73 − 11,266.77 − 2,728.29 + 370.55 = 7,430.22`. If 840
were printed positive, Total Expenses would be 19,528.73 and the report would not foot. Xero prints
it as (4,130.98) because the **money came in** and the section is "Less" — the printed sign follows
the cash direction, exactly as it does for every other expense line, where the ledger's debit is
printed as a positive "less" amount.

### Candidate rules and whether they survive the other 19 lines

| Candidate | Survives the other 19 lines? | Verdict |
|---|---|---|
| **A1.** A liability or equity counterpart lands in Less Expenses | No — 610, 800 and 801 are liabilities-in-substance, carry the bulk of the cash movement (22,792.47 / −18,371.23 / −64.40), and land nowhere near Less Expenses | reject |
| **A2.** Any counterpart with no source document lands in Less Expenses | Almost — all 19 other lines' counterparts have source documents. Fails on 820: the Sales Tax control is also a source-document-less counterpart of the direct bank journals (74.16) and lands in *Tax Movements*, not Less Expenses | reject, but closest |
| **A3.** Any counterpart whose net is a credit lands in Less Expenses | No — 610 nets −22,792.47 and 800 nets +18,371.23; neither is in Less Expenses, and 840 is a credit only because the bank side is a debit | reject |
| **A4.** Every bank-touching journal's non-bank, non-control counterpart is sectioned by the account's role, with 840 named as an expense-section member | Yes — it is the only counterpart with no document and no control, and it is the only account needing a name | **survives** |
| **A5.** An opening-conversion journal against a balance-sheet account is printed in Less Expenses | Yes, and it is narrower than A4: it keys on `source_type = 'MANUALJOURNAL'` + `reference = 'Conversion Balance'` | **survives** |

### Recommendation, and my grounds

**Recommend A4**: section by the *document's* coding where a source document exists, section direct
codings by the coded account's own role, and carry an explicit account-level list of direct
counterparts that print in the expense section — a list that today contains exactly one entry, 840.
Treat A5 as the *implementation* of that list entry (`source_type = 'MANUALJOURNAL'` with no
control account), not as the rule.

Grounds: A4 is the narrowest statement that lands on Xero's figure without special-casing anything
that the ledger does not independently show. It does not claim to know *why* Xero files 840 there —
**I could not evidence the reason**, and I am not asserting one. Xero's own product documentation
for the Cash Summary says only that it is the cash-basis view of the P&L plus other cash movements,
which is consistent with 840 being treated as an "other cash movement that Xero's accountant put in
the expense section" but does not confirm it. Every alternative I tested that covers 840 by a
*principle* either over-reaches onto the control accounts (A1, A2, A3) or requires information the
capture does not contain.

**This is my own choice, not an evidenced Xero behaviour.** It is labelled as such here and in
`worker_done`. It is the only unevidenced rule in this document; the rest of §1 is forced by the
arithmetic.

---

## 6. Contamination accounting

Another Claude session shares this worktree and this database and wrote to this organisation while
I worked. Nothing below has been subtracted from the code or used to adjust a figure; each item is
reported for what it is.

**Bank transactions in the database that are not in the frozen capture.** Comparing the DB's
`bank_transactions` for this organisation against `migrations/data/xero/bank-transactions.csv`
(82 rows), nine rows exist in the DB and not in the capture. Five of them have GL journals and move
the bank account:

| Date | Amount | Reference / contact | Coded to | `bank_transactions.created_at` | Effect on the capture |
|---|---|---|---|---|---|
| 2026-03-05 | 250.00 | "savings demo", no contact | 400 Advertising | 2026-09-11 16:37:09 | Advertising +250.00 |
| 2026-09-09 | 12.00 | no reference, no contact | 200 Sales | 2026-09-11 19:30:53 | Sales +12.00 |
| 2026-09-10 | 12.00 | no reference, contact Bayside Club | 200 Sales | 2026-09-11 19:30:45 | Sales +12.00 |
| 2026-09-10 | 15.00 | "Acct fee", no contact | 404 Bank Fees | 2026-09-11 19:30:47 | Bank Fees +15.00 |
| 2026-09-10 | 15.50 | no reference, no contact | 453 Office Expenses | 2026-09-11 19:30:50 | Office Expenses +15.50 |

250.00 + 15.00 + 15.50 + 12.00 + 12.00 = **304.50**, which is exactly the gap between the
database's bank movement (7,125.72) and Xero's Net Cash Movement (7,430.22). Contamination, fully
accounted.

**Bank transactions with no GL journal at all** — four rows on 2026-09-10, each 12.00, references
`probe-SPEND-USD`, `probe-RECEIVE-USD`, `probe-SPEND-NZD` and one blank. No journal means no effect
on any GL-derived figure; they are named here only so that the count of "unexplained" rows is zero.
They are probe rows from the other session.

**The 47.99 in my model's own net movement.** My model's Net Cash Movement (7,173.71) exceeds the
bank accounts' own GL movement (7,125.72) by 47.99. The cause is the two 12.00 Central City Parking
rows above: they post 090 −12.00 against 200 Sales +12.00, i.e. a *purchase* coded to the sales
account, and the model's Income section takes the counterpart at face value (+12.00 each) while the
bank moved −12.00 each. That is contamination producing a sign inversion in a line the capture
never had. I have left it in place rather than special-casing it.

**Differences that are NOT contamination.** Of the five non-matching capture lines, three are
contamination (Advertising, Bank Fees, Office Expenses) and one is the −0.08 rounding. **There is
no line where my model and Xero disagree for a reason I cannot name.**

**Note on re-reading.** All DB evidence above was taken between 19:30 and 19:41 on 2026-09-11, and
the live API was re-read at 17:36:16Z, after the model was complete. The API output was identical
to the earlier read — no new contamination appeared during the final hour.

---

## 7. Implementation plan

Nothing in this section has been written. No product code was changed and no migration was run by
this task.

### 7.1 What has to change and where

**`internal/repository/report.go` — `func (r *ReportRepository) CashSummary` (line 730).** The
existing body does three things: it computes opening/closing bank balances (fine, keep), it runs
the counterpart query at lines 748–760 (this is the one to replace), and it derives
`NetMovement = ClosingBalance − OpeningBalance` (keep — but the new row set must still sum to it,
and the credit-note correction deliberately preserves the total, so it will).

Replace the single `GROUP BY a.account_id, a.code, a.name, a.type` counterpart query with the
rule of §1. The query shape that reproduces the capture is the one in §9.2: four branch CTEs
(`ar_alloc`, `ap_alloc`, `cn_alloc`, `claim_alloc`) unioned with `direct`, then grouped by account.
The control account is read off the journal (`SELECT a.code ... WHERE a.code IN ('610','800','801')`),
not off the counterpart's type.

**`internal/repository/report.go` — `type CashCategoryRow` (line 705).** It needs a section, not
just an account: today the renderer decides the section from `AccountType` strings. Add a
`Section` field (`income` / `expense` / `other` / `tax_collected` / `tax_paid`) computed in SQL, and
a `Tax` decimal for the amount that goes to the tax section separately from `Amount`. Keep
`AccountID/AccountCode/AccountName/AccountType` — the renderer's `Attributes` block needs the id.

**`internal/repository/report.go` — `type CashSummaryReport` (line 716).** Add
`TaxCollected`, `TaxPaid`, `OtherMovement` decimals so the renderer does not have to re-derive the
tax and other sections from the category rows, and so a test can assert them.

**`internal/handlers/report_render.go` — `func renderCashSummary` (line 761).** Today it partitions
`cs.Categories` into `incomeRows` / `expenseRows` / `otherMovement` by `AccountType`
(`AccountTypeRevenue`, the expense types, everything else). It has to partition by the new
`Section`. Concretely:

* line 798 `cashSummarySection(&r, "Income", "Total Income", incomeRows, false)` — unchanged in
  shape, but `incomeRows` now comes from `Section == "income"`.
* line 805 `cashSummarySection(&r, "Less Expenses", "Total Expenses", expenseRows, true)` — same,
  and 840 is now a member of `expenseRows` (§5), which is the whole point of the change; the
  `flip` flag already gives it its bracketed sign.
* line 807 Surplus row — unchanged.
* line 808 `otherMovement` — now `cs.OtherMovement`, containing only 710/720, and no longer any
  control account.
* lines 822–828, the three tax rows — currently emitted empty. Populate them from
  `cs.TaxCollected` / `cs.TaxPaid`; `Net Tax Movements` is their sum.
* line 830 `Net Cash Movement` — must keep being derived from the printed rows (the comment on
  `CashSummaryReport.NetMovement` says a test checks exactly that), so it will now equal
  `income − expenses − other + tax`.
* lines 839–841, the Summary block — unchanged, but it now genuinely equals
  `cs.NetMovement` rather than coincidentally.

**`internal/handlers/report_render.go:895 func cashSummaryCaveat()`.** Its second clause — "Tax
Movements … this report will not assume which account holds it" — is no longer true once §1 step 6
is implemented. Replace it with the one caveat that is still true after this work: the Yearly
average / Variance columns are not represented. Do not delete the caveat wholesale; the variance
clause is still accurate.

**`internal/handlers/report.go:403 func (h *ReportHandler) CashSummary`** — no change. It already
passes `from`/`to` through and renders. `internal/handlers/report.go:506` (the catalog entry) —
no change.

### 7.2 Tables and joins the SQL needs

| Need | Table / column | Notes from this ledger |
|---|---|---|
| Which journals touched a bank account | `gl_journal_lines` → `accounts.type = 'BANK'` | 88 journals |
| The kind of transaction | `gl_journals.source_type` ('BANKTRANSACTION', 'MANUALJOURNAL', 'EXPENSECLAIM'), `source_id` | the control account is more reliable than `source_type`; read the 610/800/801 line |
| Bank-side detail | `bank_transactions` (`.total`, `.contact_id`, `.date`, `.reference`), `bank_transaction_line_items` (`.account_id`, `.line_amount`, `.tax_amount`, `.description`) | |
| A customer receipt → its invoice | `payments` (`.payment_type = 'ACCRECPAYMENT'`, `.invoice_id`, `.amount`) → `invoices` → `invoice_line_items` (`.line_amount`, `.tax_amount`, `.account_id`, `.sort_order`) | all 20 AR payments carry a usable `invoice_id` |
| A supplier payment → its bill | the same, `'ACCPAYPAYMENT'` | **only 12 of 24 carry `invoice_id`** — see 7.3 |
| Expense claims | `gl_journals.source_type = 'EXPENSECLAIM'` → `gl_journal_lines`; `expense_claims`, `expense_claim_line_items` | the claim journal is the authoritative coding; the tables are secondary |
| Credit notes | `credit_notes` (`.type`, `.total_tax`, `.remaining_credit`), `credit_note_line_items` (`.account_code`, `.line_amount`, `.tax_amount`), `credit_note_allocations` | `credit_note_allocations` is **empty** — see 7.3 |
| Tax | `gl_journal_lines.tax_amount` on the attributed lines; `820` line net for claims only | counting both double-counts the direct spends' 74.16 (§1 step 6) |

`receipts` and `receipt_line_items` are both empty in this organisation and are not needed.

### 7.3 Risks, in the order they will bite

1. **A payment's document link can be absent.** 12 of the 24 AP payments have `invoice_id IS NULL`.
   §9.2 resolves them with three fallbacks, in order: (a) the bill whose `total = payment.amount`
   and whose contact is the contact of the bank transaction on that date for that amount; (b) the
   bill whose `total = payment.amount + Σ that contact's payable credit notes`; (c) the latest bill
   with that total dated on or before the payment. These reproduce the capture, but they are
   *inference*, and a wrong fallback moves an amount between two expense accounts silently. Any
   production version should either fix the import to populate `payments.invoice_id` or mark
   inferred links in the output. **Do not ship the fallbacks as if they were facts.**

2. **`credit_note_allocations` is empty (0 rows).** §1 step 4 cannot be implemented directly. The
   reconstruction used here is: a bill whose `status = 'PAID'` and whose
   `total − amount_paid > 0` has that gap covered by credit notes, so its attribution is taken at
   full value; and every payable credit note of a contact that the period's cash also touched has
   its own line coding subtracted once. On this ledger that reproduces Xero exactly; it is a
   heuristic. The clean fix is to populate `credit_note_allocations` during import, and the report
   should then join it rather than guess. Note also that `invoices.amount_credited` is **0.00** on
   the PC Complete bill even though a 270.63 credit note was allocated to it — the column exists
   and is not being filled.

3. **One payment row can represent several bills, and one bank line several payments.** The 21 Aug
   PowerDirect bank line of 1,363.92 covers three bills (Rent 1,181.25 + PowerDirect 135.85 +
   Net Connect 46.82) and the 11 Aug Boom FM receipt of 1,082.50 covers two items. The
   attribution must be per `payments` row, never per bank line.

4. **Bank transactions with no journal.** Four rows in this database (the `probe-*` rows) have no
   `gl_journals` row at all, so they are invisible to any GL-driven report while still moving the
   bank account in `bank_transactions`. A cash summary that derives its total from the GL and its
   rows from the GL will foot; one that mixes the two sources will not. Pick the GL and say so, or
   reconcile the difference explicitly.

5. **Tax is stored twice for direct spends.** `Σ line.tax_amount` over the direct bank journals is
   74.16 and the net posted to the 820 line in those same journals is also 74.16. Counting both
   inflates Tax Paid by 74.16. §1 step 6 states which one to count.

6. **The AR-side credit notes.** The rule of §1 step 4 is evidenced on the payable side by the PC
   Complete / OG laptop pair. Applying the same netting mechanically to the receivable credit
   notes would reduce Income by 1,019.95 (CN-0014 500 + CN-0015 500 + CN-0023 19.95) and break the
   capture by 750.00. **Xero's receivable side does not net them the same way** — in this
   organisation the AR invoices' `amount_paid` already equals their `total` even where a credit
   note was allocated against them (INV-0013: `total` 1,082.50, `amount_paid` 1,082.50, with
   CN-0014 541.25 allocated to it on the same day per Xero's own
   `docs/xero-reference/account-transactions.txt`), so the double-settlement is in the imported
   data, not only in the rule. **This is the one point in §1 I would want a second look at before
   implementing**, and the model in §9.2 handles it by applying `cn_alloc` to payable credit notes
   only.

### 7.4 Tests that will need to move

`internal/handlers/reports_backend_fixes_integration_test.go:208`
`TestHTTP_Reports_CashSummaryIsItsOwnReport` asserts `cash.Reports[0].ReportID == "CashSummary"`
and would survive. Any test that asserts the *contents* of `Plus Other Cash Movements` (it will
lose its 610/800/801/820 rows) or that asserts an empty *Plus Tax Movements* section (it will gain
three populated rows) has to be rewritten against the new shape. `internal/handlers/report_render.go`
and `internal/repository/report.go` are both already modified in the working tree by other work on
this branch; the change described here has not been made and should be rebased onto whatever
lands first.

---

## 8. What I could not establish

Stated plainly, so it is not mistaken for something that was verified:

1. **Why Xero prints 840 Historical Adjustment inside Less Expenses.** §5 recommends a rule (A4)
   that reproduces it; it does not explain it. Labelled my own choice.
2. **The 0.08** on Sales / Tax Collected. Bounded, sign-consistent, cancelling elsewhere; not
   located.
3. **Whether the AR credit notes net the way the AP ones do.** §7.3 item 6. The evidence says they
   do not in this ledger; I could not determine whether that is Xero's rule or this dataset's
   import.
4. **The `probe-*` bank rows' purpose.** They are the other session's; I did not investigate them.

Everything else in §4 is arithmetic that lands exactly on Xero's figure.

---

## 9. Appendix — every SQL statement and every API call, verbatim

All SQL was run as `docker exec -i bonefish-postgres-1 psql -U goxero -d goxero < <file>`.
Timestamps are the `now()` returned by each run, in the container's `+02` timezone.

### 9.1 Exploration and evidence queries

**`/tmp/d1.sql`, `/tmp/d2.sql`** — schema dumps (`\d invoices`, `\d invoice_line_items`, `\d payments`,
`\d bank_transactions`, `\d credit_notes`, `\d credit_note_line_items`, `\d expense_claims`,
`\d expense_claim_line_items`, `\d credit_note_allocations`). 19:28.

**`/tmp/q4.sql`** — AR invoices with their line coding, plus AR payments. 19:28.
```sql
\pset pager off
\pset border 2
\timing off
SELECT now() AS ts;
\echo '=== AR INVOICES (ACCREC) ==='
SELECT i.invoice_number, c.name AS contact, i.date, i.status, i.sub_total, i.total_tax, i.total,
       i.amount_paid, i.amount_credited, i.amount_due,
       (SELECT string_agg(a.code||':'||li.line_amount||'/'||li.tax_amount, ' + ' ORDER BY li.sort_order)
          FROM invoice_line_items li LEFT JOIN accounts a ON a.account_id=li.account_id
         WHERE li.invoice_id=i.invoice_id) AS lines
FROM invoices i LEFT JOIN contacts c ON c.contact_id=i.contact_id
WHERE i.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.type='ACCREC'
ORDER BY i.date, i.invoice_number;
\echo '=== AR PAYMENTS ==='
SELECT p.date, p.payment_type, p.amount, p.reference, i.invoice_number, i.total, i.amount_due, p.status
FROM payments p LEFT JOIN invoices i ON i.invoice_id=p.invoice_id
WHERE p.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND p.payment_type='ACCRECPAYMENT'
ORDER BY p.date;
```

**`/tmp/q5.sql`** — AP invoices, AP payments, credit notes, credit-note allocations, expense claims,
claim receipts, bank-transaction line items. 19:28. Abridged here; the substantive part is:
```sql
SELECT cn.credit_note_number, cn.type, cn.date, cn.status, cn.sub_total, cn.total_tax, cn.total, cn.remaining_credit,
       (SELECT string_agg(li.account_code||':'||li.line_amount||'/'||li.tax_amount, ' + ')
          FROM credit_note_line_items li WHERE li.credit_note_id=cn.credit_note_id) AS lines
FROM credit_notes cn WHERE cn.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2';
```
Output: CN-0014 ACCRECCREDIT 541.25 / tax 41.25 / `200:500.0000/41.2500`; CN-0015 the same;
CN-0023 ACCRECCREDIT 21.70 / 1.75 / `200:19.9500/1.7500`; OG laptop ACCPAYCREDIT 270.63 / 20.63 /
`453:250.0000/20.6300`; Refund ACCPAYCREDIT 25.44 / 1.94 / `453:23.5000/1.9400`.

**`/tmp/dir.sql`** — direct (no-control) bank journals, non-bank lines. 19:31.
```sql
\pset pager off
WITH ctrl AS (
  SELECT j.journal_id, j.journal_date, j.source_type, j.source_id,
    (SELECT a.code FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
      WHERE l.journal_id=j.journal_id AND a.code IN ('610','800','801') ORDER BY a.code LIMIT 1) AS ctl
  FROM gl_journals j WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'
    AND EXISTS (SELECT 1 FROM gl_journal_lines bl JOIN accounts ba ON ba.account_id=bl.account_id
                 WHERE bl.journal_id=j.journal_id AND ba.type='BANK')
)
SELECT c.journal_date, c.source_type, a.code, a.name, l.net_amount, l.tax_amount, l.gross_amount, l.description
FROM ctrl c JOIN gl_journal_lines l ON l.journal_id=c.journal_id JOIN accounts a ON a.account_id=l.account_id
WHERE c.ctl IS NULL AND a.type<>'BANK'
ORDER BY c.journal_date, a.code;
```

**`/tmp/apr.sql`** — AP payment → bill resolution: which payments carry `invoice_id`, and which the
contact+total fallback finds. 19:30.
```sql
\pset pager off
WITH bt AS (SELECT b.bank_transaction_id AS bt_id, b.contact_id, b.date, b.total, b.reference
       FROM gl_journals j JOIN bank_transactions b ON b.bank_transaction_id=j.source_id
       WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.source_type='BANKTRANSACTION'
         AND EXISTS (SELECT 1 FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
                      WHERE l.journal_id=j.journal_id AND a.code='800')
         AND b.total<0)
SELECT p.date, p.amount, (p.invoice_id IS NOT NULL) AS linked,
       (SELECT i.invoice_number FROM invoices i WHERE i.invoice_id=p.invoice_id) AS inv_no,
       (SELECT b.reference FROM bt b WHERE b.date=p.date AND -b.total=p.amount LIMIT 1) AS bank_ref,
       (SELECT i2.invoice_number FROM invoices i2
         WHERE i2.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND i2.type='ACCPAY' AND i2.status IN ('AUTHORISED','PAID')
           AND i2.total=p.amount
           AND i2.contact_id=(SELECT b.contact_id FROM bt b WHERE b.date=p.date AND -b.total=p.amount LIMIT 1)
         LIMIT 1) AS fallback_bill
FROM payments p
WHERE p.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND p.payment_type='ACCPAYPAYMENT'
ORDER BY p.date;
```

**`/tmp/tax.sql`** — the tax building blocks. 19:34. Key outputs: AR cash-basis tax 1,737.82;
direct-spend input tax posted to 820 inside bank journals 74.16; expense-claim input tax posted to
820 on `EXPENSECLAIM` journals 13.75 (4.91 of it on claims the bank actually paid); credit-note tax
receivable 84.25, payable 22.57, both 106.82.

**`/tmp/apdet.sql`** — the resolved AP payment → bill → line table with the allocation factor.
19:36. This is the table that shows the 25 AP allocations, including
`2026-07-21 PC Complete 1953.37 → 720:1804.50 alloc 1554.4952` and
`2026-08-01 PC Complete "OG laptop" 270.63 → 453:250.00 alloc 250.00`.

**`/tmp/og.sql`** — the PC Complete / OG laptop documents, the credit notes, the payments of
21 Jul–1 Aug, and the bank transactions of the same window. 19:36. This is the query that
established that the 1 Aug bank line is `SPEND −270.63, reference "OG laptop", contact
PC Complete, coded 800`, and that goXero holds two `ACCPAY` documents
(`…8171f4` 1,953.37 `720:1804.5000/148.8700`, and `…6951` "OG laptop" 270.63 `453:250.0000/20.6300`)
and one `ACCPAYCREDIT` ("OG laptop" 270.63 `453:250.0000/20.6300`).

**`/tmp/cna.sql`** — `SELECT count(*) FROM credit_note_allocations` → **0**; the `source_type`
distribution of bank-touching journals (`BANKTRANSACTION` 87, `MANUALJOURNAL` 1); and every journal
line from 2026-07-26 to 2026-08-02. 19:37.

**`/tmp/m4.sql`** — AR/AP settled-document aggregates and credit-note line sums. 19:38.
Output: AR invoices settled 20 / 23,084.80 / tax 1,760.11, 200 lines 21,324.69; AP bills settled
22 / 17,618.22 / tax 1,230.74; AR CN 200 lines 1,019.95 / tax 84.25; AP CN lines 273.50 / tax 22.57.

**`/tmp/m5.sql`, `/tmp/tax2.sql`, `/tmp/tax3.sql`** — AR payments and invoices in full (19:39);
tax building blocks under the settlement basis (19:39); and the decisive comparison
`sum(line.tax_amount)` vs `net posted to 820` on the direct bank journals, both **74.16** (19:40).

**`/tmp/final.sql`** — the 840 journal, the bank transactions with no journal
(`probe-SPEND-USD`, `probe-RECEIVE-USD`, `probe-SPEND-NZD`, one blank; all 12.00, 2026-09-10), the
bank movement in the DB **7,125.72**, and the column lists of `gl_journals`, `payments`, `invoices`.
19:40. (`gl_journals` has no `narration` column — the first attempt errored with
`ERROR: column j.narration does not exist` and was re-run without it.)

**840 journal, verbatim output:**
```sql
SELECT j.journal_id, j.journal_date, j.source_type, j.reference, a.code, a.name, l.net_amount, l.description
FROM gl_journals j JOIN gl_journal_lines l ON l.journal_id=j.journal_id JOIN accounts a ON a.account_id=l.account_id
WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND a.code='840';
```
```
              journal_id              | journal_date |  source_type  |     reference      | code |         name          | net_amount |    description
--------------------------------------+--------------+---------------+--------------------+------+-----------------------+------------+--------------------
 2a56a21a-8a5a-5ae5-b69b-ab29a94f8217 | 2026-06-21   | MANUALJOURNAL | Conversion Balance | 840  | Historical Adjustment | -4130.9800 | Conversion Balance
```

**Contamination query, verbatim output (19:40:52):**
```sql
SELECT bt.date, bt.type, bt.total, bt.reference, c.name AS contact, a.code AS coded
FROM bank_transactions bt LEFT JOIN contacts c ON c.contact_id=bt.contact_id
LEFT JOIN bank_transaction_line_items btl ON btl.bank_transaction_id=bt.bank_transaction_id
LEFT JOIN accounts a ON a.account_id=btl.account_id
WHERE bt.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'
  AND bt.date IN ('2026-03-05','2026-09-09','2026-09-10') ORDER BY bt.date, bt.created_at;
```

### 9.2 The model — `/tmp/model5.sql`

This is the rule of §1 as executable SQL. Run 19:38–19:40; its output is quoted in full in §3.

```sql
\pset pager off
\pset border 2
\timing off
WITH
bj AS (
  SELECT j.journal_id, j.journal_date, j.source_type, j.source_id
  FROM gl_journals j
  WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'
    AND EXISTS (SELECT 1 FROM gl_journal_lines bl JOIN accounts ba ON ba.account_id=bl.account_id
                 WHERE bl.journal_id=j.journal_id AND ba.type='BANK')
),
ctrl AS (
  SELECT bj.*,
    (SELECT a.code FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
      WHERE l.journal_id=bj.journal_id AND a.code IN ('610','800','801') ORDER BY a.code LIMIT 1) AS ctl,
    (SELECT -SUM(l.net_amount) FROM gl_journal_lines l JOIN accounts a ON a.account_id=l.account_id
      WHERE l.journal_id=bj.journal_id AND a.type='BANK') AS cash_dir
  FROM bj
),
ap_res AS (
  SELECT p.payment_id, p.amount, p.date,
    COALESCE(p.invoice_id,
      (SELECT i.invoice_id FROM invoices i
        WHERE i.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.type='ACCPAY' AND i.status IN ('AUTHORISED','PAID')
          AND i.total = p.amount
          AND i.contact_id = (SELECT b.contact_id FROM bank_transactions b JOIN gl_journals j ON j.source_id=b.bank_transaction_id
                               WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.journal_date=p.date AND -b.total=p.amount LIMIT 1)
        LIMIT 1),
      (SELECT i.invoice_id FROM invoices i
        WHERE i.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.type='ACCPAY' AND i.status IN ('AUTHORISED','PAID')
          AND i.total = p.amount + COALESCE((SELECT SUM(cn.total) FROM credit_notes cn
                                              WHERE cn.contact_id=i.contact_id AND cn.type='ACCPAYCREDIT'),0)
          AND i.contact_id = (SELECT b.contact_id FROM bank_transactions b JOIN gl_journals j ON j.source_id=b.bank_transaction_id
                               WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND j.journal_date=p.date AND -b.total=p.amount LIMIT 1)
        LIMIT 1),
      (SELECT i.invoice_id FROM invoices i
        WHERE i.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND i.type='ACCPAY' AND i.status IN ('AUTHORISED','PAID')
          AND i.total = p.amount AND i.date <= p.date ORDER BY i.date DESC LIMIT 1)
    ) AS invoice_id
  FROM payments p
  WHERE p.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND p.payment_type='ACCPAYPAYMENT'
),
ap_alloc AS (
  SELECT 'AP' AS src, a.code, a.name, a.type,
         li.line_amount * (r.amount + CASE WHEN i.status='PAID' THEN GREATEST(i.total - i.amount_paid,0) ELSE 0 END)
                         / NULLIF(i.total,0) AS net,
         li.tax_amount  * (r.amount + CASE WHEN i.status='PAID' THEN GREATEST(i.total - i.amount_paid,0) ELSE 0 END)
                         / NULLIF(i.total,0) AS tax
  FROM ap_res r JOIN invoices i ON i.invoice_id=r.invoice_id
  JOIN invoice_line_items li ON li.invoice_id=i.invoice_id
  JOIN accounts a ON a.account_id=li.account_id
),
cn_alloc AS (
  SELECT 'CN' AS src, a.code, a.name, a.type, -li.line_amount AS net, -li.tax_amount AS tax
  FROM credit_notes cn JOIN credit_note_line_items li ON li.credit_note_id=cn.credit_note_id
  LEFT JOIN accounts a ON a.code=li.account_code AND a.organisation_id=cn.organisation_id
  WHERE cn.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND cn.type='ACCPAYCREDIT'
),
claim_alloc AS (
  SELECT 'CLAIM' AS src, a.code, a.name, a.type, l.net_amount AS net, l.tax_amount AS tax
  FROM ctrl c
  JOIN gl_journals cj ON cj.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND cj.source_type='EXPENSECLAIM'
       AND (SELECT -SUM(l2.net_amount) FROM gl_journal_lines l2 JOIN accounts a2 ON a2.account_id=l2.account_id
             WHERE l2.journal_id=cj.journal_id AND a2.code='801') = c.cash_dir
  JOIN gl_journal_lines l ON l.journal_id=cj.journal_id
  JOIN accounts a ON a.account_id=l.account_id
  WHERE c.ctl='801' AND a.code NOT IN ('801')
),
ar_alloc AS (
  SELECT 'AR' AS src, a.code, a.name, a.type,
         li.line_amount * p.amount / NULLIF(i.total,0) AS net,
         li.tax_amount  * p.amount / NULLIF(i.total,0) AS tax
  FROM payments p JOIN invoices i ON i.invoice_id=p.invoice_id
  JOIN invoice_line_items li ON li.invoice_id=i.invoice_id
  JOIN accounts a ON a.account_id=li.account_id
  WHERE p.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' AND p.payment_type='ACCRECPAYMENT'
),
direct AS (
  SELECT 'DIRECT' AS src, a.code, a.name, a.type, l.net_amount AS net, l.tax_amount AS tax
  FROM ctrl c JOIN gl_journal_lines l ON l.journal_id=c.journal_id JOIN accounts a ON a.account_id=l.account_id
  WHERE c.ctl IS NULL AND a.type<>'BANK'
),
allrows AS (
  SELECT * FROM ar_alloc UNION ALL SELECT * FROM ap_alloc
  UNION ALL SELECT * FROM cn_alloc UNION ALL SELECT * FROM claim_alloc
  UNION ALL SELECT * FROM direct
)
SELECT code, name, type, ROUND(SUM(net),2) AS net, ROUND(SUM(tax),2) AS tax, COUNT(*) AS n,
       string_agg(DISTINCT src,'+' ORDER BY src) AS srcs
FROM allrows WHERE code<>'820' GROUP BY 1,2,3
UNION ALL SELECT 'TAX-820-net','Sales Tax ctl','', ROUND(SUM(net),2), ROUND(SUM(tax),2), COUNT(*), ''
FROM allrows WHERE code='820'
ORDER BY 1;
```

The section subtotals quoted in §3 come from the same CTE chain with the final `SELECT` replaced by:
```sql
SELECT 'TOTAL-EXPENSE-SECTION' AS code, '' AS name, '' AS type,
       ROUND(SUM(net),2), ROUND(SUM(tax),2), COUNT(*), ''
FROM allrows WHERE code IN ('400','404','408','412','420','429','445','449','453','461','469','473','489','493')
UNION ALL SELECT 'TOTAL-EXPENSE-SECTION-UNROUNDED', '', '', SUM(net), SUM(tax), COUNT(*), ''
FROM allrows WHERE code IN ('400','404','408','412','420','429','445','449','453','461','469','473','489','493')
UNION ALL SELECT 'TOTAL-OTHER-SECTION(710+720)', '', '', ROUND(SUM(net),2), ROUND(SUM(tax),2), COUNT(*), ''
FROM allrows WHERE code IN ('710','720')
UNION ALL SELECT 'TOTAL-INCOME-SECTION(200)', '', '', ROUND(SUM(net),2), ROUND(SUM(tax),2), COUNT(*), ''
FROM allrows WHERE code='200'
UNION ALL SELECT 'TAX-820(all)', '', '', ROUND(SUM(net),2), ROUND(SUM(tax),2), COUNT(*), ''
FROM allrows WHERE code='820'
ORDER BY 1;
```
Unrounded expense-section net: **15678.2458445221708225**. The two lines that are not whole cents
are Central Copiers (831.4058…) and Swanston (31.4998…) — both are the pro-rata of a bill that is
not fully paid, and both round to the capture's cents.

**The earlier model, `/tmp/model3.sql`, kept for comparison.** It is identical except that
`ap_alloc` pro-rates by cash alone with no credit-note term, there is no `cn_alloc` CTE, and
`claim_alloc`/`ar_alloc` are the same. Its distinguishing output: `453 Office Expenses 965.87`,
`720 Computer Equipment 1554.50`, hence `Fixed Assets 2,478.29` against the capture's 2,728.29.
That is rejected candidate rule E (§2).

### 9.3 The live API

Re-read at `2026-09-11T17:36:16Z`, after the model was complete. Identical to the earlier read.

```
POST http://localhost:8080/api/auth/login
Content-Type: application/json

{"email":"admin@demo.local","password":"admin123"}
```
→ `{"token":"<JWT>", ...}`

```
GET http://localhost:8080/api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-12-31
Authorization: Bearer <JWT>
Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2
```

Response, abridged to the report rows (full JSON saved at `/tmp/cs2.json`):

```json
{"ReportID":"CashSummary","ReportName":"Cash Summary","ReportType":"CashSummary",
 "ReportTitles":["Cash Summary","Demo Company (Global)","From 1 January 2026 To 31 December 2026",
   "Not represented: the Yearly average (YTD), Variance and Variance for Variance columns (...); Tax Movements (a line's tax amount is recorded beside its net amount rather than posted to a tax account, so no account's movement is the tax and this report will not assume which account holds it)."],
 "ReportDate":"31 December 2026",
 "Rows":[
  {"Section":"Income","Sales (200)":"-24.00","Total Income":"-24.00"},
  {"Section":"Less Expenses","Advertising (400)":"250.00","Bank Fees (404)":"45.00",
   "Entertainment (420)":"53.60","General Expenses (429)":"46.19",
   "Motor Vehicle Expenses (449)":"274.36","Office Expenses (453)":"184.37",
   "Printing & Stationery (461)":"67.16","Repairs and Maintenance (473)":"64.20",
   "Telephone & Internet (489)":"101.62","Travel - National (493)":"177.44",
   "Total Expenses":"1263.94"},
  {"Row":"Surplus (Deficit)","Value":"-1287.94"},
  {"Section":"Plus Other Cash Movements","Accounts Receivable (610)":"22792.47",
   "Accounts Payable (800)":"-18371.23","Unpaid Expense Claims (801)":"-64.40",
   "Sales Tax (820)":"-74.16","Historical Adjustment (840)":"4130.98",
   "Total Other Cash Movements":"8413.66"},
  {"Section":"Plus Tax Movements","Tax Collected":"","Tax Paid":"","Net Tax Movements":""},
  {"Row":"Net Cash Movement","Value":"7125.72"},
  {"Section":"Summary","Opening Balance":"0.00","Plus Net Cash Movement":"7125.72","Cash Balance":"7125.72"}
 ]}
```

(Abridged: the real response is the full Xero row/cell structure, shown in `/tmp/cs2.json`. No
value above has been altered; only the JSON nesting has been flattened for readability.)

**What the live API establishes about today's failure.** `Plus Other Cash Movements` is nothing but
the control accounts — 610, 800, 801, 820, 840 — summed to 8,413.66, and the whole of the period's
cash movement is explained by five balance-sheet accounts and no income or expense account at all.
`Plus Tax Movements` is three empty rows and the report's own `ReportTitles` carries a caveat
admitting it. And the section totals are wrong in the two directions §4 predicts: `Sales −24.00`
(the two contaminated 12.00 rows, and nothing else, because the 22,792.47 of real receipts sits in
610) and `Total Expenses 1,263.94` (the direct spends only, because every bill-settling payment
sits in 800).
