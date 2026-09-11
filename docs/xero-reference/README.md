# Xero reference dataset — Demo Company (Global), shortCode `!!6Sp3`

Captured 11 September 2026 from the live Xero organisation through the Orca embedded browser.
Every value is read from Xero; nothing is estimated, averaged or filled in.

## Files

| File | Rows | Source page |
|---|---|---|
| `../migrations/data/xero/accounts.csv` | 58 accounts | `https://go.xero.com/GeneralLedger/ChartOfAccounts.aspx` |
| `../migrations/data/xero/bank-accounts.csv` | 2 accounts | `https://go.xero.com/Bank/BankAccounts.aspx` |
| `../migrations/data/xero/bank-transactions.csv` | 48 coded transactions | `https://go.xero.com/Bank/BankTransactions.aspx?accountID=...` ("Account transactions" tab) |
| `../migrations/data/xero/statement-lines.csv` | 101 statement lines | `https://go.xero.com/Bank/Statements.aspx?accountID=...` ("Bank statements" tab) |
| `org-settings.txt` | — | `https://go.xero.com/app/!!6Sp3/settings`, `/Setup/FinancialSettings.aspx`, `/organisation-details` |
| `profit-and-loss.txt` | 26 | `https://reporting.xero.com/!!6Sp3/v1/Run/1016` |
| `balance-sheet.txt` | 25 | `https://reporting.xero.com/!!6Sp3/v1/Run/1017` |
| `trial-balance.txt` | 27 | `https://reporting.xero.com/!!6Sp3/v1/Run/1005` |
| `aged-receivables-summary.txt` | 9 | `https://reporting.xero.com/!!6Sp3/v1/Run/1001` |
| `aged-payables-summary.txt` | 19 | `https://reporting.xero.com/!!6Sp3/v1/Run/1000` |
| `account-transactions.txt` | 101 | `https://reporting.xero.com/!!6Sp3/v1/Run/1009` |
| `bank-summary.txt` | 3 | `https://reporting.xero.com/!!6Sp3/v1/Run/1065` |
| `cash-summary.txt` | 34 | `https://reporting.xero.com/!!6Sp3/v1/Run/1039` |

Report period used: **current financial year, 1 January 2026 – 31 December 2026**
(financial year end is 31 December, so FY2026 is the current year).
* Profit and Loss, Cash Summary, Bank Summary, Account Transactions: date range `This financial year` (1 Jan – 31 Dec 2026).
* Balance Sheet, Trial Balance, Aged Receivables/Payables Summary: "as at" 31 December 2026 (the FY end date).
  Ageing basis left at Xero's default, *Ageing by due date*.
* All reports left in their default layout; no comparison column enabled.

## Method notes

* Chart of accounts scraped from `table#chartOfAccounts`. The page renders each account's
  **name and description on two lines inside one cell**; the CSV splits them on the first newline.
  The page's Export was not used — the table read is complete (58 rows, matching ~58 expected).
* Amounts have thousands separators removed (`4,500.00` → `4500.00`); digits are otherwise untouched.
* `signed_amount` in `bank-transactions.csv` is negative for money out. Direction was verified against
  Xero's own running-balance column: for 49 of 49 consecutive statement rows,
  `balance[n] ± amount[n] == balance[n-1]`.
* Cash effect: 48 coded transactions sum to −1223.79; adding the 2 unreconciled lines
  (−411.35 and −1181.25) equals the full statement movement.
* `account_code` / `account_name_coded` / `tax_rate` for a bank transaction were resolved from the
  transaction's own detail page (`Bank/ViewTransaction.aspx`), following one extra hop to the
  linked bill `AccountsPayable/View.aspx` or invoice (rendered SPA) when the transaction was an
  invoice/bill payment rather than a direct spend/receive money. All 48 rows resolved.
* Cross-checks: P&L Net Profit 8,266.73 = Balance Sheet Total Equity 8,266.73;
  Trial Balance debits = credits = 42,595.46; Bank Summary closing balance 7,430.22
  = Balance Sheet Business Bank Account = `balance_in_xero` in `bank-accounts.csv`.

## What could NOT be extracted, and why

1. **Business Savings Account activity.** Xero reports "No transactions imported" for it, its
   Account transactions tab is empty ("There are no transactions to display") and its Bank statements
   tab shows no statement lines. It therefore contributes **no rows** to `bank-transactions.csv` and
   `statement-lines.csv`. This is real: the account exists (number `121314-121314`) but has no data.
2. **Business Savings Account balances** are left **empty** in `bank-accounts.csv`
   (`balance_in_xero`, `statement_balance`, `unreconciled_count`) because
   `BankAccounts.aspx` shows no Balance-in-Xero / Statement-balance rows for it — only
   "No transactions imported". (Its *Statements* tab separately prints `0.00 Statement Balance`;
   that value was not used, to keep `bank-accounts.csv` faithful to its stated source page.)
3. **The 2 still-unreconciled transactions** (`7 Sep 2026` Payment: Gateway Motors 411.35 and
   `31 Aug 2026` Payment: Truxton Property Management 1,181.25) are **excluded** from
   `bank-transactions.csv` because they are not yet coded. They *are* present in
   `statement-lines.csv` with `status=unreconciled`.
4. **`reference` in `statement-lines.csv` is mapped from the page's "Particulars" column.**
   The Bank statements table's own **"Reference" column was empty for all 101 rows**; "Particulars"
   (51 of 101 rows non-empty, e.g. `M000471`, `INV-0036`, `Acct fee`) carries the same text Xero shows as the
   statement reference elsewhere. The page's `Code` and `Analysis Code` columns were empty throughout
   and were not carried over. This mapping is a naming choice, not invented data.
5. **No running balance is missing**: the Bank statements tab did show a Balance column, so every
   row of `statement-lines.csv` has one. The savings account, which shows no balance column, has no rows.
6. **`description` vs `contact` in `bank-transactions.csv`**: `description` is the text the
   Account transactions grid prints (e.g. `Payment: MCO Cleaning Services`), while `contact` is the
   contact name from the transaction record (`MCO Cleaning Services`). Both are verbatim; they are
   not the same field.
7. **Payment-type transactions with multiple coded lines** are recorded as
   `account_name_coded` / `tax_rate` joined with `"; "` (one row per bank transaction, not per GL line),
   e.g. the 11 Aug 2026 Boom FM receipt codes to `Sales; Sales`.
8. **Not captured** (outside the requested scope): Sales Tax Report, Statement of Cash Flows,
   General Ledger Detail, and the Period/Tracking comparatives on any report.
