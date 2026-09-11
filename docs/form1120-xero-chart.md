# The Form 1120 workpaper against the Xero reference chart

`migrations/00023_xero_reference_chart_of_accounts.sql` replaced the demo
organisation's chart with Xero's own: 58 accounts carrying Xero's codes, names
and types.

> **AMENDMENT (2026-09-11 late) — where the placements live now.** This note was
> written when the placements were a table in Go, each row pinned to an account
> **code** (`{Code: "400", Line: "22"}`, `{Code: "820", Line: "L18"}`, fifty
> more). That is gone: they are rows of `report_line_mappings`, created by
> migration 00028 and addressed by **`account_id`**, so a placement belongs to the
> organisation that holds the accounts and an accountant can correct one with an
> `UPDATE` instead of a rebuild. The reasons below were carried across verbatim,
> so nothing considered was lost. Five **type-wide** rules stayed in Go, because a
> type is a fact Xero fixes on the account and a code is not: `BANK` is cash,
> `INVENTORY` is stock, `DIRECTCOSTS` is the cost of goods, `TERMLIAB` is
> borrowings beyond a year, and `SALES` is income from normal business activity.
> `internal/repository/form1120.go` now names no account code and no account name.
>
> The two tables below still read correctly as **the reasoning for this chart's
> placements** — every account, its line, and why — but the column headed "Rule"
> saying "code" now means "a row of `report_line_mappings` for this account",
> not a literal in the source. The unmapped-accounts sentence the workpaper prints
> was pointing readers at the Go file; it now names the table.
>
> This note records what the workpaper does with that chart, why, and how to check
> it. The mapping data is the source of truth; this is the reasoning behind it.

## The chart it is written against

Read from the database, not from memory:

```sh
docker exec bonefish-postgres-1 psql -U goxero -d goxero -P pager=off \
  -c "SELECT code, name, type FROM accounts WHERE organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' ORDER BY code;"
```

Xero's types are coarser than the form. `REVENUE` covers Sales, Other Revenue
and Interest Income alike — three different lines on Page 1 (1a, 10, 5) — and
`EXPENSE` covers every deduction from advertising to wages. So the table keeps a
type-wide rule only where the type settles the line whatever the business does,
and pins the code everywhere else.

Every account in that chart reaches a line. The unnamed expenses are written out
one row at a time rather than swept up by a type-wide `EXPENSE` rule: a type-wide
rule would have to be overridden six times over to keep Advertising, Repairs and
maintenance, Depreciation, Interest, Rents and Wages on the lines that name them,
and it would still swallow whatever expense account a chart adds next. Row by
row, each account's reason is on the record.

### Placed

Read from the database, then mapped:

| Account | Type | 1120 line | Rule |
| --- | --- | --- | --- |
| 090, 091 Business Bank / Savings | BANK | L1 Cash | type-wide |
| 200 Sales | REVENUE | 1a Gross receipts | code |
| 260 Other Revenue | REVENUE | 10 Other income | code |
| 270 Interest Income | REVENUE | 5 Interest | code |
| 300 Purchases, 310 Cost of Goods Sold | DIRECTCOSTS | 2 Cost of goods sold | type-wide |
| 400 Advertising | EXPENSE | 22 Advertising | code |
| 416 Depreciation | EXPENSE | 20 Depreciation | code |
| 437 Interest Expense | EXPENSE | 18 Interest | code |
| 469 Rent | EXPENSE | 16 Rents | code |
| 473 Repairs and Maintenance | EXPENSE | 14 Repairs and maintenance | code |
| 477 Wages and Salaries | EXPENSE | 13 Salaries and wages | code |
| 478 Superannuation | EXPENSE | 23 Pension and profit sharing | code |
| 505 Income Tax Expense | EXPENSE | M2 Federal income tax | code — see below |
| 404, 408, 412, 420, 425, 429, 433, 441, 445, 449, 453, 461, 485, 489, 493, 494, 497, 498, 499 | EXPENSE | 26 Other deductions | code, one row each |
| 610 Accounts Receivable | CURRENT | L2 Trade notes and accounts receivable | code |
| 620 Prepayments | CURRENT | L6 Other current assets | code |
| 630 Inventory | INVENTORY | L3 Inventories | type-wide |
| 710, 720 Office / Computer Equipment | FIXED | L10a Depreciable assets | code |
| 711, 721 Accumulated depreciation | FIXED | L10b Less accumulated depreciation | code |
| 800 Accounts Payable | CURRLIAB | L16 Accounts payable | code |
| 801 Unpaid Expense Claims | CURRLIAB | L18 Other current liabilities | code |
| 820 Sales Tax | CURRLIAB | L18 | code |
| 825 Employee Tax Payable | CURRLIAB | L18 | code |
| 826 Superannuation Payable | CURRLIAB | L18 | code |
| 830 Income Tax Payable | CURRLIAB | L18 | code |
| 835 Revenue Received in Advance | CURRLIAB | L18 | code |
| 840 Historical Adjustment | CURRLIAB | L18 | code — see below |
| 850 Suspense, 860 Rounding, 877 Tracking Transfers | CURRLIAB | L18 | code, one row each |
| 855 Clearing Account | CURRLIAB | L18 | code |
| 880 Owner A Drawings, 881 Owner A Funds Introduced | CURRLIAB | L18 | code, one row each |
| 900 Loan | TERMLIAB | L19 Debt due in 1 year or more | type-wide |
| 960 Retained Earnings | EQUITY | L24 Retained earnings — unappropriated | code |
| 970 Owner A Share Capital | EQUITY | L21 Capital stock | code |

No type-wide rule is given to `REVENUE`: a fourth revenue account added to the
chart has no defensible default (interest and other income are not gross
receipts), so it falls into "Accounts with no 1120 line" until an accountant
decides.

### The accounts Page 1 does not name

Page 1 lists fifteen deductions and then keeps line 26 "Other deductions" for the
rest of them. The chart's unnamed expenses are pinned there by code, one row at a
time, and the line carries a note saying so — the workpaper prints the accounts
themselves under the line, which is the itemised statement the form asks for.

| Account(s) | Why line 26 |
| --- | --- |
| 404 Bank Fees, 412 Consulting & Accounting, 425 Freight & Courier, 429 General Expenses, 433 Insurance, 441 Legal expenses, 485 Subscriptions | Page 1 names no line for any of them. Bank fees are here on their merits, not because they are in doubt as a deduction; the form's own instructions for line 26 cite legal fees as the kind of thing that belongs there. |
| 408 Cleaning, 445 Light Power Heating, 453 Office Expenses, 461 Printing & Stationery, 489 Telephone & Internet | Overheads Page 1 does not name. None of them is Rents or Repairs and maintenance. |
| 449 Motor Vehicle Expenses | Page 1 names no line for running a vehicle, and it is not Repairs and maintenance. Whether the deduction is the standard mileage rate or actual costs is the accountant's election, so the book figure is what is carried. |
| 493 Travel - National, 494 Travel - International | Page 1 names no line for travel. |
| 420 Entertainment | Page 1 names no line for it, and it is not Taxes and licences. The account carries the **full book amount**: meals and entertainment are deductible only up to the limit the form allows, and the disallowed part is an adjustment on Schedule M-1 line 5, not a different line here. |
| 497 Bank Revaluations, 498 Unrealised Currency Gains, 499 Realised Currency Gains | Page 1 names no line for exchange differences. This chart files them under `EXPENSE` whatever their sign, so they sit here; a year that leaves one of them a net gain is income, and income belongs on line 10. |

Accounts whose line the form decides differently from their type:

| Account | Line | Why not the catch-all |
| --- | --- | --- |
| 505 Income Tax Expense | M2 Federal income tax | Not a deduction on the return at all. Schedule M-1 line 2 adds the federal income tax back to book income, which is where this figure belongs. |
| 478 Superannuation | 23 Pension and profit sharing | An employer's contribution to an employee retirement plan, which is what line 23 is for. A plan that is not a qualified plan belongs on line 24 Employee benefit programmes instead. |

## Schedule L and the check row

The captured books open with a conversion balance — DR 090 Bank 8,654.01 / CR
840 Historical Adjustment 8,654.01, dated the last day before the financial
year. 840 is typed `CURRLIAB` by the chart, so the table places it on line L18
"Other current liabilities", the same line the chart's other unnamed current
liabilities go to. Schedule L is the balance sheet *per books*, so an account
goes where the books put it; an accountant who wants the owner's own accounts
(880, 881) against equity can re-classify them in the chart, and the table
follows the chart.

With the chart placed, every balance-sheet account in the seeded books has a line
and the workpaper's own check row reads:

    Difference (line 15 less line 27) = 0.00

That row is the Schedule L's honesty: it is zero only when the chart is
completely placed.

## Accounts with no 1120 line

The section is still rendered by the workpaper and is empty for this chart. It
is kept on purpose: it is the report's own check, it is what catches an account a
future chart adds, and an account that lands there is the workpaper saying the
return is not ready until that account gets a row in the mapping table.

## The integration tests

`internal/handlers/report_form1120_integration_test.go` holds two.

`TestHTTP_Form1120_Workpaper` drives the seeded organisation over a window that
stops at 30 June and pins the figures: 100.00 gross receipts, 100.00 gross
profit, 100.00 total income, 100.00 Other deductions, 100.00 total deductions,
0.00 taxable income, 0.00 net income per books, 0.00 Schedule L check.

Its two events are posted to the seeded chart — a $100 sale on account 200
(Sales; 400, which the shared invoice helper uses, is Advertising in this chart)
and a $100 journal through account 420 (Entertainment), the expense Page 1 has no
line for, which has to arrive on line 26 and ride up into total deductions.

The window stops at 30 June because the captured Xero bank transactions are all
dated August and September 2026: a full-year window reports the capture's figures
(Sales 10,355.68) instead of the test's two events, and the concrete figures
above could not be asserted.

`TestHTTP_Form1120_EverySeededAccountReachesALine` is the check the workpaper
exists to make, read from the outside: over the whole tax year, no account in the
chart reaches no line. It is read over the full year for the opposite reason —
the capture is what puts figures on the accounts Page 1 does not name, so a
window that stopped at 30 June would not see them, and a mapping table that left
them out would pass.

It asserts that the "Accounts with no 1120 line" section still renders, with its
column headings, and that it holds no account rows; that every account the
General Ledger Detail report gives a figure to is on a line; and that no account
is printed twice. The oracle is the General Ledger Detail report rather than the
workpaper's own queries, so the check is independent of the code under test.

## Checking it

```sh
go test ./internal/handlers/ -run TestHTTP_Form1120 -v
go build ./...

# The payload for the captured tax year (the dev server on :8080 runs whatever
# binary it was started with — start a second instance to see a working tree):
SERVER_PORT=8391 go run ./cmd/server &
curl -s -H "Authorization: Bearer $(cat /tmp/gx.token)" \
     -H "Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2" \
     "http://localhost:8391/api.xro/2.0/Reports/Form1120?fromDate=2026-01-01&toDate=2026-12-31"
```

Read on 11 September 2026, over 2026-01-01 – 2026-12-31:

| Line | Before | After |
| --- | --- | --- |
| 26 Other deductions | 0.00 | 4,556.22 |
| 27 Total deductions | 6,273.25 | 10,829.47 |
| 30 Taxable income | 4,082.43 | −473.79 |
| M1 Net income per books | −473.79 | −473.79 |
| LCHECK Difference | 0.00 | 0.00 |

The eight accounts that used to reach no line total 4,556.22 and nothing else
moved: line 27 rose by exactly that, and line 30 fell by exactly that, which is
what says no account was counted twice. Line 30 now agrees with M1, the Profit &
Loss bottom line — the two figures that have to agree once every expense reaches
a deduction.

The demo database is shared with other sessions, so these figures drift as it is
used; advertising was 5,953.75 when this note was first written and 6,203.75 when
it was last read. Read them again rather than trusting the table.
