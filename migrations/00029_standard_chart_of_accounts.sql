-- +goose Up
-- +goose StatementBegin
-- The standard chart of accounts, held as data so that "Import standard chart"
-- imports a chart instead of a literal.
--
-- WHY THIS EXISTS.  The Accounts screen offered to import a "standard chart",
-- and the chart it imported was a TypeScript array in
-- web/src/lib/chart-of-accounts.ts: twelve accounts, codes 090 Checking / 120
-- Accounts Receivable / 200 Accounts Payable / 220 Sales Tax / 400 Sales / 600
-- Advertising and so on.  Those are not this chart's rows.  This organisation's
-- Accounts Receivable is 610 and its Sales Tax is 820; it has no account at 120
-- or 220 at all, and its 200 is Sales, a revenue account, not Accounts Payable.
-- Importing that array here would have created a second Sales at 400 beside the
-- real one at 200 and an "Accounts Payable" at 200 in the middle of the revenue
-- range.  It was the same defect as the code literals this work removed, one
-- layer out: a fact about one chart, written where no chart can correct it.
--
-- WHERE THE ROWS COME FROM.  Xero's own standard chart, captured from the live
-- organisation and recorded in the reference dataset
-- (docs/xero-reference/README.md; migrations/data/xero/accounts.csv, 58 rows,
-- scraped from https://go.xero.com/GeneralLedger/ChartOfAccounts.aspx).  Every
-- row below is that file's code, name, tax rate and description verbatim and in
-- its order.  Two fields are carried in a different spelling, and only these
-- two:
--
--   * `type`.  The capture records the words Xero's chart screen prints; the API
--     speaks Xero's Account.Type enum (models/account.go, AccountType*).  The
--     capture uses ten of those names and they map one to one, so nothing is
--     inferred: Bank->BANK, Current Asset->CURRENT, Current Liability->CURRLIAB,
--     Direct Costs->DIRECTCOSTS, Equity->EQUITY, Expense->EXPENSE, Fixed
--     Asset->FIXED, Inventory->INVENTORY, Non-current Liability->TERMLIAB,
--     Revenue->REVENUE.  The self-check below refuses a row whose type is not in
--     that set.
--   * the file's `ytd` column, which is deliberately NOT carried.  A balance
--     belongs to the books it was read from; a chart template that shipped one
--     would be inventing an opening figure for whichever organisation imported
--     it.
--
-- WHAT IS NOT HERE, AND WHY.  No system_account role.  Xero assigns an account
-- its SystemAccount itself and its API does not accept one from a client --
-- Account.SystemAccount is returned, never posted -- so a chart import is not
-- where a control account becomes a control account.  (This chart's own roles
-- are declared separately by 00027, from this same file's accounts.)  An
-- organisation that imports this chart gets its 58 accounts; which of them holds
-- a role remains its own declarative step, which is the shape Xero has too.
--
-- No figure in this migration is computed; every value is a field of the
-- capture above.  The table is reference data keyed by code alone: it is one
-- chart, not one chart per organisation.
CREATE TABLE standard_chart_accounts (
    code          VARCHAR(20)  NOT NULL,
    name          VARCHAR(255) NOT NULL,
    type          VARCHAR(40)  NOT NULL,
    tax_rate_name VARCHAR(100) NOT NULL DEFAULT '',
    description   TEXT         NOT NULL DEFAULT '',
    display_order INTEGER      NOT NULL,
    PRIMARY KEY (code)
);

INSERT INTO standard_chart_accounts (code, name, type, tax_rate_name, description, display_order)
VALUES
    ('090', 'Business Bank Account', 'BANK', 'Tax Exempt (0%)', '', 0),
    ('091', 'Business Savings Account', 'BANK', 'Tax Exempt (0%)', '', 1),
    ('200', 'Sales', 'REVENUE', 'Tax on Consulting (8.25%)', 'Income from any normal business activity', 2),
    ('260', 'Other Revenue', 'REVENUE', 'Tax on Consulting (8.25%)', 'Any other income that does not relate to normal business activities and is not recurring', 3),
    ('270', 'Interest Income', 'REVENUE', 'Tax Exempt (0%)', 'Interest income', 4),
    ('300', 'Purchases', 'DIRECTCOSTS', 'Tax on Purchases (8.25%)', 'Goods purchased with the intention of selling these to customers', 5),
    ('310', 'Cost of Goods Sold', 'DIRECTCOSTS', 'Tax on Purchases (8.25%)', 'Cost of goods sold by the business.', 6),
    ('400', 'Advertising', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred for advertising while trying to increase sales', 7),
    ('404', 'Bank Fees', 'EXPENSE', 'Tax Exempt (0%)', 'Fees charged by your bank for transactions regarding your bank account(s).', 8),
    ('408', 'Cleaning', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred for cleaning business property.', 9),
    ('412', 'Consulting & Accounting', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses related to paying consultants', 10),
    ('416', 'Depreciation', 'EXPENSE', 'Tax Exempt (0%)', 'The amount of the asset''s cost (based on the useful life) that was consumed during the period', 11),
    ('420', 'Entertainment', 'EXPENSE', 'Tax Exempt (0%)', 'Expenses paid by company for the business but are not deductable for income tax purposes.', 12),
    ('425', 'Freight & Courier', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred on courier & freight costs', 13),
    ('429', 'General Expenses', 'EXPENSE', 'Tax on Purchases (8.25%)', 'General expenses related to the running of the business.', 14),
    ('433', 'Insurance', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred for insuring the business'' assets', 15),
    ('437', 'Interest Expense', 'EXPENSE', 'Tax Exempt (0%)', 'Any interest expenses paid to your tax authority, business bank accounts or credit card accounts.', 16),
    ('441', 'Legal expenses', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred on any legal matters', 17),
    ('445', 'Light, Power, Heating', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred for lighting, powering or heating the premises', 18),
    ('449', 'Motor Vehicle Expenses', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred on the running of company motor vehicles', 19),
    ('453', 'Office Expenses', 'EXPENSE', 'Tax on Purchases (8.25%)', 'General expenses related to the running of the business office.', 20),
    ('461', 'Printing & Stationery', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred by the entity as a result of printing and stationery', 21),
    ('469', 'Rent', 'EXPENSE', 'Tax on Purchases (8.25%)', 'The payment to lease a building or area.', 22),
    ('473', 'Repairs and Maintenance', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred on a damaged or run down asset that will bring the asset back to its original condition.', 23),
    ('477', 'Wages and Salaries', 'EXPENSE', 'Tax Exempt (0%)', 'Payment to employees in exchange for their resources', 24),
    ('478', 'Superannuation', 'EXPENSE', 'Tax Exempt (0%)', 'Superannuation contributions', 25),
    ('485', 'Subscriptions', 'EXPENSE', 'Tax on Purchases (8.25%)', 'E.g. Magazines, professional bodies', 26),
    ('489', 'Telephone & Internet', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenditure incurred from any business-related phone calls, phone lines, or internet connections', 27),
    ('493', 'Travel - National', 'EXPENSE', 'Tax on Purchases (8.25%)', 'Expenses incurred from domestic travel which has a business purpose', 28),
    ('494', 'Travel - International', 'EXPENSE', 'Tax Exempt (0%)', 'Expenses incurred from international travel which has a business purpose', 29),
    ('497', 'Bank Revaluations', 'EXPENSE', 'Tax Exempt (0%)', 'Bank account revaluations due for foreign exchange rate changes', 30),
    ('498', 'Unrealised Currency Gains', 'EXPENSE', 'Tax Exempt (0%)', 'Unrealised currency gains on outstanding items', 31),
    ('499', 'Realised Currency Gains', 'EXPENSE', 'Tax Exempt (0%)', 'Gains or losses made due to currency exchange rate changes', 32),
    ('505', 'Income Tax Expense', 'EXPENSE', 'Tax Exempt (0%)', 'A percentage of total earnings paid to the government.', 33),
    ('610', 'Accounts Receivable', 'CURRENT', 'Tax Exempt (0%)', 'Outstanding invoices the company has issued out to the client but has not yet received in cash at balance date.', 34),
    ('620', 'Prepayments', 'CURRENT', 'Tax Exempt (0%)', 'An expenditure that has been paid for in advance.', 35),
    ('630', 'Inventory', 'INVENTORY', 'Tax Exempt (0%)', 'Value of tracked inventory items for resale.', 36),
    ('710', 'Office Equipment', 'FIXED', 'Tax on Purchases (8.25%)', 'Office equipment that is owned and controlled by the business', 37),
    ('711', 'Less Accumulated Depreciation on Office Equipment', 'FIXED', 'Tax Exempt (0%)', 'The total amount of office equipment cost that has been consumed by the entity (based on the useful life)', 38),
    ('720', 'Computer Equipment', 'FIXED', 'Tax on Purchases (8.25%)', 'Computer equipment that is owned and controlled by the business', 39),
    ('721', 'Less Accumulated Depreciation on Computer Equipment', 'FIXED', 'Tax Exempt (0%)', 'The total amount of computer equipment cost that has been consumed by the business (based on the useful life)', 40),
    ('800', 'Accounts Payable', 'CURRLIAB', 'Tax Exempt (0%)', 'Outstanding invoices the company has received from suppliers but has not yet paid at balance date', 41),
    ('801', 'Unpaid Expense Claims', 'CURRLIAB', 'Tax Exempt (0%)', 'Expense claims typically made by employees/shareholder employees still outstanding.', 42),
    ('820', 'Sales Tax', 'CURRLIAB', 'Tax Exempt (0%)', 'The balance in this account represents Sales Tax owing to or from your tax authority. At the end of the tax period, it is this account that should be used to code against either the ''refunds from'' or ''payments to'' your tax authority that will appear on the bank statement. Xero has been designed to use only one sales tax account to track sales taxes on income and expenses, so there is no need to add any new sales tax accounts to Xero.', 43),
    ('825', 'Employee Tax Payable', 'CURRLIAB', 'Tax Exempt (0%)', 'The amount of tax that has been deducted from wages or salaries paid to employes and is due to be paid', 44),
    ('826', 'Superannuation Payable', 'CURRLIAB', 'Tax Exempt (0%)', 'The amount of superannuation that is due to be paid', 45),
    ('830', 'Income Tax Payable', 'CURRLIAB', 'Tax Exempt (0%)', 'The amount of income tax that is due to be paid, also resident withholding tax paid on interest received.', 46),
    ('835', 'Revenue Received in Advance', 'CURRLIAB', 'Tax Exempt (0%)', 'When customers pay in advance of work/services.', 47),
    ('840', 'Historical Adjustment', 'CURRLIAB', 'Tax Exempt (0%)', 'For accountant adjustments', 48),
    ('850', 'Suspense', 'CURRLIAB', 'Tax Exempt (0%)', 'An entry that allows an unknown transaction to be entered, so the accounts can still be worked on in balance and the entry can be dealt with later.', 49),
    ('855', 'Clearing Account', 'CURRLIAB', 'Tax Exempt (0%)', '', 50),
    ('860', 'Rounding', 'CURRLIAB', 'Tax Exempt (0%)', 'An adjustment entry to allow for rounding', 51),
    ('877', 'Tracking Transfers', 'CURRLIAB', 'Tax Exempt (0%)', 'Transfers between tracking categories', 52),
    ('880', 'Owner A Drawings', 'CURRLIAB', 'Tax Exempt (0%)', 'Withdrawals by the owners', 53),
    ('881', 'Owner A Funds Introduced', 'CURRLIAB', 'Tax Exempt (0%)', 'Funds contributed by the owner', 54),
    ('900', 'Loan', 'TERMLIAB', 'Tax Exempt (0%)', 'Money that has been borrowed from a creditor', 55),
    ('960', 'Retained Earnings', 'EQUITY', 'Tax Exempt (0%)', 'Do not Use', 56),
    ('970', 'Owner A Share Capital', 'EQUITY', 'Tax Exempt (0%)', 'The value of shares purchased by the shareholders', 57)
;

-- Self-check: all 58 captured accounts landed, in order, with no duplicate code
-- and no type outside Xero's own enum.
DO $$
DECLARE n int; m int; bad text;
BEGIN
  SELECT count(*), count(DISTINCT code) INTO n, m FROM standard_chart_accounts;
  IF n <> 58 OR m <> 58 THEN
    RAISE EXCEPTION 'standard chart has % rows and % distinct codes, expected 58 and 58', n, m;
  END IF;
  IF (SELECT count(*) FROM standard_chart_accounts WHERE display_order < 0 OR display_order > 57) <> 0 THEN
    RAISE EXCEPTION 'standard chart display_order is not 0..57';
  END IF;
  SELECT string_agg(DISTINCT type, ', ') INTO bad FROM standard_chart_accounts
   WHERE type NOT IN ('BANK','CURRENT','FIXED','INVENTORY','NONCURRENT','PREPAYMENT',
                      'CURRLIAB','LIABILITY','TERMLIAB','PAYGLIABILITY','SUPERANNUATIONLIABILITY',
                      'EQUITY','REVENUE','SALES','EXPENSE','OVERHEADS','DEPRECIATN',
                      'DIRECTCOSTS','WAGESEXPENSE');
  IF bad IS NOT NULL THEN
    RAISE EXCEPTION 'standard chart carries account types outside Xero''s enum: %', bad;
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE standard_chart_accounts;
-- +goose StatementEnd
