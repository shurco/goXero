# Trial Balance reconciliation — Demo Company (Global), FY2026

**Question this file answers:** can the Xero Trial Balance printed for Demo Company (Global)
at 31 December 2026 be reproduced, line for line, from the source documents extracted into
`migrations/data/xero/` — with no invented figures and no unexplained gap?

**Answer: yes, for all 25 accounts.** Every account in `trial-balance.txt` is reproduced
exactly from the extracted CSVs, and the reproduced debits equal the reproduced credits equal
**42,595.46**, which is the total Xero prints. Section 7 lists what could not be reproduced —
nothing, but it states plainly the three things a reader should not misread as evidence.

Source of truth for the comparison: `docs/xero-reference/trial-balance.txt`
(report 1005, "as at" 31 December 2026, re-rendered during this capture and byte-identical to
the previous worker's copy). Supporting evidence added under `docs/xero-reference/`:
`general-ledger-detail.txt` (report 1071, 1 Jan – 31 Dec 2026, 543 ledger lines in 27 account
sections) and `journal-report.txt` (report 1098, 1,280 journal lines).

## 1. Disclosure — what was changed in `migrations/data/xero/`

| File | Change | Why |
|---|---|---|
| `bank-transactions.csv` | **34 rows appended.** The previous worker's 48 rows are untouched and still occupy lines 2-49. | Their 48 rows only cover 11 Aug – 11 Sep 2026 (the statement-import window). Xero's own "Account transactions" grid for Business Bank Account reports **82 total items** spanning 1 Jul – 11 Sep 2026. Without the other 34 the bank-driven accounts (Bank Fees, Entertainment, General Expenses, Motor Vehicle, Office Expenses, Printing & Stationery, Repairs and Maintenance, Telephone & Internet, Travel - National, Sales Tax, and 090/800 themselves) cannot be reproduced. |
| `invoice-lines.csv` | **Regenerated**, 40 rows. | The previous version was missing the second line of INV-0028 (the 10.00 Tax-Exempt "Delivery charge", coded to account **425 Freight & Courier**), which is why Sales read 10.00 high. Regenerated from the invoice API payloads so every line's own `account_code` is present. |
| `bill-lines.csv` | **Regenerated**, 47 rows, with two columns added: `contact` and `bill_date`. | Bill numbers are not unique in this org (six bills share `RENT`, four share `AP`, six share `Rpt`, four have a blank number). Keyed on `bill_number` alone a line could not be attributed to a single bill, which overstated Rent ×5 and Computer Equipment ×2. |
| `expense-claims.csv` | **Rewritten**, 4 rows. | The `tax_rate` column held a tax *amount* and the `tax_amount` column held a GL running balance, and the account was a name rather than a code. Values were re-read from `general-ledger-detail.txt` (accounts 453/461/493) and an `account_code` column added. |
| `payments.csv` | unchanged (44 rows) | Verified complete: every payment in the year is present. |
| `contacts.csv`, `invoices.csv`, `bills.csv`, `credit-notes.csv`, `credit-note-lines.csv`, `manual-journals.csv`, `manual-journal-lines.csv`, `opening-balances.csv`, `fixed-assets.csv` | unchanged | — |
| `accounts.csv`, `bank-accounts.csv`, `statement-lines.csv`, and every pre-existing `docs/xero-reference/*` dump | **not touched** | — |

Two of the 34 appended rows are the transactions Xero's grid shows as `Unreconciled`. The
existing `README.md` section 3 says the 7 Sep 2026 Gateway Motors 411.35 and 31 Aug 2026
Truxton Property Management 1,181.25 rows are left out "because they are not yet coded".
That is **not what the ledger shows**: both are coded Payable Payments (449 Motor Vehicle
Expenses and 469 Rent respectively) and both appear in `general-ledger-detail.txt`. Excluding
them leaves 090 Business Bank Account 1,592.60 light and 800 Accounts Payable 1,592.60 heavy.
They are therefore included, and `statement-lines.csv` still carries them as `unreconciled`, as
the previous worker left it.

No Go, Svelte, TypeScript or SQL file was read or written by this capture.

## 2. How each document posts

The reproduction is a plain double-entry posting run over the CSVs. Every rule below is the rule
Xero itself applied, re-derived from `general-ledger-detail.txt`, not assumed:

| Document | Posting used |
|---|---|
| `invoices.csv` + `invoice-lines.csv`, status not DRAFT/VOIDED/DELETED | Dr 610 Accounts Receivable = total; Cr each line's own `account_code` = `line_amount`; Cr 820 Sales Tax = `tax_total` |
| `bills.csv` + `bill-lines.csv`, status not DRAFT/VOIDED/DELETED | Cr 800 = total; Dr each line's `account_code` = `line_amount`; **Dr** 820 = `tax_total` (input tax reduces the liability, it does not increase it) |
| `credit-notes.csv` + `credit-note-lines.csv`, type ACCRECCREDIT | Cr 610 = total; Dr line `account_code`; Dr 820 = tax |
| … type ACCPAYCREDIT | Dr 800 = total; Cr line `account_code`; Cr 820 = tax |
| `expense-claims.csv` | Dr `account_code` = `line_amount`; Dr 820 = `tax_amount`; Cr 801 Unpaid Expense Claims = `claim_total` **once per claim** (a claim with two lines occupies two rows but posts one liability) |
| `opening-balances.csv` | Posted by `side`; account 630 is skipped because it self-balances to nil (section 6) |
| `bank-transactions.csv` where `description` starts `Payment:` | Cr 090 for money out / Dr 090 for money in, against **800** (out: bill payment), **610** (in: invoice receipt), or **801** when the coded account is 801 (expense-claim payment) |
| `bank-transactions.csv`, anything else (direct spend / receive money) | Dr (Cr if money in) the coded account for the tax-exclusive part, and 820 for the tax, against 090. Tax-exclusive part = `amount / (1 + rate)`, using the rate Xero prints in `tax_rate` |

The `account_code` / `account_name_coded` columns of a bank transaction hold the **underlying
document's** coding (the ABC Furniture payment shows 710 Office Equipment, the bill's account),
which is the previous worker's convention and the reason the posting table above keys off
`description` rather than off `account_code`.

## 3. Trial Balance reproduced from the extracted source documents

One row per account in `trial-balance.txt`. "Reproduced from" names the extracted file and how
many postings of that file land in the account — a posting is a document header, a line, or a
bank leg. "Reproduced total" is the signed account balance the posting run produces: the same
quantity Xero prints in its Debit/Credit "Year to date" column, so a negative reproduced total
is a credit balance and matches the figure Xero prints in the Credit column.

| `090` | Business Bank Account | 7430.22 |  | bank-transactions.csv (bank leg) ×82; opening-balances.csv ×1 | 7430.22 |
| `200` | Sales |  | 29539.18 | credit-note-lines.csv ×3; invoice-lines.csv ×37 | -29539.18 |
| `300` | Purchases | 775.98 |  | bill-lines.csv ×1 | 775.98 |
| `400` | Advertising | 9657.05 |  | bill-lines.csv ×3 | 9657.05 |
| `404` | Bank Fees | 30.00 |  | bank-transactions.csv (coded grid) ×2 | 30.00 |
| `408` | Cleaning | 1110.00 |  | bill-lines.csv ×6 | 1110.00 |
| `412` | Consulting & Accounting | 87.00 |  | bill-lines.csv ×3 | 87.00 |
| `420` | Entertainment | 1553.60 |  | bank-transactions.csv (coded grid) ×3; bill-lines.csv ×1 | 1553.60 |
| `425` | Freight & Courier | 105.50 |  | bill-lines.csv ×1; invoice-lines.csv ×1 | 105.50 |
| `429` | General Expenses | 166.28 |  | bank-transactions.csv (coded grid) ×1; bill-lines.csv ×1 | 166.28 |
| `445` | Light, Power, Heating | 335.82 |  | bill-lines.csv ×3 | 335.82 |
| `449` | Motor Vehicle Expenses | 654.36 |  | bank-transactions.csv (coded grid) ×2; bill-lines.csv ×1 | 654.36 |
| `453` | Office Expenses | 862.48 |  | bank-transactions.csv (coded grid) ×10; bill-lines.csv ×4; credit-note-lines.csv ×2; expense-claims.csv ×1 | 862.48 |
| `461` | Printing & Stationery | 94.41 |  | bank-transactions.csv (coded grid) ×2; expense-claims.csv ×1 | 94.41 |
| `469` | Rent | 3273.66 |  | bill-lines.csv ×3 | 3273.66 |
| `473` | Repairs and Maintenance | 1896.70 |  | bank-transactions.csv (coded grid) ×1; bill-lines.csv ×2 | 1896.70 |
| `489` | Telephone & Internet | 236.37 |  | bank-transactions.csv (coded grid) ×1; bill-lines.csv ×3 | 236.37 |
| `493` | Travel - National | 433.24 |  | bank-transactions.csv (coded grid) ×16; bill-lines.csv ×1; expense-claims.csv ×2 | 433.24 |
| `610` | Accounts Receivable | 9194.51 |  | bank-transactions.csv (Receivable Payment) ×20; credit-notes.csv ×3; invoices.csv ×30 | 9194.51 |
| `710` | Office Equipment | 923.79 |  | bill-lines.csv ×1 | 923.79 |
| `720` | Computer Equipment | 3774.49 |  | bill-lines.csv ×2 | 3774.49 |
| `800` | Accounts Payable |  | 8386.76 | bank-transactions.csv (Payable Payment) ×22; bills.csv ×35; credit-notes.csv ×2 | -8386.76 |
| `801` | Unpaid Expense Claims |  | 115.95 | bank-transactions.csv (Expense Claim Payment) ×2; expense-claims.csv ×3 | -115.95 |
| `820` | Sales Tax |  | 422.59 | bank-transactions.csv (tax) ×38; bills.csv ×35; credit-note-lines.csv ×5; expense-claims.csv ×4; invoices.csv ×30 | -422.59 |
| `840` | Historical Adjustment |  | 4130.98 | opening-balances.csv ×1 | -4130.98 |

## 4. Checks

| Check | Result |
|---|---|
| Sum of reproduced debits | **42,595.46** |
| Sum of reproduced credits | **42,595.46** |
| Xero's own Trial Balance total (`trial-balance.txt`) | **42,595.46 / 42,595.46** |
| Accounts whose reproduced total differs from the figure Xero prints | **0 of 25** |
| Accounts in the Trial Balance the reproduction does not reach | **0** |
| Documents posted | 82 bank rows (all 82 the bank grid shows), 30 posted invoices, 35 posted bills, 5 credit notes, 4 expense-claim rows, 2 opening-balance rows |

## 5. Independent spine — General Ledger Detail (report 1071)

`docs/xero-reference/general-ledger-detail.txt` is a second, independent rendering of the same
year straight off Xero's reporting app. It holds 543 ledger lines in 27 account sections. Summed
by section, debit totals equal credit totals (111,263.08 / 111,263.08) and each section's net
equals the Trial Balance figure to the cent:

| code | account | GL lines | GL debit | GL credit | GL net | Trial Balance figure |
|---|---|---|---|---|---|---|
| `090` | Business Bank Account | 85 | 26923.45 | 19493.23 | 7430.22 | 7,430.22 |
| `200` | Sales | 40 | 1019.95 | 30559.13 | -29539.18 | 29,539.18 |
| `300` | Purchases | 1 | 775.98 | 0.00 | 775.98 | 775.98 |
| `400` | Advertising | 3 | 9657.05 | 0.00 | 9657.05 | 9,657.05 |
| `404` | Bank Fees | 2 | 30.00 | 0.00 | 30.00 | 30.00 |
| `408` | Cleaning | 6 | 1110.00 | 0.00 | 1110.00 | 1,110.00 |
| `412` | Consulting & Accounting | 3 | 87.00 | 0.00 | 87.00 | 87.00 |
| `420` | Entertainment | 4 | 1553.60 | 0.00 | 1553.60 | 1,553.60 |
| `425` | Freight & Courier | 2 | 115.50 | 10.00 | 105.50 | 105.50 |
| `429` | General Expenses | 2 | 166.28 | 0.00 | 166.28 | 166.28 |
| `445` | Light, Power, Heating | 3 | 335.82 | 0.00 | 335.82 | 335.82 |
| `449` | Motor Vehicle Expenses | 3 | 654.36 | 0.00 | 654.36 | 654.36 |
| `453` | Office Expenses | 17 | 1135.98 | 273.50 | 862.48 | 862.48 |
| `461` | Printing & Stationery | 3 | 94.41 | 0.00 | 94.41 | 94.41 |
| `469` | Rent | 3 | 3273.66 | 0.00 | 3273.66 | 3,273.66 |
| `473` | Repairs and Maintenance | 3 | 1896.70 | 0.00 | 1896.70 | 1,896.70 |
| `489` | Telephone & Internet | 4 | 236.37 | 0.00 | 236.37 | 236.37 |
| `493` | Travel - National | 19 | 433.24 | 0.00 | 433.24 | 433.24 |
| `610` | Accounts Receivable | 59 | 34195.38 | 25000.87 | 9194.51 | 9,194.51 |
| `630` | Inventory | 6 | 320.00 | 320.00 | 0.00 | — (nil in GL, absent from Trial Balance) |
| `710` | Office Equipment | 1 | 923.79 | 0.00 | 923.79 | 923.79 |
| `720` | Computer Equipment | 2 | 3774.49 | 0.00 | 3774.49 | 3,774.49 |
| `800` | Accounts Payable | 65 | 18963.37 | 27350.13 | -8386.76 | 8,386.76 |
| `801` | Unpaid Expense Claims | 5 | 64.40 | 180.35 | -115.95 | 115.95 |
| `820` | Sales Tax | 109 | 2122.03 | 2544.62 | -422.59 | 422.59 |
| `840` | Historical Adjustment | 1 | 0.00 | 4130.98 | -4130.98 | 4,130.98 |
| `877` | Tracking Transfers | 10 | 1400.27 | 1400.27 | 0.00 | — (nil in GL, absent from Trial Balance) |

The GL Detail contains two accounts the Trial Balance does not: **630 Inventory**
(320.00 Dr / 320.00 Cr, net 0.00) and **877 Tracking Transfers** (1,400.27 Dr / 1,400.27 Cr,
net 0.00). Both net to nil, which is why they are correctly absent from the Trial Balance, and
they are the reason the GL grand total is 111,263.08 while the Trial Balance total is 42,595.46.

## 6. Journal Report cross-check (report 1098)

`docs/xero-reference/journal-report.txt` — 1,280 journal lines. Posted per account code the lines
net to the same 25 balances (sum of debits = sum of credits = 42,595.46), plus 630 and 877 at
0.00. It also identifies the two journals carrying balances that have no source document:

* **ID 388 "Conversion Balance"** (21 Jun 2026): Dr 090 Business Bank Account 4,130.98 /
  Cr 840 Historical Adjustment 4,130.98 → `opening-balances.csv`, account 840 = 4,130.98.
* **ID 651 "Inventory Opening Balance"** (11 Sep 2026): self-balancing inside 630 Inventory →
  `opening-balances.csv`, and correctly not part of the Trial Balance.

## 7. Trial Balance lines that could NOT be reproduced

**None.** All 25 accounts reconcile to the cent and the debits == credits == 42,595.46 check
holds, so there is no residual and no difference to name. Three things are stated plainly rather
than left to be misread as evidence:

1. **`manual-journals.csv` and `manual-journal-lines.csv` are header-only, and that is correct.**
   `https://go.xero.com/Journal/Search.aspx` reports "There are no manual journals to display."
   The two journals that do carry balances (IDs 388 and 651, section 6) are system journals on
   the Journal Report, and are recorded in `opening-balances.csv`.
2. **The fixed-asset register does not feed the Trial Balance.** `fixed-assets.csv` holds
   FA-0001 to FA-0004, all dated 11 Sept 2026 (two registered, two still draft). None of them has
   a GL entry: the Trial Balance Office Equipment 923.79 and Computer Equipment 3,774.49 come
   from bill lines (ABC Furniture "Coffee table for reception" 923.79; PC Complete
   "Laptop (Oliver)" 1,804.50 + "Laptop (Tracy)" 1,969.99), and there is no depreciation account
   416 anywhere in the Trial Balance. `fixed-assets.csv` should not be expected to sum to 710/720.
3. **22 of the 51 contacts have an empty `type`.** That is Xero's own state, not a read failure:
   those contacts carry no customer/supplier flag on the contact record, and the Contacts list
   grid has no type column at all. `type` was taken from the contacts API's own field and left
   blank where Xero leaves it blank.

## 8. Acceptance evidence

`wc -l migrations/data/xero/*.csv`:

```
migrations/data/xero/accounts.csv              59
migrations/data/xero/bank-accounts.csv         3
migrations/data/xero/bank-transactions.csv     83
migrations/data/xero/bill-lines.csv            48
migrations/data/xero/bills.csv                 47
migrations/data/xero/contacts.csv              52
migrations/data/xero/credit-note-lines.csv     6
migrations/data/xero/credit-notes.csv          6
migrations/data/xero/expense-claims.csv        5
migrations/data/xero/fixed-assets.csv          5
migrations/data/xero/invoice-lines.csv         41
migrations/data/xero/invoices.csv              33
migrations/data/xero/manual-journal-lines.csv  1
migrations/data/xero/manual-journals.csv       1
migrations/data/xero/opening-balances.csv      9
migrations/data/xero/payments.csv              45
migrations/data/xero/statement-lines.csv       102
total                                          546
```

First rows of every file added by this task:

```
# contacts.csv
    name,type,email,phone,town
    24 Locks,,,,
    7-Eleven,,,,
    Abby & Wells,,,,

# invoices.csv
    number,contact,reference,date,due_date,status,currency,subtotal,tax_total,total,amount_paid,amount_due
    INV-0001,Hamilton Smith Ltd,Monthly Support,2026-07-11,2026-07-22,PAID,USD,500.00,41.25,541.25,541.25,0.00
    INV-0002,Young Bros Transport,Monthly Support,2026-07-11,2026-07-22,PAID,USD,500.00,41.25,541.25,541.25,0.00
    INV-0003,Port & Philip Freight,Monthly Support,2026-07-11,2026-07-22,PAID,USD,500.00,41.25,541.25,541.25,0.00

# invoice-lines.csv
    invoice_number,description,quantity,unit_amount,account_code,tax_rate,tax_amount,line_amount
    INV-0001,Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.,1.0000,500.0000,200,Tax on Consulting (8.25%),41.25,500.00
    INV-0002,Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.,1.0000,500.0000,200,Tax on Consulting (8.25%),41.25,500.00
    INV-0003,Desktop/network support via email & phone. Per month fixed fee for minimum 20 hours/month.,1.0000,500.0000,200,Tax on Consulting (8.25%),41.25,500.00

# bills.csv
    number,contact,reference,date,due_date,status,currency,subtotal,tax_total,total,amount_paid,amount_due
    AP,Xero,,2026-07-08,2026-07-08,PAID,USD,29.00,2.39,31.39,31.39,0.00
    RENT,Truxton Property Management,,2026-07-11,2026-07-11,PAID,USD,1091.22,90.03,1181.25,1181.25,0.00
    Rpt,PowerDirect,,2026-07-11,2026-07-21,PAID,USD,110.00,9.08,119.08,119.08,0.00

# bill-lines.csv
    bill_number,contact,bill_date,description,quantity,unit_amount,account_code,tax_rate,tax_amount,line_amount
    AP,Xero,2026-07-08,Monthly subscription,1.0000,29.0000,412,Tax on Purchases (8.25%),2.39,29.00
    RENT,Truxton Property Management,2026-07-11,Monthy rent in advance,1.0000,1091.2240,469,Tax on Purchases (8.25%),90.03,1091.22
    Rpt,PowerDirect,2026-07-11,Monthly power supply,1.0000,110.0000,445,Tax on Purchases (8.25%),9.08,110.00

# credit-notes.csv
    number,contact,type,date,status,total,remaining_credit
    CN-0014,Boom FM,ACCRECCREDIT,2026-07-30,PAID,541.25,0.00
    CN-0015,Hamilton Smith Ltd,ACCRECCREDIT,2026-08-21,PAID,541.25,0.00
    CN-0023,DIISR - Small Business Services,ACCRECCREDIT,2026-08-16,PAID,21.70,0.00

# credit-note-lines.csv
    credit_note_number,description,quantity,unit_amount,account_code,tax_rate,tax_amount,line_amount
    CN-0014,CREDIT Half day training - Microsoft Office and include in suite of training INV-0013,1.0000,500.00,200,Tax on Consulting (8.25%),41.25,500.00
    CN-0015,Full credit - DUPLICATE of INV-0001,1.0000,500.00,200,Tax on Consulting (8.25%),41.25,500.00
    CN-0023,'Fish out of Water: Finding Your Brand' - credit - charged in error - should be included overall project,1.0000,19.95,200,Tax on Goods (8.75%),1.75,19.95

# payments.csv
    date,contact,document_type,document_number,amount,account_code,bank_account
    1 Aug 2026,PC Complete,ACCPAY,OG laptop,270.63,800,Business Bank Account
    10 Sep 2026,MCO Cleaning Services,ACCPAY,M000471,216.50,800,Business Bank Account
    11 Aug 2026,Boom FM,ACCREC,INV-0013,1082.50,610,Business Bank Account

# manual-journals.csv
    journal_number,date,narration,status,total

# manual-journal-lines.csv
    journal_number,account_code,description,debit,credit,tax_rate

# expense-claims.csv
    contact,date,claim_total,status,paid_date,paid_amount,description,account_code,account_name,tax_rate,tax_amount,line_amount
    Xero Demo,2026-09-10,115.95,unpaid,,0.00,Xero Demo - Battery pack & power cable for home office,453,Office Expenses,Tax on Purchases (8.25%),8.84,107.11
    Orlena Greenville,2026-08-11,29.50,paid,2026-08-11,29.50,Orlena Greenville - Print/bind report,461,Printing & Stationery,Tax on Purchases (8.25%),2.25,27.25
    Orlena Greenville,2026-07-11,34.90,paid,2026-07-27,34.90,Orlena Greenville - Parking for MRE conference,493,Travel - National,Tax on Purchases (8.25%),1.37,16.63

# fixed-assets.csv
    asset_number,name,type,purchase_date,cost,asset_account,accumulated_depreciation_account,depreciation_expense_account,depreciation_start_date,depreciation_method,rate,register,book_value,accumulated_depreciation,ytd_depreciation
    FA-0001,Laptop (Tracy),Computer Equipment,11 Sept 2026,1969.99,720 - Computer Equipment,721 - Less Accumulated Depreciation on Computer Equipment,416 - Depreciation,11 Sept 2026,Declining balance,40.00,draft,,,
    FA-0002,Photocopier,Office Equipment,11 Sept 2026,12000.00,710 - Office Equipment,711 - Less Accumulated Depreciation on Office Equipment,416 - Depreciation,11 Sept 2026,Declining balance,40.00,registered,12000.00,0.00,
    FA-0003,LCD Display,Computer Equipment,11 Sept 2026,6500.00,720 - Computer Equipment,721 - Less Accumulated Depreciation on Computer Equipment,416 - Depreciation,11 Sept 2026,Declining balance,40.00,registered,6500.00,0.00,

# opening-balances.csv
    account_code,account_name,date,description,amount,source,side,note
    090,Business Bank Account,2026-06-21,Conversion Balance,4130.98,Conversion Balance Journal,debit,Journal ID 388 on https://reporting.xero.com/!!6Sp3/v1/Run/1098; offsetting line is account 840
    840,Historical Adjustment,2026-06-21,Conversion Balance,-4130.98,Conversion Balance Journal,credit,Journal ID 388 on https://reporting.xero.com/!!6Sp3/v1/Run/1098; offsetting line is account 090
    630,Inventory,2026-09-11,Inventory Opening Balance,80.00,Inventory Opening Balance,debit,"Journal ID 651 (https://reporting.xero.com/!!6Sp3/v1/Run/1098), self-balancing inside account 630"
```

## 9. Method notes

* Everything was read from live Xero through the Orca embedded browser: the client-rendered
  invoice and credit-note grids via DOM table reads, the classic ASP pages
  (`Bank/BankTransactions.aspx`, `Bank/ViewTransaction.aspx`, `Contacts/Search.aspx`,
  `Journal/Search.aspx`) fetched same-origin and parsed with `DOMParser`, and the document APIs
  (`api.xero.com/invoicing.xro/1.0/invoices/<id>`, `/credit-notes/<id>`) with the session bearer
  token. No page was ever exported to CSV by Xero and re-imported; no figure was typed by hand.
* The bank grid paginates; `#mainPagerItemPerPageDropDown` was raised to 200 so all
  82 transactions (and their `bankTransactionID` links) were read in one pass.
* Dates are as Xero prints them (`11 Aug 2026` in `bank-transactions.csv`/`payments.csv`,
  ISO `2026-08-11` in the document files). Thousands separators are removed; money out is
  negative; `signed_amount` is negative for money out, as the previous worker's README specifies.
